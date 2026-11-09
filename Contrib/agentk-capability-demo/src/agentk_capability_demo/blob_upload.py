from __future__ import annotations

import secrets
from pathlib import Path

import anyio
import httpx


class BlobUploadStorage:
    """Anonymous HTTP PUT storage compatible with AgentK managed artifact URLs."""

    def __init__(
        self,
        endpoint: str,
        bucket: str,
        prefix: str,
        cdn_base: str | None = None,
        *,
        client: httpx.AsyncClient | None = None,
    ) -> None:
        self.endpoint = endpoint.rstrip("/")
        self.bucket = bucket.strip("/")
        self.prefix = prefix.strip("/")
        self.cdn_base = cdn_base.rstrip("/") if cdn_base else None
        self._client = client or httpx.AsyncClient(timeout=httpx.Timeout(300))
        self._owns_client = client is None

    async def put(
        self, path: Path, original_name: str, size: int, content_type: str
    ) -> tuple[str, str]:
        suffix = Path(original_name).suffix.lower()[:12]
        if not suffix[1:].isalnum():
            suffix = ""
        key = f"{self.prefix}/{secrets.token_hex(16)}{suffix}"
        object_url = f"{self.endpoint}/{self.bucket}/{key}"

        async def chunks():
            async with await anyio.open_file(path, "rb") as source:
                while data := await source.read(1024 * 1024):
                    yield data

        response = await self._client.put(
            object_url,
            content=chunks(),
            headers={"Content-Type": content_type, "Content-Length": str(size)},
        )
        if response.status_code not in {200, 204}:
            detail = response.text.strip()[:512]
            raise RuntimeError(f"BlobStore upload failed: HTTP {response.status_code}: {detail}")
        if self.cdn_base:
            return f"{self.cdn_base}/{key}", key
        return object_url, key

    async def aclose(self) -> None:
        if self._owns_client:
            await self._client.aclose()
