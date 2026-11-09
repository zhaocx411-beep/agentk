"""离线数据集(mm-datasets/*/cache)的只读浏览层。

这些 case 已经落在服务器磁盘上，目录约定是扁平的一层：

    cache/<case_id>/
        prompt.txt                 # prompt 正文（权威来源）
        case.json                  # 可选：refs 的 label→文件 映射 + 原始 url
        ref_01_Image1.jpeg ...     # 参考素材
        result_01.mp4 ...          # 待质检视频（同一 prompt 的多个生成结果）

只有一部分 case 有 `case.json`；没有的靠文件名反解 label，规则与
`static/studio.js` 的 `LABEL_RE` 保持一致 —— label 是模型把 prompt 里
「参考 Image 4」对回具体素材的唯一依据，两端解出来的编号必须一样。

素材**不复制**：`/api/upload_analyze` 拿到文件名后原地引用，避免把几十 MB
的视频先下发到浏览器再传回来。因此这里所有对外暴露的路径都必须经过
`_safe_child()` 校验，防止路径逃逸。
"""

from __future__ import annotations

import json
import mimetypes
import os
import re
import subprocess
from pathlib import Path
from typing import Any
from urllib.parse import quote

from aigc_eval import paths

# 默认数据集：按本仓库位置解析，不写死绝对路径 —— 之前写死的是
# `/mmu_mllm_hdd_4/fuhongyi/...`（少了 `02`，是另一个人的目录），
# 那个路径恰好存在，所以一直静默读错数据集而不报错。
DATASET_ROOT = Path(
    os.environ.get(
        "VIDEO_JUDGE_DATASET_ROOT",
        str(paths.DEFAULT_DATASET_CACHE),
    )
).resolve()

_KIND_LABEL = {"image": "Image", "audio": "Audio", "video": "Video"}
_AUDIO_EXT = {".mp3", ".wav", ".m4a", ".aac", ".flac", ".ogg"}
_VIDEO_EXT = {".mp4", ".mov", ".webm", ".mkv"}
_IMAGE_EXT = {".png", ".jpg", ".jpeg", ".webp"}
_MEDIA_EXT = _AUDIO_EXT | _VIDEO_EXT | _IMAGE_EXT

_CASE_ID_RE = re.compile(r"^[0-9A-Za-z._-]{1,64}$")
# 与 studio.js 的 LABEL_RE 同构：`1-2-image-4.png` / `ref_04_Image4.png` / `s2-7-img-1.png`
_LABEL_RE = re.compile(r"(?:^|[-_\s])(image|img|audio|video)[-_\s]*(\d+)", re.IGNORECASE)
_PROMPT_PREVIEW_CHARS = 140


def _kind_for_suffix(suffix: str) -> str:
    suffix = suffix.lower()
    if suffix in _AUDIO_EXT:
        return "audio"
    if suffix in _VIDEO_EXT:
        return "video"
    return "image"


def _natural_key(name: str) -> list[Any]:
    """`2.1.1` < `2.2` < `s2-2` < `s2-10`：数字段按数值比，其余按字符串。"""
    return [int(part) if part.isdigit() else part for part in re.split(r"(\d+)", name)]


def _safe_child(parent: Path, name: str) -> Path:
    """取 parent 下的直接子项，拒绝任何形式的路径逃逸。"""
    if not name or "/" in name or "\\" in name or name in {".", ".."}:
        raise ValueError(f"非法文件名：{name}")
    target = (parent / name).resolve()
    if target.parent != parent.resolve():
        raise ValueError(f"非法文件名：{name}")
    return target


def dataset_case_dir(case_id: str) -> Path:
    if not _CASE_ID_RE.match(case_id or ""):
        raise ValueError(f"非法 case_id：{case_id}")
    case_dir = _safe_child(DATASET_ROOT, case_id)
    if not case_dir.is_dir():
        raise FileNotFoundError(f"离线数据集无此 case：{case_id}")
    return case_dir


def dataset_asset_path(case_id: str, filename: str) -> Path:
    """给 `/api/upload_analyze` 用：把 (case_id, 文件名) 解成磁盘上的真实路径。"""
    target = _safe_child(dataset_case_dir(case_id), filename)
    if not target.is_file():
        raise FileNotFoundError(f"素材不存在：{case_id}/{filename}")
    if target.suffix.lower() not in _MEDIA_EXT:
        raise ValueError(f"非媒体文件：{filename}")
    return target


def dataset_media_path(case_id: str, filename: str) -> tuple[Path, str]:
    target = dataset_asset_path(case_id, filename)
    return target, mimetypes.guess_type(target.name)[0] or "application/octet-stream"


def _media_url(case_id: str, filename: str) -> str:
    return f"./api/dataset_media?case_id={quote(case_id, safe='')}&file={quote(filename, safe='')}"


def _thumb_url(case_id: str, filename: str) -> str:
    return f"./api/dataset_thumb?case_id={quote(case_id, safe='')}&file={quote(filename, safe='')}"


def _load_prompt(case_dir: Path, manifest: dict[str, Any]) -> str:
    """prompt.txt 优先：批处理脚本会单独修订它，比 case.json 里的副本新。"""
    prompt_file = case_dir / "prompt.txt"
    if prompt_file.is_file():
        text = prompt_file.read_text(encoding="utf-8", errors="replace").strip()
        if text:
            return text
    return str(manifest.get("prompt") or "").strip()


def _load_manifest(case_dir: Path) -> dict[str, Any]:
    path = case_dir / "case.json"
    if not path.is_file():
        return {}
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (json.JSONDecodeError, OSError):
        return {}
    return data if isinstance(data, dict) else {}


def _sniff_label(filename: str, kind: str) -> str:
    match = _LABEL_RE.search(filename)
    if not match:
        return ""
    sniffed = match.group(1).lower()
    if sniffed == "img":
        sniffed = "image"
    if sniffed != kind:
        return ""
    return f"{_KIND_LABEL[kind]} {int(match.group(2))}"


def _collect(case_dir: Path) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    """扫出 (参考素材, 待质检视频)。

    `result_*` 是生成结果（同一 prompt 常有多条），其余媒体文件都当参考素材。
    """
    manifest = _load_manifest(case_dir)
    # case.json 里的 label 是人工确认过的，优先于文件名反解。
    declared = {
        str(ref.get("file")): str(ref.get("label") or "")
        for ref in manifest.get("refs") or []
        if isinstance(ref, dict) and ref.get("file")
    }

    refs: list[dict[str, Any]] = []
    results: list[dict[str, Any]] = []
    counters = {"image": 0, "audio": 0, "video": 0}

    for path in sorted(case_dir.iterdir(), key=lambda p: _natural_key(p.name)):
        if not path.is_file() or path.suffix.lower() not in _MEDIA_EXT:
            continue
        entry = {
            "file": path.name,
            "size": path.stat().st_size,
            "url": _media_url(case_dir.name, path.name),
            "thumb": _thumb_url(case_dir.name, path.name),
        }
        if path.name.lower().startswith("result"):
            results.append(entry)
            continue
        kind = _kind_for_suffix(path.suffix)
        counters[kind] += 1
        label = declared.get(path.name) or _sniff_label(path.name, kind)
        refs.append({
            **entry,
            "kind": kind,
            # 兜底编号只在文件名不带 Image/Audio/Video 序号时才用到。
            "label": label or f"{_KIND_LABEL[kind]} {counters[kind]}",
        })

    order = {"image": 0, "audio": 1, "video": 2}
    refs.sort(key=lambda r: (order[r["kind"]], _natural_key(r["label"])))
    return refs, results


def list_dataset_cases() -> list[dict[str, Any]]:
    """列表页用的轻量摘要：不读任何媒体文件内容。"""
    if not DATASET_ROOT.is_dir():
        return []
    cases: list[dict[str, Any]] = []
    for case_dir in sorted(
        (p for p in DATASET_ROOT.iterdir() if p.is_dir()), key=lambda p: _natural_key(p.name)
    ):
        try:
            refs, results = _collect(case_dir)
        except OSError:
            continue
        if not results:
            continue  # 没有待质检视频的目录进不了质检流程，不展示
        prompt = _load_prompt(case_dir, _load_manifest(case_dir))
        counts = {"image": 0, "audio": 0, "video": 0}
        for ref in refs:
            counts[ref["kind"]] += 1
        cases.append({
            "case_id": case_dir.name,
            "prompt_preview": prompt[:_PROMPT_PREVIEW_CHARS],
            "prompt_chars": len(prompt),
            "ref_count": len(refs),
            "result_count": len(results),
            "ref_kinds": counts,
            "mode": "I2VA" if counts["image"] else "T2VA",
        })
    return cases


def dataset_case_detail(case_id: str) -> dict[str, Any]:
    """详情页用：prompt 全文 + 每个素材的 label/kind/预览 URL。"""
    case_dir = dataset_case_dir(case_id)
    refs, results = _collect(case_dir)
    if not results:
        raise FileNotFoundError(f"该 case 没有待质检视频：{case_id}")
    return {
        "case_id": case_id,
        "prompt": _load_prompt(case_dir, _load_manifest(case_dir)),
        "refs": refs,
        "results": results,
        "mode": "I2VA" if any(r["kind"] == "image" for r in refs) else "T2VA",
    }


# ---------------------------------------------------------------------------
# 缩略图 / 视频海报
#
# 列表里的卡片只有 76×56 px，直传原图是纯粹的浪费：一张参考图 4 MB，展开一条
# 12 素材的 case 就要 28.7 MB，而这些字节还要绕 code-server 代理回到浏览器 ——
# 这正是"每次读取数据都很卡"的主因。转成 320px JPEG 后单张约 13 KB（降 99.7%）。
#
# 缩略图按 (源文件 mtime, 尺寸) 命名落盘，源文件不变就永远命中缓存不重算。
# ---------------------------------------------------------------------------
THUMB_ROOT = paths.THUMB_ROOT
THUMB_SIDE = int(os.environ.get("VIDEO_JUDGE_THUMB_SIDE", "320"))
_FFMPEG_TIMEOUT = 20


def _thumb_cache_path(source: Path, side: int) -> Path:
    """按源文件 mtime 命名：源文件被替换则自动生成新缩略图，无需手动清缓存。"""
    stat = source.stat()
    stem = re.sub(r"[^0-9A-Za-z_.-]", "_", source.stem)[:40]
    return THUMB_ROOT / f"{stem}_{int(stat.st_mtime)}_{stat.st_size}_{side}.jpg"


def _render_image_thumb(source: Path, dest: Path, side: int) -> bool:
    try:
        from PIL import Image
    except ImportError:
        return False
    try:
        with Image.open(source) as im:
            # draft() 让 JPEG 在解码阶段就按比例缩小，比先全解码再缩快数倍。
            im.draft("RGB", (side, side))
            im = im.convert("RGB")
            im.thumbnail((side, side), Image.LANCZOS)
            im.save(dest, "JPEG", quality=78, optimize=True)
        return True
    except Exception:  # noqa: BLE001 - 缩略图失败不该影响浏览，调用方回退原图
        dest.unlink(missing_ok=True)
        return False


def _render_video_poster(source: Path, dest: Path, side: int) -> bool:
    # 取 0.5s 而非首帧：不少生成视频第一帧是纯黑，做海报没有信息量。
    command = [
        "ffmpeg", "-v", "error", "-ss", "0.5", "-i", str(source),
        "-frames:v", "1", "-vf", f"scale={side}:-2", "-q:v", "5", "-y", str(dest),
    ]
    try:
        subprocess.run(command, check=True, capture_output=True, timeout=_FFMPEG_TIMEOUT)
        return dest.exists() and dest.stat().st_size > 0
    except Exception:  # noqa: BLE001
        dest.unlink(missing_ok=True)
        return False


def dataset_thumb_path(case_id: str, filename: str, side: int | None = None) -> tuple[Path, str]:
    """返回可直接发送的缩略图路径。

    图片走 Pillow，视频抽首帧海报；音频没有可视内容，直接抛 404 让前端用图标占位。
    生成失败时回退原文件 —— 宁可慢一次，也不要在列表里开天窗。
    """
    source = dataset_asset_path(case_id, filename)
    kind = _kind_for_suffix(source.suffix)
    if kind == "audio":
        raise FileNotFoundError("音频无缩略图")

    side = side or THUMB_SIDE
    dest = _thumb_cache_path(source, side)
    if dest.exists() and dest.stat().st_size > 0:
        return dest, "image/jpeg"

    THUMB_ROOT.mkdir(parents=True, exist_ok=True)
    ok = _render_image_thumb(source, dest, side) if kind == "image" else _render_video_poster(source, dest, side)
    if ok:
        return dest, "image/jpeg"
    return source, mimetypes.guess_type(source.name)[0] or "application/octet-stream"
