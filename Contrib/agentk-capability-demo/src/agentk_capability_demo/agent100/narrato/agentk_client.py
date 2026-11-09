from __future__ import annotations

from typing import Any

from agentk.client import AsyncClient

from agentk_capability_demo.agentk_client import create_client


def create_agentk_client(base_url: str | None, api_key: str | None = None) -> AsyncClient:
    return create_client(base_url, api_key)


def message_to_python(value: Any) -> Any:
    converter = getattr(value, "to_python", None)
    return converter() if callable(converter) else value
