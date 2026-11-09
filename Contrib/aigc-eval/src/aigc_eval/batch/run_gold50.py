"""批量质检 I2VA*/T2VA* 集合里的全部 case（直连 analyze_case，不经 HTTP）。

用法：
    ./deploy/run_agentk.sh -m aigc_eval.batch.run_gold50 --workers 4
    ./deploy/run_agentk.sh -m aigc_eval.batch.run_gold50 --workers 4 --refresh      # 忽略历史重跑
    ./deploy/run_agentk.sh -m aigc_eval.batch.run_gold50 --workers 1 --limit 1      # 先验证
"""

from __future__ import annotations

import argparse
import json
import threading
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path
from typing import Any

from aigc_eval.judge.native8 import summarize_native8
from aigc_eval.judge.pipeline import RUNTIME_ROOT, analyze_case, discover_cases

OUT_DIR = RUNTIME_ROOT / "batch_gold50"
SUMMARY_PATH = OUT_DIR / "summary.jsonl"

_lock = threading.Lock()
_counter = {"ok": 0, "failed": 0, "done": 0}


def _log(message: str) -> None:
    with _lock:
        print(message, flush=True)


def _append(record: dict[str, Any]) -> None:
    with _lock:
        with SUMMARY_PATH.open("a", encoding="utf-8") as handle:
            handle.write(json.dumps(record, ensure_ascii=False) + "\n")


def _done_keys() -> set[str]:
    if not SUMMARY_PATH.exists():
        return set()
    keys: set[str] = set()
    for line in SUMMARY_PATH.read_text(encoding="utf-8").splitlines():
        if not line.strip():
            continue
        try:
            row = json.loads(line)
        except json.JSONDecodeError:
            continue
        if not row.get("error"):
            keys.add(str(row.get("case_id", "")))
    return keys


def run_one(case: dict[str, str], *, total: int, refresh: bool, mock: bool) -> None:
    case_id = case["case_id"]
    started = time.time()
    try:
        report = analyze_case(
            case_id,
            use_vlm=not mock,
            provider="agentk",
            allow_fallback=mock,
            source="kling",
            refresh=refresh,
        )
        price = (report.get("cost") or {}).get("price") or {}
        record = {
            "case_id": case_id,
            "collection": case.get("collection"),
            "mode_hint": case.get("mode_hint"),
            **summarize_native8(report),
            "from_cache": report.get("from_cache"),
            "cost_cny": price.get("total_cost"),
            "elapsed_s": round(time.time() - started, 1),
            "report_html": report.get("report_html_path"),
        }
        _append(record)
        with _lock:
            _counter["ok"] += 1
            _counter["done"] += 1
            index = _counter["done"]
        cache_mark = "⋆缓存" if record["from_cache"] else "      "
        _log(
            f"[{index}/{total}] {cache_mark} {case_id:34} "
            f"违规 {record['violated_subcriteria']:>3} · "
            f"未核实 {record['unverified_dimensions']}/{record['applicable_dimensions']} 维 · "
            f"待复核 {record['review_required_count']:>2} · "
            f"{record['elapsed_s']:6.1f}s ¥{record['cost_cny'] or 0:.2f}"
        )
    except Exception as error:  # noqa: BLE001 - 单条失败不能拖垮整批
        _append({"case_id": case_id, "error": f"{type(error).__name__}: {error}"[:500]})
        with _lock:
            _counter["failed"] += 1
            _counter["done"] += 1
            index = _counter["done"]
        _log(f"[{index}/{total}] ✗ {case_id:34} {type(error).__name__}: {str(error)[:90]}")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--workers", type=int, default=4)
    parser.add_argument("--limit", type=int, default=0)
    parser.add_argument("--collection", default="", help="只跑某个集合前缀，如 T2VA")
    parser.add_argument("--refresh", action="store_true", help="忽略历史结果重算")
    parser.add_argument("--resume", action="store_true", help="跳过 summary 里已成功的")
    parser.add_argument("--mock", action="store_true")
    args = parser.parse_args()

    OUT_DIR.mkdir(parents=True, exist_ok=True)
    cases = discover_cases()
    if args.collection:
        cases = [c for c in cases if c["collection"].startswith(args.collection)]
    if args.resume:
        done = _done_keys()
        before = len(cases)
        cases = [c for c in cases if c["case_id"] not in done]
        _log(f"续跑：已完成 {before - len(cases)}，剩余 {len(cases)}")
    if args.limit:
        cases = cases[: args.limit]

    total = len(cases)
    by_col: dict[str, int] = {}
    for case in cases:
        by_col[case["collection"]] = by_col.get(case["collection"], 0) + 1
    _log(
        f"待跑 {total} 个 case（{by_col}）· 并发 {args.workers} · "
        f"{'mock' if args.mock else 'Gemini'}{' · refresh' if args.refresh else ''}"
    )
    if not total:
        return

    started = time.time()
    with ThreadPoolExecutor(max_workers=args.workers) as pool:
        futures = [
            pool.submit(run_one, c, total=total, refresh=args.refresh, mock=args.mock)
            for c in cases
        ]
        for future in as_completed(futures):
            future.result()

    rows = [
        json.loads(line)
        for line in SUMMARY_PATH.read_text(encoding="utf-8").splitlines()
        if line.strip()
    ][-total:]
    ok = [r for r in rows if not r.get("error")]
    costs = sum(r.get("cost_cny") or 0 for r in ok)
    # 整批只汇总能相加的量，且必须带上覆盖率：违规总数低也可能只是这批没看清。
    violated = sum(r.get("violated_subcriteria") or 0 for r in ok)
    review = sum(r.get("review_required_count") or 0 for r in ok)
    partial = sum(1 for r in ok if r.get("unverified_dimensions"))
    _log(
        f"\n完成：{_counter['ok']} 成功 / {_counter['failed']} 失败 · "
        f"耗时 {(time.time() - started) / 60:.1f} 分钟 · 合计 ¥{costs:.2f}"
    )
    if ok:
        _log(
            f"违规子判据 {violated} 条 · 待复核 {review} 条 · "
            f"含未核实维度的 case {partial}/{len(ok)}"
        )
    _log(f"汇总 → {SUMMARY_PATH}")


if __name__ == "__main__":
    main()
