from pathlib import Path

import httpx

from agentk_capability_demo.blob_upload import BlobUploadStorage
from agentk_capability_demo.config import Settings


async def test_blob_upload_uses_dedicated_prefix_and_streams_file(tmp_path: Path):
    source = tmp_path / "reference.png"
    source.write_bytes(b"\x89PNG\r\n\x1a\nimage-data")
    captured = {}

    async def handler(request: httpx.Request) -> httpx.Response:
        captured["url"] = str(request.url)
        captured["content_type"] = request.headers["content-type"]
        captured["content_length"] = request.headers["content-length"]
        captured["body"] = await request.aread()
        return httpx.Response(204)

    client = httpx.AsyncClient(transport=httpx.MockTransport(handler))
    storage = BlobUploadStorage(
        "https://bs3-hb1.staging.kuaishou.com",
        "user-zhangyuchen05",
        "agentk-capability-demo",
        client=client,
    )
    url, key = await storage.put(source, "reference.png", source.stat().st_size, "image/png")

    assert key.startswith("agentk-capability-demo/")
    assert key.endswith(".png")
    assert url == f"https://bs3-hb1.staging.kuaishou.com/user-zhangyuchen05/{key}"
    assert captured == {
        "url": url,
        "content_type": "image/png",
        "content_length": str(source.stat().st_size),
        "body": source.read_bytes(),
    }
    await client.aclose()


def test_blob_settings_require_endpoint_and_bucket_together():
    try:
        Settings("http://agentk.test", "secret", blob_endpoint="https://blob.test")
    except ValueError as exc:
        assert "configured together" in str(exc)
    else:
        raise AssertionError("partial BlobStore configuration must fail")


def test_blob_settings_use_dedicated_default_prefix():
    settings = Settings(
        "http://agentk.test",
        "secret",
        blob_endpoint="https://bs3-hb1.staging.kuaishou.com",
        blob_bucket="user-zhangyuchen05",
    )
    assert settings.blob_prefix == "agentk-capability-demo"
    assert settings.public()["uploadsManaged"] is True
