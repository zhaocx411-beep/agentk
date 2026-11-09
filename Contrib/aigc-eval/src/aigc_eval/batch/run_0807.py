"""0807 数据集：解析 xlsx → 下载素材 → 批量质检。

参考素材的标签必须与 prompt 里的引用对齐：prompt 写「参考 Image 4」「声音参考 Audio 1」，
而 URL 文件名是 `1-2-image-4.png` / `1-2-audio-1.mp3`，两种命名（image-N / img-N）都出现过。
标签解析失败就等于模型对不上素材，所以这里对 135 个 URL 做过全量校验。

用法：
    python3 batch_0807.py --download-only          # 只下载
    python3 batch_0807.py --limit 1                # 跑 1 条（先验证）
    python3 batch_0807.py --mock --limit 2         # 不花钱的端到端自测
"""

from __future__ import annotations

import argparse
import json
import re
import sys
import threading
import time
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from typing import Any

from aigc_eval import paths


DATASET_DIR = paths.WORKSPACE_ROOT / "mm-datasets" / "0807-dataset"
XLSX_PATH = DATASET_DIR / "eval_cases_0807_part1.xlsx"
CACHE_DIR = DATASET_DIR / "cache"

_KIND_LABEL = {"image": "Image", "img": "Image", "audio": "Audio", "video": "Video"}
# 两种命名：`1-2-image-4.png` 与 `s2-7-img-1.png`；后缀还有 .MP3 / .png.png 之类的脏数据。
_LABEL_RE = re.compile(r"-(image|img|audio|video)-(\d+)", re.I)


def parse_label(url: str) -> tuple[str, str]:
    """从文件名反解 (kind, label)，label 形如 "Image 4"。"""
    filename = url.rsplit("/", 1)[-1]
    match = _LABEL_RE.search(filename)
    if not match:
        raise ValueError(f"无法解析参考素材标签：{filename}")
    kind_raw = match.group(1).lower()
    kind = "image" if kind_raw in ("image", "img") else kind_raw
    return kind, f"{_KIND_LABEL[kind_raw]} {int(match.group(2))}"


def _urls(cell: Any) -> list[str]:
    if cell is None:
        return []
    text = str(cell)
    if not text or text == "nan":
        return []
    return [u.strip() for u in re.split(r"\s+", text) if u.strip().startswith("http")]


def _safe_name(case_id: str) -> str:
    return re.sub(r"[^0-9A-Za-z_.-]", "_", case_id)


def _read_xlsx(path: Path) -> list[dict[str, str]]:
    """stdlib 解析 xlsx → [{列名: 单元格文本}]。

    机器上装不了 openpyxl（无 PyPI 出口），而 xlsx 就是个 zip + XML，
    这里只取「读文本单元格」这一个子集：sharedStrings + inline/数字，够本脚本用。
    """
    import xml.etree.ElementTree as ET
    import zipfile

    ns = "{http://schemas.openxmlformats.org/spreadsheetml/2006/main}"
    with zipfile.ZipFile(path) as bundle:
        shared: list[str] = []
        if "xl/sharedStrings.xml" in bundle.namelist():
            for item in ET.fromstring(bundle.read("xl/sharedStrings.xml")).findall(f"{ns}si"):
                shared.append("".join(node.text or "" for node in item.iter(f"{ns}t")))
        sheet = ET.fromstring(bundle.read("xl/worksheets/sheet1.xml"))

    def column(ref: str) -> int:
        letters = "".join(ch for ch in ref if ch.isalpha())
        index = 0
        for ch in letters:
            index = index * 26 + (ord(ch.upper()) - 64)
        return index - 1

    rows: list[list[str]] = []
    for row_node in sheet.iter(f"{ns}row"):
        cells: dict[int, str] = {}
        for cell in row_node.findall(f"{ns}c"):
            value_node = cell.find(f"{ns}v")
            if cell.get("t") == "s" and value_node is not None:
                text = shared[int(value_node.text or 0)]
            elif cell.get("t") == "inlineStr":
                text = "".join(n.text or "" for n in cell.iter(f"{ns}t"))
            else:
                text = (value_node.text or "") if value_node is not None else ""
            cells[column(cell.get("r") or "A1")] = text.strip()
        rows.append([cells.get(i, "") for i in range(max(cells) + 1 if cells else 0)])

    if not rows:
        return []
    header = rows[0]
    return [
        {header[i]: row[i] for i in range(min(len(header), len(row))) if header[i]}
        for row in rows[1:]
    ]


def load_dataset() -> list[dict[str, Any]]:
    """一行 xlsx → 一个 case（含 N 个参考素材与 M 个待质检结果视频）。"""
    cases: list[dict[str, Any]] = []
    for row in _read_xlsx(XLSX_PATH):
        case_id = str(row.get("Case ID") or "").strip()
        if not case_id:
            continue
        prompt = str(row.get("输入") or "").strip()
        # 「输入」列统一以 "prompt " 开头，去掉这个噪声前缀。
        if prompt.lower().startswith("prompt "):
            prompt = prompt[7:].strip()
        refs = []
        for url in _urls(row.get("参考图片、视频、音频")):
            kind, label = parse_label(url)
            refs.append({"kind": kind, "label": label, "url": url})
        # 标签排序：Image 1..N, Audio 1..N, Video 1..N，与 prompt 阅读顺序一致。
        order = {"image": 0, "audio": 1, "video": 2}
        refs.sort(key=lambda r: (order[r["kind"]], int(r["label"].split()[-1])))
        results = _urls(row.get("结果"))
        if not prompt or not results:
            continue
        cases.append({"case_id": case_id, "prompt": prompt, "refs": refs, "results": results})
    return cases


# ---------------------------------------------------------------------------
# 下载：已存在且非空即跳过，支持反复中断续跑。
# ---------------------------------------------------------------------------

_PRINT_LOCK = threading.Lock()


def _log(message: str) -> None:
    with _PRINT_LOCK:
        sys.stdout.write(message + "\n")
        sys.stdout.flush()


def download(url: str, dest: Path, *, retries: int = 3, quiet: bool = False) -> Path | None:
    if dest.exists() and dest.stat().st_size > 0:
        return dest
    dest.parent.mkdir(parents=True, exist_ok=True)
    tmp = dest.with_suffix(dest.suffix + ".part")
    for attempt in range(retries):
        try:
            request = urllib.request.Request(url, headers={"User-Agent": "aigc-eval/1.0"})
            with urllib.request.urlopen(request, timeout=120) as response, tmp.open("wb") as handle:
                while chunk := response.read(1 << 20):
                    handle.write(chunk)
            if tmp.stat().st_size == 0:
                raise OSError("空文件")
            tmp.replace(dest)
            return dest
        except (urllib.error.URLError, OSError, TimeoutError) as error:
            tmp.unlink(missing_ok=True)
            if attempt == retries - 1:
                if not quiet:
                    _log(f"  ✗ 下载失败 {dest.name}: {error}")
                return None
            time.sleep(2 * (attempt + 1))
    return None


def _ext(url: str, fallback: str) -> str:
    suffix = Path(url.split("?")[0]).suffix.lower()
    # `.png.png` / `.MP3` 之类的脏后缀在这里被归一。
    return suffix if suffix in {".png", ".jpg", ".jpeg", ".webp", ".mp4", ".mp3", ".wav", ".m4a"} else fallback


# kind → 该类别可接受的后缀，第一个是首选。数据集里 `s3-3a-audio-1.mp4` 标签写 audio
# 却挂 `.mp4` 后缀（真身是 mp3，原 URL 404），若照抄后缀落盘，`reference_kind()` 会按
# 后缀把它判成 video 送进 video_part —— 音频参考就此静默丢失。所以以**标签的 kind 为准**，
# 后缀与 kind 冲突时改用该 kind 的候选后缀逐个探测。
_KIND_EXTS: dict[str, tuple[str, ...]] = {
    "image": (".png", ".jpg", ".jpeg", ".webp"),
    "audio": (".mp3", ".wav", ".m4a"),
    "video": (".mp4", ".mov"),
}


def download_ref(ref: dict[str, Any], case_dir: Path, index: int) -> Path | None:
    """按标签 kind 落盘参考素材，后缀与 kind 冲突时换后缀重试。"""
    exts = _KIND_EXTS[ref["kind"]]
    url_ext = Path(ref["url"].split("?")[0]).suffix.lower()
    candidates = [ref["url"]]
    if url_ext not in exts:
        base = ref["url"][: -len(url_ext)] if url_ext else ref["url"]
        candidates += [base + ext for ext in exts]
    for url in candidates:
        ext = Path(url.split("?")[0]).suffix.lower()
        ext = ext if ext in exts else exts[0]
        dest = case_dir / f"ref_{index:02d}_{ref['label'].replace(' ', '')}{ext}"
        got = download(url, dest, retries=1 if len(candidates) > 1 else 3, quiet=len(candidates) > 1)
        if got:
            if url != ref["url"]:
                _log(f"  ⚠ {ref['label']} 原 URL 后缀与标签不符，改用 {ext} 成功")
            return got
    _log(f"  ✗ 下载失败 {ref['label']}（试过 {len(candidates)} 个后缀）")
    return None


def write_case_meta(case: dict[str, Any], case_dir: Path) -> None:
    """prompt 与素材落在同一个 cache 目录，缓存目录自身即可读、可核对。

    `prompt.txt` 给人看；`case.json` 记录 label → 本地文件名的映射，用来核对
    prompt 里的「参考 Image 4」到底落成了哪个文件（对齐错了模型就看错素材）。
    """
    case_dir.mkdir(parents=True, exist_ok=True)
    (case_dir / "prompt.txt").write_text(case["prompt"] + "\n", encoding="utf-8")
    meta = {
        "case_id": case["case_id"],
        "prompt": case["prompt"],
        "refs": [
            {
                "label": ref["label"],
                "kind": ref["kind"],
                "file": Path(ref["path"]).name if ref.get("path") else None,
                "url": ref["url"],
            }
            for ref in case["refs"]
        ],
        "results": [
            {
                "index": index,
                "file": path.name if path else None,
                "url": url,
            }
            for index, (path, url) in enumerate(zip(case["result_paths"], case["results"]), start=1)
        ],
    }
    (case_dir / "case.json").write_text(
        json.dumps(meta, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )


def materialize(case: dict[str, Any], *, workers: int = 8) -> dict[str, Any]:
    """把一个 case 的所有 URL 落到本地，返回带本地路径的 case。"""
    case_dir = CACHE_DIR / _safe_name(case["case_id"])
    jobs: list[tuple[str, Path, dict[str, Any] | None, str]] = []
    for index, url in enumerate(case["results"], start=1):
        jobs.append((url, case_dir / f"result_{index:02d}{_ext(url, '.mp4')}", None, ""))

    results_local: list[Path | None] = [None] * len(case["results"])
    with ThreadPoolExecutor(max_workers=workers) as pool:
        futures = {pool.submit(download, url, dest): (dest, ref) for url, dest, ref, _ in jobs}
        ref_futures = {
            pool.submit(download_ref, ref, case_dir, index): ref
            for index, ref in enumerate(case["refs"], start=1)
        }
        for future in futures:
            dest, ref = futures[future]
            got = future.result()
            if ref is not None:
                ref["path"] = str(got) if got else None
        for future, ref in ref_futures.items():
            got = future.result()
            ref["path"] = str(got) if got else None
    for index in range(len(case["results"])):
        candidate = case_dir / f"result_{index + 1:02d}{_ext(case['results'][index], '.mp4')}"
        results_local[index] = candidate if candidate.exists() else None

    case["refs"] = [r for r in case["refs"] if r.get("path")]
    case["result_paths"] = results_local
    write_case_meta(case, case_dir)
    return case


def cmd_download(cases: list[dict[str, Any]]) -> None:
    total_refs = total_videos = 0
    for index, case in enumerate(cases, start=1):
        _log(f"[{index}/{len(cases)}] {case['case_id']}  refs={len(case['refs'])} results={len(case['results'])}")
        materialize(case)
        total_refs += len(case["refs"])
        total_videos += sum(1 for p in case["result_paths"] if p)
    _log(f"\n完成：{total_refs} 个参考素材、{total_videos} 个结果视频 → {CACHE_DIR}")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--download-only", action="store_true", help="只下载素材")
    parser.add_argument("--limit", type=int, default=0, help="只处理前 N 个 case")
    parser.add_argument("--videos-per-case", type=int, default=1, help="每个 case 最多质检几个结果视频")
    parser.add_argument("--mock", action="store_true", help="不调模型的端到端自测")
    parser.add_argument("--case", default="", help="只跑指定 Case ID")
    args = parser.parse_args()

    cases = load_dataset()
    if args.case:
        cases = [c for c in cases if c["case_id"] == args.case]
    if args.limit:
        cases = cases[: args.limit]
    _log(f"数据集：{len(cases)} 个 case")

    if args.download_only:
        cmd_download(cases)
        return

    run_batch(cases, videos_per_case=args.videos_per_case, mock=args.mock)


def run_batch(cases: list[dict[str, Any]], *, videos_per_case: int = 1, mock: bool = False) -> None:
    """下载 → 合成 CaseFiles → analyze_case → 汇总。"""
    from aigc_eval.judge.native8 import summarize_native8
    from aigc_eval.judge.pipeline import RUNTIME_ROOT, analyze_case
    from aigc_eval.dataset.upload_case import build_case_files

    out_dir = RUNTIME_ROOT / "batch_0807"
    out_dir.mkdir(parents=True, exist_ok=True)
    summary_path = out_dir / "summary.jsonl"

    for index, case in enumerate(cases, start=1):
        _log(f"\n=== [{index}/{len(cases)}] {case['case_id']} ===")
        materialize(case)
        videos = [p for p in case["result_paths"] if p][:videos_per_case]
        if not videos:
            _log("  跳过：无可用结果视频")
            continue
        for video_index, video_path in enumerate(videos, start=1):
            label = f"{case['case_id']}#{video_index}"
            try:
                case_files = build_case_files(
                    case_id=label,
                    prompt=case["prompt"],
                    video_path=video_path,
                    references=case["refs"],
                )
                started = time.time()
                report = analyze_case(
                    case_files.case_id,
                    case_files=case_files,
                    use_vlm=not mock,
                    provider="agentk",
                    allow_fallback=mock,
                    source="upload",
                    on_event=lambda e: (
                        _log(f"    · {e.get('title') or e.get('id')} [{e.get('status')}]")
                        if e.get("type") == "stage" and e.get("status") != "running"
                        else None
                    ),
                )
                row = {
                    "case_id": case["case_id"],
                    "video_index": video_index,
                    "mode": (report.get("case") or {}).get("mode"),
                    **summarize_native8(report),
                    "refs": len(case["refs"]),
                    "ref_labels": [r["label"] for r in case["refs"]],
                    "cost": (report.get("cost") or {}).get("price"),
                    "report_html": report.get("report_html_path"),
                    "elapsed_s": round(time.time() - started, 1),
                }
                _log(f"  ✓ {label}: 违规 {row['violated_subcriteria']} 条 · "
                     f"未核实 {row['unverified_dimensions']}/{row['applicable_dimensions']} 维 · "
                     f"待复核 {row['review_required_count']} · "
                     f"cost={row['cost']} {row['elapsed_s']}s")
            except Exception as error:  # noqa: BLE001 - 单条失败不能打断整批
                row = {"case_id": case["case_id"], "video_index": video_index, "error": str(error)}
                _log(f"  ✗ {label}: {error}")
            with summary_path.open("a", encoding="utf-8") as handle:
                handle.write(json.dumps(row, ensure_ascii=False) + "\n")

    _log(f"\n汇总 → {summary_path}")


if __name__ == "__main__":
    main()
