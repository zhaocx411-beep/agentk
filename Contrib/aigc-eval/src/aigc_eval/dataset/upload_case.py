"""上传态输入：合成 CaseFiles + 流式 multipart 解析。

原有输入是**身份寻址**的：`get_case_files()` 要求目录位于 `PROJECT_ROOT/{I2VA*,T2VA*}/<case>/`
且含 `prompt.json` + `Kling*.mp4`，越界直接 `raise ValueError("非法 case_id")`。上传来的素材
没有 case 目录，所以这里在 `runtime/uploads/<slug>/` 下**造一个满足契约的目录**，再产出一个
真正的 `CaseFiles` —— 七步 pipeline 全部吃 `Path`，因此无需 fork `analyze_case`。

slug 走**内容寻址**（视频 + prompt + 各素材的哈希），同一批素材重复提交自动命中缓存。
"""

from __future__ import annotations

import hashlib
import json
import mimetypes
import re
import shutil
from pathlib import Path
from typing import Any, BinaryIO

from aigc_eval.judge.pipeline import RUNTIME_ROOT, CaseFiles


UPLOAD_ROOT = RUNTIME_ROOT / "uploads"

_KIND_LABEL = {"image": "Image", "audio": "Audio", "video": "Video"}
_IMAGE_EXT = {".png", ".jpg", ".jpeg", ".webp"}
_AUDIO_EXT = {".mp3", ".wav", ".m4a", ".aac", ".flac", ".ogg"}
_VIDEO_EXT = {".mp4", ".mov", ".webm", ".mkv"}


def kind_for_suffix(suffix: str) -> str:
    suffix = suffix.lower()
    if suffix in _AUDIO_EXT:
        return "audio"
    if suffix in _VIDEO_EXT:
        return "video"
    return "image"


def _file_digest(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        while chunk := handle.read(1 << 20):
            digest.update(chunk)
    return digest.hexdigest()


def normalize_prompt(prompt: str) -> str:
    """把 prompt 归一到 LF，供 slug 哈希用。

    HTML 规定 `<textarea>` 的值在 multipart 里按 CRLF 传（RFC 7578 的 normalize
    line breaks），而磁盘上的 prompt.txt 是 LF。同一条多行 prompt 因此有两个哈希：
    浏览器提交算出 CRLF 版、批处理脚本算出 LF 版，缓存永远互不命中 ——
    表现就是「单行 prompt 的 case 能命中缓存，多行的每次都重跑」。
    """
    return prompt.replace("\r\n", "\n").replace("\r", "\n")


def content_slug(prompt: str, video_path: Path, references: list[dict[str, Any]]) -> str:
    """内容寻址 slug：素材不变 → slug 不变 → 命中 runtime/results 缓存。"""
    digest = hashlib.sha256()
    digest.update(normalize_prompt(prompt).encode("utf-8"))
    digest.update(_file_digest(video_path).encode("ascii"))
    for ref in sorted(references, key=lambda r: str(r.get("label") or "")):
        path = ref.get("path")
        if not path:
            continue
        digest.update(str(ref.get("label") or "").encode("utf-8"))
        digest.update(_file_digest(Path(path)).encode("ascii"))
    return "up_" + digest.hexdigest()[:16]


def normalize_references(references: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """补齐 kind/label，并按 Image→Audio→Video、序号升序排列。

    label 是模型把「参考 Image 4」对到正确素材的唯一依据，缺失时按 kind 顺序补号。
    """
    counters = {"image": 0, "audio": 0, "video": 0}
    out: list[dict[str, Any]] = []
    for ref in references:
        path = ref.get("path")
        if not path or not Path(path).exists():
            continue
        kind = str(ref.get("kind") or kind_for_suffix(Path(path).suffix)).lower()
        if kind not in counters:
            kind = "image"
        counters[kind] += 1
        label = str(ref.get("label") or "").strip() or f"{_KIND_LABEL[kind]} {counters[kind]}"
        out.append(
            {
                "reference_id": label,
                "kind": kind,
                "label": label,
                "path": str(path),
                "expected_role": str(ref.get("expected_role") or _default_role(kind)),
                "required": bool(ref.get("required", True)),
            }
        )
    order = {"image": 0, "audio": 1, "video": 2}

    def sort_key(ref: dict[str, Any]) -> tuple[int, int, str]:
        match = re.search(r"(\d+)", ref["label"])
        return (order[ref["kind"]], int(match.group(1)) if match else 0, ref["label"])

    out.sort(key=sort_key)
    return out


def _default_role(kind: str) -> str:
    return {
        "image": "主体/外观参考",
        "audio": "声音参考（音色、语调、配乐风格）",
        "video": "运镜/动态参考",
    }[kind]


def build_case_files(
    *,
    case_id: str,
    prompt: str,
    video_path: Path,
    references: list[dict[str, Any]],
    work_dir: Path | None = None,
) -> CaseFiles:
    """把上传素材落成一个满足 pipeline 契约的合成 case。

    参考素材**不复制**（可能几十 MB），直接引用原路径；只有 prompt.json 需要写盘，
    因为 `load_prompt()` 会去读它。
    """
    video_path = Path(video_path)
    if not video_path.exists():
        raise FileNotFoundError(f"视频不存在：{video_path}")
    normalized = normalize_references(references)
    slug = content_slug(prompt, video_path, normalized)
    case_dir = Path(work_dir) if work_dir else (UPLOAD_ROOT / slug)
    case_dir.mkdir(parents=True, exist_ok=True)

    prompt_path = case_dir / "prompt.json"
    prompt_path.write_text(
        json.dumps({"prompt_zh": prompt, "prompt_en": "", "gold_label": "", "gold_note": ""},
                   ensure_ascii=False, indent=2),
        encoding="utf-8",
    )

    # manifest 是 /api/upload_media 唯一的取用依据：素材不复制进 case_dir，
    # 所以必须留下一张「slug → 真实路径」的表，报告里的媒体链接才解得开。
    (case_dir / "manifest.json").write_text(
        json.dumps(
            {
                "case_id": case_id,
                "slug": slug,
                "video": str(video_path),
                "references": [
                    {k: r[k] for k in ("reference_id", "kind", "label", "path", "expected_role")}
                    for r in normalized
                ],
            },
            ensure_ascii=False,
            indent=2,
        ),
        encoding="utf-8",
    )

    images = [r for r in normalized if r["kind"] == "image"]
    case_files = CaseFiles(
        case_id=case_id,
        collection="uploads",
        case_name=case_id,
        case_dir=case_dir,
        prompt_path=prompt_path,
        video_path=video_path,
        seedance_path=None,
        input_image_path=Path(images[0]["path"]) if images else None,
        reference_assets=normalized,
    )
    # slug 由 CaseFiles.__init__ 从 case_id 推导，这里覆盖为内容寻址值。
    case_files.slug = slug
    return case_files


def finalize_staging(
    staging_dir: Path,
    *,
    prompt: str,
    video_path: Path,
    references: list[dict[str, Any]],
) -> tuple[Path, list[dict[str, Any]], str]:
    """把临时落盘的素材迁到内容寻址目录，返回 (视频路径, 参考素材, slug)。

    先补齐 label 再算 slug（label 参与哈希），然后整体搬到 `runtime/uploads/<slug>/assets/`。
    同一批素材重复提交 → 同一 slug → 同一目录，天然幂等且不会堆积临时目录。
    """
    normalized = normalize_references(references)
    slug = content_slug(prompt, video_path, normalized)
    assets = UPLOAD_ROOT / slug / "assets"
    assets.mkdir(parents=True, exist_ok=True)
    staging_resolved = staging_dir.resolve()

    def relocate(src: Path) -> Path:
        dest = assets / src.name
        if dest.resolve() == src.resolve():
            return dest
        # 只搬 staging 内的临时文件。复用离线数据集时素材是原地引用的，
        # 搬走会直接破坏数据集本身，所以非 staging 的路径一律原样返回。
        if staging_resolved not in src.resolve().parents:
            return src
        if dest.exists():
            dest.unlink()
        shutil.move(str(src), str(dest))
        return dest

    video_final = relocate(Path(video_path))
    for ref in normalized:
        ref["path"] = str(relocate(Path(ref["path"])))
    shutil.rmtree(staging_dir, ignore_errors=True)
    return video_final, normalized, slug


def upload_media_path(slug: str, asset: str, ref_id: str | None = None) -> tuple[Path, str]:
    """按 slug 取上传态素材，供 /api/upload_media 回显与报告内引用。

    slug 是内容哈希（`up_` + 16 hex），先做白名单校验再落到 manifest，
    避免任何形式的路径逃逸。
    """
    if not re.fullmatch(r"up_[0-9a-f]{16}", slug or ""):
        raise ValueError("非法 slug")
    manifest_path = UPLOAD_ROOT / slug / "manifest.json"
    if not manifest_path.exists():
        raise FileNotFoundError(f"上传素材不存在：{slug}")
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))

    if asset == "video":
        target = Path(str(manifest.get("video") or ""))
    elif asset == "reference":
        refs = [r for r in manifest.get("references") or [] if isinstance(r, dict)]
        if not refs:
            raise FileNotFoundError("该上传无参考素材")
        if ref_id in (None, ""):
            # 没指定就给第一个（老报告里 input_image_url 的兼容取法）。
            picked = refs[0]
        else:
            # 指定了却找不到，绝不能退回第一个：报告里写着「Image 9」却显示 Image 4
            # 属于静默错配，比 404 更难发现。
            picked = next((r for r in refs if str(r.get("reference_id")) == str(ref_id)), None)
            if picked is None:
                raise FileNotFoundError(f"参考素材不存在：{ref_id}")
        target = Path(str(picked.get("path") or ""))
    else:
        raise ValueError(f"未知素材类型：{asset}")

    if not target.exists():
        raise FileNotFoundError(f"素材文件已不存在：{target.name}")
    return target, mimetypes.guess_type(target.name)[0] or "application/octet-stream"


def build_upload_case_files(
    *,
    prompt: str,
    video_path: Path,
    references: list[dict[str, Any]],
) -> CaseFiles:
    """上传流的默认入口：case_id 即内容哈希 slug，无需调用方自己编号。"""
    normalized = normalize_references(references)
    slug = content_slug(prompt, Path(video_path), normalized)
    return build_case_files(
        case_id=slug,
        prompt=prompt,
        video_path=Path(video_path),
        references=normalized,
        work_dir=UPLOAD_ROOT / slug,
    )


def save_upload(stream: BinaryIO, dest: Path, *, max_bytes: int) -> int:
    """流式写盘，超限即中止（不把整个视频读进内存）。"""
    dest.parent.mkdir(parents=True, exist_ok=True)
    written = 0
    with dest.open("wb") as handle:
        while chunk := stream.read(1 << 20):
            written += len(chunk)
            if written > max_bytes:
                handle.close()
                dest.unlink(missing_ok=True)
                raise ValueError(f"文件超过上限 {max_bytes // (1 << 20)}MB")
            handle.write(chunk)
    return written


# ---------------------------------------------------------------------------
# multipart/form-data 解析
#
# Python 3.13 删掉了 `cgi` 模块，而 `email.parser` 需要把整个 body 读进内存 ——
# 视频动辄 15MB+ 且可多文件，所以这里按 boundary 边读边写盘，内存占用恒定。
# ---------------------------------------------------------------------------


class MultipartError(ValueError):
    pass


def _parse_headers(block: bytes) -> dict[str, str]:
    headers: dict[str, str] = {}
    for line in block.split(b"\r\n"):
        if not line or b":" not in line:
            continue
        name, _, value = line.partition(b":")
        headers[name.decode("latin-1").strip().lower()] = value.decode("utf-8", "replace").strip()
    return headers


def _content_disposition(value: str) -> tuple[str, str]:
    name = re.search(r'name="([^"]*)"', value)
    filename = re.search(r'filename="([^"]*)"', value)
    return (name.group(1) if name else "", filename.group(1) if filename else "")


def parse_multipart(
    rfile: BinaryIO,
    content_type: str,
    content_length: int,
    dest_dir: Path,
    *,
    max_total_bytes: int = 600 * (1 << 20),
    max_field_bytes: int = 1 << 20,
) -> tuple[dict[str, str], list[dict[str, Any]]]:
    """返回 (文本字段, 文件列表)。文件已落盘到 dest_dir。

    文件项形如 {"field","filename","path","size"}。
    """
    match = re.search(r'boundary="?([^";]+)"?', content_type or "")
    if not match:
        raise MultipartError("缺少 multipart boundary")
    if content_length <= 0:
        raise MultipartError("空请求体")
    if content_length > max_total_bytes:
        raise MultipartError(f"请求体超过上限 {max_total_bytes // (1 << 20)}MB")

    delimiter = b"--" + match.group(1).encode("latin-1")
    dest_dir.mkdir(parents=True, exist_ok=True)

    fields: dict[str, str] = {}
    files: list[dict[str, Any]] = []

    remaining = content_length
    buffer = b""
    chunk_size = 1 << 20
    # 缓冲区里始终保留至少 len(delimiter)+4 字节，避免 boundary 被切成两半而漏判。
    keep = len(delimiter) + 4

    def fill() -> bool:
        nonlocal buffer, remaining
        if remaining <= 0:
            return False
        chunk = rfile.read(min(chunk_size, remaining))
        if not chunk:
            remaining = 0
            return False
        remaining -= len(chunk)
        buffer += chunk
        return True

    # 定位到第一个 boundary
    while delimiter not in buffer:
        if not fill():
            raise MultipartError("未找到 multipart 起始边界")
    buffer = buffer.split(delimiter, 1)[1]

    file_index = 0
    while True:
        if buffer.startswith(b"--"):
            break  # 结束边界
        # 读到 header/body 分隔
        while b"\r\n\r\n" not in buffer:
            if not fill():
                raise MultipartError("multipart 段头不完整")
        raw_headers, buffer = buffer.split(b"\r\n\r\n", 1)
        headers = _parse_headers(raw_headers.lstrip(b"\r\n"))
        field, filename = _content_disposition(headers.get("content-disposition", ""))

        handle = None
        target: Path | None = None
        if filename:
            file_index += 1
            suffix = Path(filename).suffix.lower()[:10] or ".bin"
            safe = re.sub(r"[^0-9A-Za-z_.-]", "_", Path(filename).stem)[:40] or "file"
            target = dest_dir / f"{file_index:02d}_{safe}{suffix}"
            handle = target.open("wb")
        collected = bytearray()
        size = 0

        try:
            while True:
                index = buffer.find(delimiter)
                if index >= 0:
                    body, buffer = buffer[:index], buffer[index + len(delimiter):]
                    # 段尾固定以 \r\n 结束，剥掉它才是真实内容
                    if body.endswith(b"\r\n"):
                        body = body[:-2]
                    if handle:
                        handle.write(body)
                        size += len(body)
                    else:
                        collected += body
                    break
                # 未见 boundary：冲刷除尾部 keep 字节以外的内容
                if len(buffer) > keep:
                    flush, buffer = buffer[:-keep], buffer[-keep:]
                    if handle:
                        handle.write(flush)
                        size += len(flush)
                    else:
                        collected += flush
                        if len(collected) > max_field_bytes:
                            raise MultipartError(f"字段 {field} 过大")
                if not fill():
                    # 流已尽，剩余即最后一段内容
                    if handle:
                        handle.write(buffer)
                        size += len(buffer)
                    else:
                        collected += buffer
                    buffer = b"--"
                    break
        finally:
            if handle:
                handle.close()

        if filename and target is not None:
            if size == 0:
                target.unlink(missing_ok=True)
            else:
                files.append({"field": field, "filename": filename, "path": str(target), "size": size})
        elif field:
            fields[field] = collected.decode("utf-8", "replace")

        if buffer.startswith(b"\r\n"):
            buffer = buffer[2:]
        if buffer.startswith(b"--") or (remaining <= 0 and not buffer.strip(b"-\r\n")):
            break
        while len(buffer) < 2 and fill():
            pass
        if not buffer:
            break

    return fields, files


def cleanup_upload(job_dir: Path) -> None:
    shutil.rmtree(job_dir, ignore_errors=True)
