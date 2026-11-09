"""素材的编码与统计：MIME 判定、base64 内联、请求模态盘点。

这些函数与具体 provider 无关，判定链路（media_parts、stages）和 agentk 通道都在用。
它们原先住在 gemini.py 里，是 Gemini 直连通道还在时的历史布局；通道下线后搬到这里，
避免"删一条传输链路要连带动判定核心"。
"""

from __future__ import annotations

import base64
from pathlib import Path
from typing import Any

# 单一真源。此前 gemini.py 与 blob_store.py 各存了一份，靠注释互相提醒同步。
# 两处的兜底值不同，所以兜底由调用方显式传入，不写进表里。
_MIME_BY_SUFFIX = {
    ".jpg": "image/jpeg",
    ".jpeg": "image/jpeg",
    ".png": "image/png",
    ".webp": "image/webp",
    ".gif": "image/gif",
    ".mp4": "video/mp4",
    ".mov": "video/quicktime",
    ".webm": "video/webm",
    ".mp3": "audio/mpeg",
    ".wav": "audio/wav",
    ".m4a": "audio/mp4",
    ".aac": "audio/aac",
    ".flac": "audio/flac",
    ".ogg": "audio/ogg",
}


def guess_mime(suffix: str, *, default: str = "application/octet-stream") -> str:
    """按后缀猜 MIME。

    兜底值必须由调用方决定：对象存储写错 Content-Type 会让上游模型拒识
    （故用 application/octet-stream 暴露问题），而内联编码时未知后缀基本都是帧图
    （故用 image/jpeg 让请求还能成立）。
    """
    return _MIME_BY_SUFFIX.get(suffix.lower(), default)


def encode_file(path: Path) -> dict[str, str]:
    """把文件读成 inline_data 形状（base64）。"""
    return {
        "mime_type": guess_mime(path.suffix, default="image/jpeg"),
        "data": base64.b64encode(path.read_bytes()).decode("ascii"),
    }


def part_modalities(parts: list[dict[str, Any]]) -> dict[str, Any]:
    """盘点这次请求实际送了什么：文本块、图、视频、音频、字节数。

    进 trace 用于事后核账——报告里"看了几张图"这类说法要能对得上真实请求。
    """
    text_parts = images = videos = other = 0
    audios = 0
    media_bytes = 0
    for part in parts:
        if not isinstance(part, dict):
            continue
        if part.get("audio_path"):
            audios += 1
            media_bytes += _file_size(part["audio_path"])
        elif part.get("video_path"):
            # 整片视频走文件路径而非 base64，字节数按文件大小统计
            videos += 1
            media_bytes += _file_size(part["video_path"])
        elif "inline_data" in part:
            blob = part.get("inline_data") or {}
            mime = str(blob.get("mime_type") or "")
            media_bytes += int(len(blob.get("data") or "") * 3 / 4)
            if mime.startswith("video/"):
                videos += 1
            elif mime.startswith("audio/"):
                audios += 1
            elif mime.startswith("image/"):
                images += 1
            else:
                other += 1
        elif "text" in part:
            text_parts += 1
    kind = "video" if videos else "audio" if audios else "image" if images else "text"
    return {
        "input_kind": kind,
        "text_parts": text_parts,
        "images": images,
        "videos": videos,
        "audios": audios,
        "other_media": other,
        "media_bytes": media_bytes,
    }


def _file_size(path: Any) -> int:
    """取文件大小；取不到就算 0——这只是统计口径，不该让请求失败。"""
    try:
        return Path(str(path)).stat().st_size
    except OSError:
        return 0
