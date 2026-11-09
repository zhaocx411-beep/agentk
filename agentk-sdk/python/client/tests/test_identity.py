from __future__ import annotations

import base64

import pytest

from agentk.client._identity import _kws_model, identity_headers


@pytest.mark.parametrize(
    ("values", "expected"),
    [
        ({}, {}),
        (
            {"KWS_SERVICE_NAME": "agentk.sdk.fixture/原样"},
            {"kws-bin": "ChlhZ2VudGsuc2RrLmZpeHR1cmUv5Y6f5qC3"},
        ),
        (
            {"KWS_SERVICE_CREDENTIAL": " credential 原样 "},
            {"kwsCredential": " credential 原样 "},
        ),
        (
            {
                "KWS_SERVICE_NAME": "Service.Name/原样",
                "KWS_SERVICE_CREDENTIAL": "credential",
            },
            {
                "kws-bin": "ChNTZXJ2aWNlLk5hbWUv5Y6f5qC3",
                "kwsCredential": "credential",
            },
        ),
    ],
)
def test_identity_headers(values: dict[str, str], expected: dict[str, str]) -> None:
    assert identity_headers(values.get) == expected


def test_cross_language_fixture() -> None:
    raw = _kws_model("agentk.sdk.fixture/原样")

    assert raw.hex() == "0a196167656e746b2e73646b2e666978747572652fe58e9fe6a0b7"
    assert base64.b64encode(raw).decode("ascii").rstrip("=") == (
        "ChlhZ2VudGsuc2RrLmZpeHR1cmUv5Y6f5qC3"
    )


def test_long_service_name_uses_multibyte_varint() -> None:
    raw = _kws_model("x" * 128)

    assert raw[:3] == b"\x0a\x80\x01"
    assert raw[3:] == b"x" * 128


def test_identity_headers_are_immutable() -> None:
    headers = identity_headers({"KWS_SERVICE_NAME": "service"}.get)

    with pytest.raises(TypeError):
        headers["kws-bin"] = "changed"  # type: ignore[index]
