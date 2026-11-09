#!/usr/bin/env python3
"""把离线数据集 cache/ 下的全部 case 并行跑一遍 Gemini 质检。

与 `batch_0807.py` 的区别：那个脚本从 xlsx 出发、需要联网下载素材、且串行；
这里直接以磁盘上已有的 `mm-datasets/*/cache/<case>/` 为准，并发跑。

用法：
    python3 batch_dataset.py                    # 全部 case 的全部 result 视频
    python3 batch_dataset.py --first-only       # 每个 case 只跑第 1 条 result
    python3 batch_dataset.py --workers 4        # 调整并发
    python3 batch_dataset.py --mock             # 不花钱的端到端自测
    python3 batch_dataset.py --resume           # 跳过已成功跑过的（断点续跑）
    python3 batch_dataset.py --provider agentk  # 走 AgentK 通道而非直连 Gemini

结果逐条追加到 runtime/batch_dataset/summary.jsonl，单条失败不影响整批。
"""

from __future__ import annotations

import argparse
import json
import os
import re
import threading
import time
import traceback
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path
from typing import Any

# 批量跑不需要人工核查用的证据图，跳过可显著提速。
os.environ.setdefault("JUDGE_SKIP_EVIDENCE", "1")

from aigc_eval import paths
from aigc_eval.config import LIVE_PROVIDERS
from aigc_eval.dataset.cache import dataset_asset_path, dataset_case_detail, list_dataset_cases

OUT_DIR = paths.RUNTIME_ROOT / "batch_dataset"
# summary 按数据集分文件：case_id 只是 cache/ 下的目录名（1.2、s2-3……），
# 不同数据集会重名，共用一个 summary 会让 --resume 把新数据集里同名的 case
# 当成"已经跑过"直接跳掉。以数据集目录名作后缀彻底隔开。
# 未设环境变量时跟 dataset_cache.DATASET_ROOT 走同一个默认值，避免两边不一致。
from aigc_eval.dataset.cache import DATASET_ROOT as _DEFAULT_DATASET_ROOT

_DATASET_TAG = re.sub(
    r"[^0-9A-Za-z_.-]", "_",
    Path(os.environ.get("VIDEO_JUDGE_DATASET_ROOT", str(_DEFAULT_DATASET_ROOT))).resolve().parent.name,
)
SUMMARY_PATH = OUT_DIR / f"summary__{_DATASET_TAG}.jsonl"

_print_lock = threading.Lock()
_write_lock = threading.Lock()
_counter = {"done": 0, "ok": 0, "failed": 0}


def _log(message: str) -> None:
    with _print_lock:
        print(f"[{time.strftime('%H:%M:%S')}] {message}", flush=True)


def _append(row: dict[str, Any]) -> None:
    with _write_lock:
        with SUMMARY_PATH.open("a", encoding="utf-8") as handle:
            handle.write(json.dumps(row, ensure_ascii=False) + "\n")


def _done_keys() -> set[str]:
    """已成功跑过的 (case_id, video) —— --resume 用它跳过。

    完成与否只认报告一定会写出的 schema_version / analysis_status。这里判错的
    代价是不对称的：漏认一条就要重跑、重新计费，所以磁盘上只带 score_total 的
    旧汇总行也一并认账 —— 多认一条最多少跑一条，不烧钱。
    """
    if not SUMMARY_PATH.exists():
        return set()
    keys: set[str] = set()
    for line in SUMMARY_PATH.read_text(encoding="utf-8").splitlines():
        try:
            row = json.loads(line)
        except json.JSONDecodeError:
            continue
        if row.get("error"):
            continue
        if row.get("schema_version") or row.get("analysis_status") or row.get("score_total") is not None:
            keys.add(f"{row.get('case_id')}::{row.get('video')}")
    return keys


def build_jobs(*, first_only: bool, only_case: str = "") -> list[dict[str, Any]]:
    jobs: list[dict[str, Any]] = []
    for item in list_dataset_cases():
        case_id = item["case_id"]
        if only_case and case_id != only_case:
            continue
        try:
            detail = dataset_case_detail(case_id)
        except Exception as error:  # noqa: BLE001
            _log(f"跳过 {case_id}：{error}")
            continue
        results = detail["results"][:1] if first_only else detail["results"]
        for index, result in enumerate(results, start=1):
            jobs.append({
                "case_id": case_id,
                "video": result["file"],
                "video_index": index,
                "prompt": detail["prompt"],
                "refs": detail["refs"],
                "mode": detail["mode"],
            })
    return jobs


def run_job(
    job: dict[str, Any], *, total: int, mock: bool, provider: str = "agentk"
) -> dict[str, Any]:
    from aigc_eval.judge.pipeline import analyze_case
    from aigc_eval.judge.native8 import summarize_native8
    from aigc_eval.dataset.upload_case import build_upload_case_files

    label = f"{job['case_id']}/{job['video']}"
    started = time.time()
    try:
        refs = [
            {
                "path": str(dataset_asset_path(job["case_id"], ref["file"])),
                "label": ref["label"],
                "kind": ref["kind"],
            }
            for ref in job["refs"]
        ]
        case_files = build_upload_case_files(
            prompt=job["prompt"],
            video_path=dataset_asset_path(job["case_id"], job["video"]),
            references=refs,
        )
        report = analyze_case(
            case_files.case_id,
            case_files=case_files,
            use_vlm=not mock,
            provider=provider,
            allow_fallback=mock,
            source="upload",
        )
        cost = report.get("cost") or {}
        row = {
            "case_id": job["case_id"],
            "video": job["video"],
            "mode": job["mode"],
            **summarize_native8(report),
            "calls": cost.get("calls"),
            "total_tokens": cost.get("total_tokens"),
            "cost_cny": (cost.get("price") or {}).get("total_cost"),
            "report_html": report.get("report_html_path"),
            "run_id": ((report.get("llm_trace") or {}).get("run_id")),
            "elapsed_s": round(time.time() - started, 1),
        }
        _counter["ok"] += 1
    except Exception as error:  # noqa: BLE001 - 单条失败不能打断整批
        row = {
            "case_id": job["case_id"],
            "video": job["video"],
            "error": str(error)[:300],
            "traceback": traceback.format_exc()[-800:],
            "elapsed_s": round(time.time() - started, 1),
        }
        _counter["failed"] += 1

    _counter["done"] += 1
    n = _counter["done"]
    if row.get("error"):
        _log(f"✗ [{n}/{total}] {label}: {row['error'][:120]}")
    else:
        # 未核实维度数必须和违规数并排出现：漏掉它，一条几乎没看清的 case
        # 会以「违规 0 条」的样子滑过操作员的眼睛。
        _log(
            f"✓ [{n}/{total}] {label}: 违规 {row['violated_subcriteria']} 条 · "
            f"未核实 {row['unverified_dimensions']}/{row['applicable_dimensions']} 维 · "
            f"待复核 {row['review_required_count']} · calls={row['calls']} "
            f"¥{row['cost_cny']} {row['elapsed_s']}s"
        )
    _append(row)
    return row


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--workers", type=int, default=6, help="并发数")
    parser.add_argument("--first-only", action="store_true", help="每个 case 只跑第 1 条 result")
    parser.add_argument("--case", default="", help="只跑指定 case_id")
    parser.add_argument("--limit", type=int, default=0, help="只跑前 N 条")
    parser.add_argument("--mock", action="store_true", help="不调模型的端到端自测")
    parser.add_argument("--resume", action="store_true", help="跳过已成功跑过的")
    # 写死 agentk 而不是读 DEFAULT_PROVIDER：离线基线的可比性依赖固定通道，
    # 不随服务端默认值漂移。
    # 想用 AgentK 通道跑批量时显式传 --provider agentk。
    parser.add_argument(
        "--provider", default="agentk", choices=LIVE_PROVIDERS, help="模型通道"
    )
    args = parser.parse_args()

    OUT_DIR.mkdir(parents=True, exist_ok=True)
    jobs = build_jobs(first_only=args.first_only, only_case=args.case)

    if args.resume:
        done = _done_keys()
        before = len(jobs)
        jobs = [j for j in jobs if f"{j['case_id']}::{j['video']}" not in done]
        _log(f"续跑：已完成 {before - len(jobs)} 条，剩余 {len(jobs)} 条")
    if args.limit:
        jobs = jobs[: args.limit]

    total = len(jobs)
    cases = len({j["case_id"] for j in jobs})
    _log(f"待跑 {total} 条视频（{cases} 个 case）· 并发 {args.workers} · "
         f"{'mock' if args.mock else args.provider}")
    if not total:
        return

    started = time.time()
    with ThreadPoolExecutor(max_workers=args.workers) as pool:
        futures = [
            pool.submit(run_job, j, total=total, mock=args.mock, provider=args.provider)
            for j in jobs
        ]
        for future in as_completed(futures):
            future.result()

    elapsed = time.time() - started
    rows = [
        json.loads(line)
        for line in SUMMARY_PATH.read_text(encoding="utf-8").splitlines()
        if line.strip()
    ]
    recent = rows[-total:]
    costs = [r.get("cost_cny") or 0 for r in recent if not r.get("error")]
    calls = [r.get("calls") or 0 for r in recent if not r.get("error")]
    _log(
        f"\n完成：{_counter['ok']} 成功 / {_counter['failed']} 失败 · "
        f"耗时 {elapsed / 60:.1f} 分钟 · 合计 ¥{sum(costs):.1f} · "
        f"平均 {sum(calls) / len(calls):.1f} 次调用/条"
        if calls else f"\n完成：{_counter['ok']} 成功 / {_counter['failed']} 失败"
    )
    _log(f"汇总 → {SUMMARY_PATH}")


if __name__ == "__main__":
    main()
