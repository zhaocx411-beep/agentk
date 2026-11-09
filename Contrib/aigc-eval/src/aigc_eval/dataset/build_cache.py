#!/usr/bin/env python3
"""xlsx → mm-datasets/<name>/cache/：任意数据集通用的素材落盘脚本。

`batch_0807.py` 把数据集目录、xlsx 名与列名全写死了（那份表的列叫「输入」/
「参考图片、视频、音频」），后来的表换成了「原始输入」/「MediaData Key」，
所以这里做成参数化版本，列名两套都认。

用法：
    python3 build_dataset_cache.py 0811-dataset
    python3 build_dataset_cache.py 0811-dataset --reuse-videos-from 0810-dataset
    python3 build_dataset_cache.py 0811-dataset --dry-run

`--reuse-videos-from` 用于两份表的「结果」URL 完全相同的情况（同一批视频、
只改了 prompt）：结果视频改为硬链接复用，省掉重复下载与一份磁盘拷贝。
链接前会比对 URL，不同就照常下载，不会张冠李戴。
"""

from __future__ import annotations

import argparse
import json
import os
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from typing import Any

from aigc_eval import paths
from aigc_eval.batch.run_0807 import _ext, _read_xlsx, _safe_name, _urls, download, download_ref, parse_label

DATASETS_ROOT = paths.WORKSPACE_ROOT / "mm-datasets"

# 两代表头都要认：老表是「输入」/「参考图片、视频、音频」，新表是「原始输入」/「MediaData Key」。
PROMPT_KEYS = ("原始输入", "输入", "prompt")
REF_KEYS = ("MediaData Key", "参考图片、视频、音频")
RESULT_KEYS = ("结果", "results")


def _pick(row: dict[str, str], keys: tuple[str, ...]) -> str:
    for key in keys:
        if row.get(key):
            return str(row[key])
    return ""


def load_cases(xlsx: Path) -> list[dict[str, Any]]:
    cases: list[dict[str, Any]] = []
    for row in _read_xlsx(xlsx):
        case_id = str(row.get("Case ID") or "").strip()
        if not case_id:
            continue
        prompt = _pick(row, PROMPT_KEYS).strip()
        # 部分表的 prompt 带 "prompt " 前缀，是导出噪声，不是内容。
        if prompt.lower().startswith("prompt "):
            prompt = prompt[7:].strip()
        refs = []
        for url in _urls(_pick(row, REF_KEYS)):
            try:
                kind, label = parse_label(url)
            except ValueError as error:
                print(f"  ! {case_id} 跳过无法解析标签的素材：{error}")
                continue
            refs.append({"kind": kind, "label": label, "url": url})
        order = {"image": 0, "audio": 1, "video": 2}
        refs.sort(key=lambda r: (order[r["kind"]], int(r["label"].split()[-1])))
        results = _urls(_pick(row, RESULT_KEYS))
        if not prompt or not results:
            print(f"  ! {case_id} 缺 prompt 或结果视频，跳过")
            continue
        cases.append({"case_id": case_id, "prompt": prompt, "refs": refs, "results": results})
    return cases


def _reuse_index(donor: Path) -> dict[str, Path]:
    """供体数据集的 URL → 本地文件。case.json 与 prompt.json 里都记着这份映射，
    两个都扫（0810 只落了 prompt.json）。结果视频与参考素材一并收录 ——
    这台机器没有外网出口，能复用的必须复用，否则整个数据集建不起来。"""
    index: dict[str, Path] = {}
    for meta_path in sorted(donor.glob("*/case.json")) + sorted(donor.glob("*/prompt.json")):
        try:
            meta = json.loads(meta_path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            continue
        for key in ("results", "references", "refs"):
            for item in meta.get(key) or []:
                if not isinstance(item, dict):
                    continue
                local = meta_path.parent / str(item.get("file") or "")
                if item.get("url") and item.get("file") and local.exists():
                    index.setdefault(str(item["url"]), local)
    return index


def _link_or_copy(src: Path, dest: Path) -> None:
    try:
        os.link(src, dest)          # 同盘硬链接：不占额外空间
    except OSError:
        dest.write_bytes(src.read_bytes())


def build(name: str, *, reuse_from: str = "", workers: int = 8, dry_run: bool = False) -> None:
    dataset_dir = DATASETS_ROOT / name
    xlsx_files = sorted(dataset_dir.glob("*.xlsx"))
    if not xlsx_files:
        raise SystemExit(f"{dataset_dir} 下没有 xlsx")
    if len(xlsx_files) > 1:
        print(f"发现多个 xlsx，用第一个：{[f.name for f in xlsx_files]}")
    xlsx = xlsx_files[0]
    cache_dir = dataset_dir / "cache"

    cases = load_cases(xlsx)
    n_ref = sum(len(c["refs"]) for c in cases)
    n_res = sum(len(c["results"]) for c in cases)
    print(f"{xlsx.name}: {len(cases)} case · 参考素材 {n_ref} 个 · 结果视频 {n_res} 条")

    reuse: dict[str, Path] = {}
    if reuse_from:
        reuse = _reuse_index(DATASETS_ROOT / reuse_from / "cache")
        hit_res = sum(1 for c in cases for u in c["results"] if u in reuse)
        hit_ref = sum(1 for c in cases for r in c["refs"] if r["url"] in reuse)
        print(f"复用 {reuse_from}：视频 {hit_res}/{n_res} · 参考素材 {hit_ref}/{n_ref}")
        if hit_res + hit_ref < n_res + n_ref:
            print(f"  ! 有 {n_res + n_ref - hit_res - hit_ref} 个素材需联网下载；"
                  f"若本机无外网出口，这些会标记为缺失")

    if dry_run:
        print("（dry-run，未写任何文件）")
        return

    for pos, case in enumerate(cases, start=1):
        case_dir = cache_dir / _safe_name(case["case_id"])
        case_dir.mkdir(parents=True, exist_ok=True)
        linked = downloaded = 0
        result_paths: list[Path | None] = [None] * len(case["results"])

        with ThreadPoolExecutor(max_workers=workers) as pool:
            pending = {}
            for index, url in enumerate(case["results"], start=1):
                dest = case_dir / f"result_{index:02d}{_ext(url, '.mp4')}"
                if dest.exists() and dest.stat().st_size > 0:
                    result_paths[index - 1] = dest
                    continue
                donor = reuse.get(url)
                if donor is not None:
                    _link_or_copy(donor, dest)
                    result_paths[index - 1] = dest
                    linked += 1
                    continue
                pending[pool.submit(download, url, dest)] = index
            # 参考素材同样先查复用，再考虑下载。文件名沿用供体的（label 已编码在名里）。
            ref_pending = {}
            for index, ref in enumerate(case["refs"], start=1):
                donor = reuse.get(ref["url"])
                if donor is not None:
                    dest = case_dir / donor.name
                    if not (dest.exists() and dest.stat().st_size > 0):
                        _link_or_copy(donor, dest)
                        linked += 1
                    ref["path"] = str(dest)
                    continue
                ref_pending[pool.submit(download_ref, ref, case_dir, index)] = ref
            for future, index in pending.items():
                got = future.result()
                result_paths[index - 1] = got
                downloaded += got is not None
            for future, ref in ref_pending.items():
                got = future.result()
                ref["path"] = str(got) if got else None
                downloaded += got is not None

        case["result_paths"] = result_paths
        missing_ref = sum(1 for r in case["refs"] if not r.get("path"))
        missing_res = sum(1 for p in result_paths if p is None)
        _write_meta(case, case_dir)
        flag = "✓" if not (missing_ref or missing_res) else "✗"
        print(f"  {flag} [{pos}/{len(cases)}] {case['case_id']}: "
              f"视频 {len(result_paths) - missing_res}/{len(result_paths)}"
              f"（链接 {linked} / 下载 {downloaded}）· "
              f"素材 {len(case['refs']) - missing_ref}/{len(case['refs'])}")

    print(f"\n完成 → {cache_dir}")


def _write_meta(case: dict[str, Any], case_dir: Path) -> None:
    """prompt.txt 给人看；prompt.json / case.json 记录 label → 本地文件的映射。

    dataset_cache.py 读的是 prompt.json 的 prompt_zh，两个文件都写以兼容旧报告。
    """
    (case_dir / "prompt.txt").write_text(case["prompt"] + "\n", encoding="utf-8")
    refs = [
        {
            "label": ref["label"],
            "kind": ref["kind"],
            "file": Path(ref["path"]).name if ref.get("path") else None,
            "url": ref["url"],
        }
        for ref in case["refs"]
    ]
    results = [
        {"index": i, "file": p.name if p else None, "url": u}
        for i, (p, u) in enumerate(zip(case["result_paths"], case["results"]), start=1)
    ]
    meta = {"case_id": case["case_id"], "prompt": case["prompt"], "refs": refs, "results": results}
    (case_dir / "case.json").write_text(
        json.dumps(meta, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    (case_dir / "prompt.json").write_text(
        json.dumps({
            "prompt_zh": case["prompt"],
            "prompt_en": "",
            "gold_label": "",
            "gold_note": "",
            "case_id": case["case_id"],
            "references": refs,
            "results": results,
        }, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("dataset", help="mm-datasets 下的目录名，如 0811-dataset")
    parser.add_argument("--reuse-videos-from", default="", help="结果视频 URL 相同时从该数据集硬链接复用")
    parser.add_argument("--workers", type=int, default=8)
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument(
        "--no-download", action="store_true",
        help="只用复用的素材，完全不联网（本机无外网出口时用；"
             "否则每个不可达 URL 要重试 3×120s）")
    args = parser.parse_args()
    if args.no_download:
        import aigc_eval.batch.run_0807 as batch_0807

        def _offline(url, dest, *, retries=1, quiet=False):  # noqa: ANN001
            return None

        batch_0807.download = _offline
        globals()["download"] = _offline
        globals()["download_ref"] = lambda ref, case_dir, index: None
    build(args.dataset, reuse_from=args.reuse_videos_from,
          workers=args.workers, dry_run=args.dry_run)


if __name__ == "__main__":
    main()
