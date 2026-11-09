import asyncio
from pathlib import Path

import pytest

from narrato_agentk.media import BS3Backend, MediaUploadService, UploadRejected


class Reader:
    def __init__(self, data: bytes) -> None:
        self.data = data
        self.offset = 0

    async def read(self, size: int = -1) -> bytes:
        if self.offset >= len(self.data):
            return b""
        end = len(self.data) if size < 0 else self.offset + size
        chunk = self.data[self.offset : end]
        self.offset += len(chunk)
        return chunk


class Backend:
    def __init__(self, exists=False) -> None:
        self.already_exists = exists
        self.puts = []

    async def exists(self, key: str) -> bool:
        self.key = key
        return self.already_exists

    async def put_file(self, key: str, path: Path, content_type: str, size: int) -> None:
        self.last_path = path
        self.puts.append((key, path.read_bytes(), content_type, size))

    def uri(self, key: str) -> str:
        return "https://cdn.test/" + key


def test_upload_hashes_stream_and_deduplicates():
    backend = Backend(exists=True)
    result = asyncio.run(
        MediaUploadService(backend, key_prefix="safe").upload(Reader(b"video"), "video/mp4")
    )
    assert result.sizeBytes == 5
    assert result.uri.startswith("https://cdn.test/safe/uploads/")
    assert result.uri.endswith(".mp4")
    assert backend.puts == []


def test_upload_writes_new_object_without_using_filename():
    backend = Backend()
    result = asyncio.run(
        MediaUploadService(backend).upload(
            Reader(b"abc"),
            "video/quicktime",
            "../../user-controlled.mov",
        )
    )
    assert backend.puts[0][1] == b"abc"
    assert backend.puts[0][2] == "video/quicktime"
    assert "user-controlled" not in backend.puts[0][0]
    assert result.uri.endswith(".mov")
    assert not backend.last_path.exists()


def test_upload_rejects_unsupported_and_empty_files():
    with pytest.raises(UploadRejected, match="unsupported"):
        asyncio.run(MediaUploadService(Backend()).upload(Reader(b"x"), "text/plain"))
    with pytest.raises(UploadRejected, match="empty"):
        asyncio.run(MediaUploadService(Backend()).upload(Reader(b""), "video/mp4"))


def test_upload_rejects_mismatched_extension():
    with pytest.raises(UploadRejected, match="does not match"):
        asyncio.run(
            MediaUploadService(Backend()).upload(
                Reader(b"video"),
                "video/mp4",
                "../../payload.exe",
            )
        )


def test_upload_enforces_streaming_size_limit(monkeypatch):
    monkeypatch.setattr("narrato_agentk.media.store.MAX_VIDEO_BYTES", 4)
    with pytest.raises(UploadRejected, match="exceeds"):
        asyncio.run(MediaUploadService(Backend()).upload(Reader(b"12345"), "video/mp4"))


def test_bs3_backend_generates_encoded_storage_and_cdn_urls():
    backend = BS3Backend(
        endpoint="http://bs3.test/",
        bucket="upload-bucket",
        cdn_base="https://cdn.test/base/",
    )

    assert backend._object_url("folder/a b.mp4") == (
        "http://bs3.test/upload-bucket/folder/a%20b.mp4"
    )
    assert backend.uri("folder/a b.mp4") == "https://cdn.test/base/folder/a%20b.mp4"
