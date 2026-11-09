from __future__ import annotations

import asyncio
import hashlib
import os
import tempfile
from collections.abc import Iterator
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Protocol
from urllib.parse import quote

import aiofiles
import httpx

from narrato_agentk.contracts import MAX_VIDEO_BYTES

CHUNK_BYTES = 1 << 20
ALLOWED_VIDEO_TYPES = {
    "video/mp4": "mp4",
    "video/quicktime": "mov",
    "video/x-matroska": "mkv",
    "video/webm": "webm",
}


class UploadReader(Protocol):
    async def read(self, size: int = -1) -> bytes: ...


class UploadBackend(Protocol):
    async def exists(self, key: str) -> bool: ...

    async def put_file(self, key: str, path: Path, content_type: str, size: int) -> None: ...

    def uri(self, key: str) -> str: ...


class UploadRejected(ValueError):
    pass


@dataclass(frozen=True, slots=True)
class UploadResult:
    uri: str
    sha256: str
    sizeBytes: int
    contentType: str

    def to_dict(self) -> dict[str, object]:
        return asdict(self)


class BS3Backend:
    def __init__(
        self,
        *,
        endpoint: str,
        bucket: str,
        cdn_base: str,
        timeout_seconds: float = 600,
    ) -> None:
        self.endpoint = endpoint.rstrip("/")
        self.bucket = bucket.strip("/")
        self.cdn_base = cdn_base.rstrip("/")
        self.timeout_seconds = timeout_seconds
        if not self.endpoint or not self.bucket or not self.cdn_base:
            raise ValueError("BS3 endpoint, bucket and CDN base are required")

    async def exists(self, key: str) -> bool:
        return await asyncio.to_thread(self._exists, key)

    def _exists(self, key: str) -> bool:
        with httpx.Client(timeout=20) as client:
            response = client.head(self._object_url(key))
            if response.status_code == 404:
                return False
            response.raise_for_status()
            return True

    async def put_file(self, key: str, path: Path, content_type: str, size: int) -> None:
        await asyncio.to_thread(self._put_file, key, path, content_type, size)

    def _put_file(self, key: str, path: Path, content_type: str, size: int) -> None:
        with path.open("rb") as handle, httpx.Client(timeout=self.timeout_seconds) as client:
            response = client.put(
                self._object_url(key),
                content=_read_chunks(handle),
                headers={"Content-Length": str(size), "Content-Type": content_type},
            )
            response.raise_for_status()

    def uri(self, key: str) -> str:
        return f"{self.cdn_base}/{_quote_key(key)}"

    def _object_url(self, key: str) -> str:
        return f"{self.endpoint}/{quote(self.bucket, safe='')}/{_quote_key(key)}"


class MediaUploadService:
    def __init__(self, backend: UploadBackend, *, key_prefix: str = "narrato-agentk") -> None:
        self.backend = backend
        self.key_prefix = key_prefix.strip("/")

    async def upload(
        self,
        reader: UploadReader,
        content_type: str,
        filename: str = "",
    ) -> UploadResult:
        normalized_type = content_type.split(";", 1)[0].strip().lower()
        extension = ALLOWED_VIDEO_TYPES.get(normalized_type)
        if extension is None:
            raise UploadRejected(f"unsupported video content type: {content_type or '(missing)'}")
        if filename:
            suffix = Path(filename).suffix.lower().lstrip(".")
            if suffix != extension:
                raise UploadRejected(
                    f"video extension .{suffix or '(missing)'} does not match {normalized_type}"
                )

        digest = hashlib.sha256()
        size = 0
        fd, raw_path = tempfile.mkstemp(prefix="narrato-agentk-upload-", suffix=f".{extension}")
        os.close(fd)
        temp_path = Path(raw_path)
        try:
            async with aiofiles.open(temp_path, "wb") as handle:
                while True:
                    chunk = await reader.read(CHUNK_BYTES)
                    if not chunk:
                        break
                    size += len(chunk)
                    if size > MAX_VIDEO_BYTES:
                        raise UploadRejected(f"video exceeds {MAX_VIDEO_BYTES} bytes")
                    digest.update(chunk)
                    await handle.write(chunk)
            if size == 0:
                raise UploadRejected("video is empty")
            sha256 = digest.hexdigest()
            parts = [part for part in (self.key_prefix, "uploads") if part]
            key = "/".join([*parts, f"{sha256}.{extension}"])
            if not await self.backend.exists(key):
                await self.backend.put_file(key, temp_path, normalized_type, size)
            return UploadResult(
                uri=self.backend.uri(key),
                sha256=sha256,
                sizeBytes=size,
                contentType=normalized_type,
            )
        finally:
            temp_path.unlink(missing_ok=True)


def _read_chunks(handle: object) -> Iterator[bytes]:
    while True:
        chunk = handle.read(CHUNK_BYTES)
        if not chunk:
            return
        yield chunk


def _quote_key(key: str) -> str:
    return "/".join(quote(part, safe="") for part in key.split("/"))
