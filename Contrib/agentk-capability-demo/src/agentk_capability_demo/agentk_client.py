from __future__ import annotations

from typing import TYPE_CHECKING

from agentk.api.v1.service_connect import AgentKServiceClient
from agentk.client import AsyncClient, Options
from agentk.client.grpc import create_client as _create_kess_client

if TYPE_CHECKING:
    from pyqwest import Client as HTTPClient


class FixedAddressProvider:
    def __init__(self, address: str, http_client: HTTPClient | None = None) -> None:
        self._rpc = AgentKServiceClient(address, http_client=http_client)
        self._closed = False

    async def acquire(self, lane: str = "") -> AgentKServiceClient:
        if self._closed:
            raise RuntimeError("AgentK client is closed")
        return self._rpc

    async def aclose(self) -> None:
        if not self._closed:
            self._closed = True
            await self._rpc.close()


def create_client(
    address: str | None,
    api_key: str | None = None,
    *,
    http_client: HTTPClient | None = None,
) -> AsyncClient:
    """Create an AgentK client using the SDK's process-level KWS identity.

    ``api_key`` is retained for compatibility with existing demo configuration.
    AgentK SDK 0.3.0 obtains authentication from ``KWS_SERVICE_NAME`` and
    ``KWS_SERVICE_CREDENTIAL`` instead of accepting an API key per client.
    """
    del api_key
    if address:
        return AsyncClient(FixedAddressProvider(address, http_client))
    if http_client is not None:
        raise ValueError("http_client requires an explicit AgentK address")
    return _create_kess_client(Options())


class AgentKUnavailable(RuntimeError):
    pass


def friendly_error(exc: Exception) -> str:
    message = str(exc).strip() or exc.__class__.__name__
    lowered = message.lower()
    if "connect" in lowered or "refused" in lowered or "dns" in lowered:
        return f"无法连接 AgentK：{message}"
    if "unauth" in lowered or "permission" in lowered:
        return f"AgentK 鉴权失败：{message}"
    return message
