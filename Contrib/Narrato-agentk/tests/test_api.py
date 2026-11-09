from dataclasses import dataclass

import pytest
from fastapi.testclient import TestClient

from narrato_agentk.api.main import create_app
from narrato_agentk.media import UploadRejected
from narrato_agentk.settings import Settings


@dataclass
class Result:
    def to_dict(self):
        return {
            "uri": "https://cdn.test/hash.mp4",
            "sha256": "abc",
            "sizeBytes": 3,
            "contentType": "video/mp4",
        }


class UploadService:
    def __init__(self, error=None):
        self.error = error
        self.seen = None

    async def upload(self, reader, content_type, filename):
        self.seen = (content_type, filename, await reader.read())
        if self.error:
            raise self.error
        return Result()


def test_health_and_stream_upload_contract():
    service = UploadService()
    client = TestClient(create_app(Settings(), service))
    assert client.get("/healthz").json() == {"status": "ok"}
    response = client.post(
        "/v1/media",
        files={"file": ("input.mp4", b"abc", "video/mp4")},
    )
    assert response.status_code == 201
    assert response.json() == {
        "uri": "https://cdn.test/hash.mp4",
        "sha256": "abc",
        "sizeBytes": 3,
        "contentType": "video/mp4",
    }
    assert service.seen == ("video/mp4", "input.mp4", b"abc")


def test_upload_error_statuses():
    too_large = TestClient(
        create_app(Settings(), UploadService(UploadRejected("video exceeds 4 bytes")))
    )
    assert (
        too_large.post("/v1/media", files={"file": ("a.mp4", b"abc", "video/mp4")}).status_code
        == 413
    )

    invalid = TestClient(
        create_app(Settings(), UploadService(UploadRejected("unsupported video content type")))
    )
    assert (
        invalid.post("/v1/media", files={"file": ("a.txt", b"abc", "text/plain")}).status_code
        == 422
    )


def test_configured_access_token_protects_uploads():
    service = UploadService()
    client = TestClient(create_app(Settings(console_token="upload-secret"), service))

    unauthorized = client.post(
        "/v1/media",
        files={"file": ("input.mp4", b"abc", "video/mp4")},
    )
    authorized = client.post(
        "/v1/media",
        files={"file": ("input.mp4", b"abc", "video/mp4")},
        headers={"X-Narrato-Console-Token": "upload-secret"},
    )

    assert unauthorized.status_code == 401
    assert authorized.status_code == 201
    assert service.seen == ("video/mp4", "input.mp4", b"abc")


def test_api_responses_include_browser_security_headers():
    response = TestClient(create_app(Settings(), UploadService())).get("/")

    assert response.headers["x-content-type-options"] == "nosniff"
    assert response.headers["x-frame-options"] == "DENY"
    assert "default-src 'self'" in response.headers["content-security-policy"]


def test_app_factory_rejects_insecure_non_loopback_binding():
    with pytest.raises(ValueError, match="NARRATO_CONSOLE_TOKEN"):
        create_app(Settings(api_host="0.0.0.0"), UploadService())
