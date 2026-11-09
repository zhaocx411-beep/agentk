from collections.abc import Awaitable, Callable
from typing import Protocol

from agentk.api.v1.service_connect import AgentKServiceClient


class RPCProvider(Protocol):
    async def acquire(self) -> AgentKServiceClient: ...

    async def aclose(self) -> None: ...


class OwnedClient:
    """One RPC client plus the transport resources it depends on.

    Closers run in the given order after the RPC client itself is closed; a
    failure in one does not skip the rest.
    """

    def __init__(
        self, rpc: AgentKServiceClient, *closers: Callable[[], Awaitable[None]]
    ) -> None:
        self._rpc = rpc
        self._closers: tuple[Callable[[], Awaitable[None]], ...] = (rpc.close, *closers)
        self._closed = False

    async def acquire(self) -> AgentKServiceClient:
        if self._closed:
            raise RuntimeError("agentk client: client is closed")
        return self._rpc

    async def aclose(self) -> None:
        if self._closed:
            return
        self._closed = True
        await _close_all(self._closers)


async def _close_all(closers: tuple[Callable[[], Awaitable[None]], ...]) -> None:
    if not closers:
        return
    try:
        await closers[0]()
    finally:
        await _close_all(closers[1:])
