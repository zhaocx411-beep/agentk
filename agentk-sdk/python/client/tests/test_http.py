from __future__ import annotations

from collections.abc import Callable

import pytest

from agentk.client import Options
from agentk.client.http import _gateway_url, create_client


@pytest.mark.parametrize(
    ("stage", "expected"),
    [
        ("STAGING", "https://agentk-api.staging.kuaishou.com"),
        (" staging ", "https://agentk-api.staging.kuaishou.com"),
        ("CANDIDATE", "https://agentk-api.staging.kuaishou.com"),
        ("PRODUCTION", "http://agentk-api.internal"),
        ("", "http://agentk-api.internal"),
        (None, "http://agentk-api.internal"),
    ],
)
def test_gateway_url(stage: str | None, expected: str) -> None:
    getenv: Callable[[str], str | None] = lambda _name: stage

    assert _gateway_url(getenv) == expected


def test_create_client_uses_endpoint_override(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    captured: dict[str, object] = {}

    class RPC:
        def __init__(self, address: str) -> None:
            captured["address"] = address

        async def close(self) -> None:
            return

    monkeypatch.setattr("agentk.client.http.AgentKServiceClient", RPC)

    create_client(Options(endpoint="http://127.0.0.1:8080"))

    assert captured["address"] == "http://127.0.0.1:8080"


def test_create_client_rejects_invalid_endpoint() -> None:
    with pytest.raises(ValueError, match="invalid endpoint"):
        create_client(Options(endpoint="127.0.0.1:8080"))
