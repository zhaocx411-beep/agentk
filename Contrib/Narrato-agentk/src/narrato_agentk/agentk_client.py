from __future__ import annotations

from typing import Any

from agentk.api.v1.service_connect import AgentKServiceClient
from agentk.client import AsyncClient


class _FixedProvider:
    def __init__(self, rpc: AgentKServiceClient) -> None:
        self._rpc = rpc
        self._closed = False

    async def acquire(self) -> AgentKServiceClient:
        if self._closed:
            raise RuntimeError("AgentK client is closed")
        return self._rpc

    async def aclose(self) -> None:
        if self._closed:
            return
        self._closed = True
        await self._rpc.close()


def create_agentk_client(base_url: str, api_key: str) -> AsyncClient:
    address = base_url.strip().rstrip("/")
    if not address:
        raise ValueError("AGENTK_BASE_URL is required")
    rpc = AgentKServiceClient(address)
    return AsyncClient(_FixedProvider(rpc), api_key)


def message_to_python(value: Any) -> Any:
    converter = getattr(value, "to_python", None)
    return converter() if callable(converter) else value
