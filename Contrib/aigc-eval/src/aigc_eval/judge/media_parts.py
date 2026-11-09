"""把素材翻译成模型能吃的 parts：整段视频、抽帧、参考图、音频。

这一层只管「怎么装」，不管「装什么」——素材选择由 cases/integrity 决定，
判定语义由 stages/zoom_inspector 决定。

对 CaseFiles 的依赖只存在于类型标注：cases 需要本模块的 reference_kind，
若在运行时反向 import 就会成环。放进 TYPE_CHECKING 既断了环，也如实说明
本模块运行时并不需要 cases。
"""
from __future__ import annotations

import hashlib
import os
import re
import subprocess
import threading
from pathlib import Path
from typing import Any, TYPE_CHECKING

from aigc_eval.judge.integrity import detect_shot_segments, sample_frames_motion_aware
from aigc_eval.judge.layout import RUNTIME_ROOT, work_dir
from aigc_eval.judge.settings import (
    REFERENCE_BUDGET,
    REFERENCE_MAX_SIDE,
    VIDEO_FPS_DETAIL,
    VIDEO_FPS_TEMPORAL,
    VIDEO_FRAME_TOKENS_DETAIL,
    VIDEO_FRAME_TOKENS_TEMPORAL,
    VIDEO_TOKENS_TOTAL,
)
from aigc_eval.providers.media_io import encode_file

if TYPE_CHECKING:
    from aigc_eval.judge.cases import CaseFiles


def build_contact_sheet(case_files: CaseFiles, *, source: str = "kling", video_path: Path | None = None) -> str:
    video_path = video_path or case_files.video_path
    RUNTIME_ROOT.mkdir(parents=True, exist_ok=True)
    output_path = RUNTIME_ROOT / f"{case_files.slug}__{source}_contact.jpg"
    if not output_path.exists() or output_path.stat().st_mtime < video_path.stat().st_mtime:
        completed = subprocess.run(
            [
                "ffmpeg",
                "-y",
                "-i",
                str(video_path),
                "-vf",
                "fps=6,scale=200:-1,tile=6x5:padding=6:margin=6",
                "-frames:v",
                "1",
                str(output_path),
            ],
            capture_output=True,
            text=True,
            check=False,
        )
        if completed.returncode != 0:
            raise RuntimeError(f"联系表生成失败：{completed.stderr.strip()[:400]}")
    return f"./runtime/{output_path.name}"


def prepare_frame_bundle(
    case_files: CaseFiles,
    metadata: dict[str, Any],
    *,
    source: str = "kling",
    video_path: Path | None = None,
) -> dict[str, Any]:
    video_path = video_path or case_files.video_path
    frame_dir = work_dir(case_files.slug, source, "frames")
    sampling = sample_frames_motion_aware(video_path, frame_dir, metadata, max_frames=12)
    shot_segments = []
    try:
        shot_segments = detect_shot_segments(video_path, metadata)
    except Exception:  # noqa: BLE001
        shot_segments = []
    contact_url = build_contact_sheet(case_files, source=source, video_path=video_path)
    contact_path = RUNTIME_ROOT / f"{case_files.slug}__{source}_contact.jpg"
    return {
        "frames": sampling["paths"],
        "frame_meta": sampling["frames"],
        "motion": {
            "strategy": sampling["strategy"],
            "dense_count": sampling["dense_count"],
            "windows": sampling["windows"],
            "sample_count": len(sampling["motion_samples"]),
        },
        "shot_segments": shot_segments,
        "contact_path": contact_path if contact_path.exists() else None,
        "contact_sheet_url": contact_url,
    }


def video_part(
    video_path: Path,
    *,
    detail: bool = True,
    fps: float | None = None,
    frame_tokens: int | None = None,
    total_tokens: int | None = None,
) -> dict[str, Any]:
    """构造一个视频部件。detail=True 走「低 fps + 高单帧分辨率」的细节档。"""
    return {
        "video_path": str(video_path),
        "fps": fps if fps is not None else (VIDEO_FPS_DETAIL if detail else VIDEO_FPS_TEMPORAL),
        "frame_tokens_max": frame_tokens
        if frame_tokens is not None
        else (VIDEO_FRAME_TOKENS_DETAIL if detail else VIDEO_FRAME_TOKENS_TEMPORAL),
        "video_tokens_total": total_tokens if total_tokens is not None else VIDEO_TOKENS_TOTAL,
    }


def audio_part(audio_path: Path) -> dict[str, Any]:
    """参考音频部件。与 video_part 对称，由各 client 翻译成自己的表达。"""
    return {"audio_path": str(audio_path)}


def reference_kind(ref: dict[str, Any]) -> str:
    """老 case 的 reference_assets 没有 kind 字段，按后缀兜底推断。"""
    kind = str(ref.get("kind") or "").lower()
    if kind in {"image", "audio", "video"}:
        return kind
    suffix = Path(str(ref.get("path") or "")).suffix.lower()
    if suffix in {".mp3", ".wav", ".m4a", ".aac", ".flac", ".ogg"}:
        return "audio"
    if suffix in {".mp4", ".mov", ".webm", ".mkv"}:
        return "video"
    return "image"


def reference_label(ref: dict[str, Any]) -> str:
    """prompt 里「参考 Image 4」靠这个标签对回素材。"""
    return str(ref.get("label") or ref.get("reference_id") or "参考素材")


LABEL_CITE_RE = re.compile(r"\b(image|img|audio|video)\s*[-_#]?\s*(\d+)", re.I)


def cited_labels(prompt: str | None) -> set[str]:
    """prompt 里显式引用到的素材标签，如「参考 Image 4」→ {"Image 4"}。"""
    out: set[str] = set()
    for match in LABEL_CITE_RE.finditer(str(prompt or "")):
        kind = match.group(1).lower()
        kind = "image" if kind == "img" else kind
        out.add(f"{kind.title()} {int(match.group(2))}")
    return out


def select_references(
    references: list[dict[str, Any]] | None,
    *,
    stage: str,
    prompt: str | None = None,
    exclude_path: Path | None = None,
) -> list[dict[str, Any]]:
    """按阶段预算挑选存在于磁盘上的参考素材，保持原有顺序。

    预算不够时**优先保留 prompt 显式引用到的素材**：case 1.2 有 9 张图而观察器只收 8 张，
    按到达顺序截断会丢掉 prompt 里点名的「背景切换成 Image 9」，却留着没人引用的 Image 3 ——
    模型于是无法核对那条约束。所以先按「是否被引用」分层，再在层内保持原序。

    ``exclude_path`` 剔除一条已被调用方单独送出的素材。V2V 的源视频既在
    ``reference_assets`` 里、又被按「源视频 → 生成视频」的固定顺序单独送出，不排除
    就会同一段视频送两遍，而且两遍的说明词不同（一处叫判定基线、一处叫参考素材
    【Video 1】），模型很可能当成两个素材去比对。剔除必须发生在预算扣减**之前**，
    否则源视频会先占掉 video 额度。
    """
    budget = dict(REFERENCE_BUDGET.get(stage, REFERENCE_BUDGET["observe"]))
    excluded = exclude_path.resolve() if exclude_path is not None else None
    alive = [
        ref for ref in references or []
        if ref.get("path")
        and Path(str(ref["path"])).exists()
        and (excluded is None or Path(str(ref["path"])).resolve() != excluded)
    ]
    cited = cited_labels(prompt)
    ranked = sorted(
        enumerate(alive),
        key=lambda pair: (0 if reference_label(pair[1]) in cited else 1, pair[0]),
    )
    picked: list[tuple[int, dict[str, Any]]] = []
    for index, ref in ranked:
        kind = reference_kind(ref)
        if budget.get(kind, 0) <= 0:
            continue
        budget[kind] -= 1
        picked.append((index, ref))
    return [ref for _, ref in sorted(picked, key=lambda pair: pair[0])]


def reference_parts(references: list[dict[str, Any]], *, capped: bool = True) -> list[dict[str, Any]]:
    """把参考素材转成带标签的 parts。标签必须随素材一起给，否则模型无法与 prompt 对应。"""
    parts: list[dict[str, Any]] = []
    for ref in references:
        path = Path(str(ref["path"]))
        kind = reference_kind(ref)
        label = reference_label(ref)
        role = ref.get("expected_role") or ""
        parts.append({"text": f"参考素材【{label}】（{kind}，{role}）："})
        if kind == "audio":
            parts.append(audio_part(path))
        elif kind == "video":
            parts.append(video_part(path, detail=False))
        else:
            parts.append({"inline_data": encode_image_capped(path) if capped else encode_file(path)})
    return parts


def reference_digest(references: list[dict[str, Any]] | None) -> list[dict[str, str]]:
    """给纯文本阶段（验证器）用的素材清单，让它知道有哪些标签可被引用。"""
    return [
        {"label": reference_label(r), "kind": reference_kind(r),
         "role": str(r.get("expected_role") or "")}
        for r in (references or [])
        if r.get("path")
    ]


_REF_CACHE_DIR = RUNTIME_ROOT / "ref_capped"


def encode_image_capped(path: Path, max_side: int = REFERENCE_MAX_SIDE) -> dict[str, Any]:
    """Encode an image, first downscaling it if its longer edge exceeds ``max_side``.

    Input reference images are user-supplied originals — some are 4000×3000 / 15 MB
    photos. Under Gemini's flat per-image token price that was harmless, but Keye
    charges by pixel count, so one such reference costs ~15k tokens and on its own
    overflows the 32k context (observed: HTTP 400 at 29.5k input tokens, where the
    reference alone was 14.9 MB). Downscaled copies are cached on disk keyed by
    source size+mtime, so the ffmpeg call happens once per reference.
    """
    if not max_side or max_side <= 0:
        return encode_file(path)
    try:
        stat = path.stat()
    except OSError:
        return encode_file(path)
    digest = hashlib.md5(
        f"{path}|{stat.st_size}|{int(stat.st_mtime)}|{max_side}".encode("utf-8")
    ).hexdigest()[:12]
    cached = _REF_CACHE_DIR / f"{digest}.jpg"
    if not cached.exists() or cached.stat().st_size == 0:
        _REF_CACHE_DIR.mkdir(parents=True, exist_ok=True)
        # 必须先写临时文件再原子改名：ffmpeg 直接写 cached 时，并发的其他 worker
        # 会看到"文件已存在"就去读，读到的是写了一半（甚至 0 字节）的内容，
        # 于是请求里带一个空 inline_data，Gemini 直接 400 判整条 case 失败。
        # 同一批参考图被同 case 的多条 result 共享，并发一开必然撞上。
        tmp = _REF_CACHE_DIR / f"{digest}.{os.getpid()}.{threading.get_ident()}.tmp.jpg"
        completed = subprocess.run(
            ["ffmpeg", "-y", "-v", "error", "-i", str(path), "-vf",
             f"scale='min(iw,{max_side})':'min(ih,{max_side})'"
             ":force_original_aspect_ratio=decrease:flags=lanczos",
             "-q:v", "3", str(tmp)],
            capture_output=True,
        )
        ok = completed.returncode == 0 and tmp.exists() and tmp.stat().st_size > 0
        if ok:
            os.replace(tmp, cached)
        else:
            tmp.unlink(missing_ok=True)
            return encode_file(path)  # 缩放失败就退回原图，不阻断流程
    return encode_file(cached)


def _image_parts(paths: list[Path], label_prefix: str) -> list[dict[str, Any]]:
    parts: list[dict[str, Any]] = []
    for index, path in enumerate(paths):
        if not path.exists():
            continue
        parts.append({"text": f"{label_prefix} {index}:"})
        parts.append({"inline_data": encode_image_capped(path)})
    return parts


def _parse_time(value: Any) -> float | None:
    """把 '00:02.4' / '2.4s' / '00:01.0-00:01.8' / 2.4 解析成秒（区间取起点）。"""
    if isinstance(value, (int, float)):
        return float(value)
    if not isinstance(value, str) or not value.strip():
        return None
    head = value.split("-")[0].strip()
    match = re.match(r"^(?:(\d+):)?(\d+(?:\.\d+)?)\s*s?$", head)
    if not match:
        return None
    minutes = float(match.group(1) or 0)
    seconds = float(match.group(2))
    return minutes * 60 + seconds


def _keyframe_parts(frame_meta: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """Label each keyframe with its timestamp and whether it is a densified
    high-motion frame, so the observer can reason about motion continuity."""
    parts: list[dict[str, Any]] = []
    for index, item in enumerate(frame_meta):
        path = item.get("path")
        if not isinstance(path, Path) or not path.exists():
            continue
        stamp = f"{item['time']:.2f}s" if item.get("time") is not None else f"#{index}"
        tag = "（高运动加密帧）" if item.get("dense") else ""
        parts.append({"text": f"关键帧 t={stamp}{tag}:"})
        parts.append({"inline_data": encode_file(path)})
    return parts
