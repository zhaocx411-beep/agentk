from __future__ import annotations

import asyncio
import base64

import pyqwest
from agentk.api.v1.service_connect import AgentKServiceClient
from connectrpc.protocol import ProtocolType
from ksresolve.rpc import current_carrier
from ksresolve.rpc.connect import Transport

from .._core import AsyncClient, Options, _validate_endpoint
from .._provider import OwnedClient

_SERVICE_NAME = "klingai-agentk-api"
# Same key the carrier uses in gRPC metadata; ksresolve writes it on the KESS
# path, the direct endpoint path writes it here.
_CARRIER_HEADER = "trace_context-bin"


def _native_transport(**kwargs: bool) -> pyqwest.HTTPTransport:
    return pyqwest.HTTPTransport(
        http_version=pyqwest.HTTPVersion.HTTP2, enable_otel=True, **kwargs
    )


class _CarrierTransport:
    """Writes the context's trace carrier for direct connections that bypass ksresolve."""

    def __init__(self, transport: pyqwest.HTTPTransport) -> None:
        self._transport = transport

    async def execute(self, request: pyqwest.Request) -> pyqwest.Response:
        raw = current_carrier()
        if raw is None or request.headers.get(_CARRIER_HEADER):
            return await self._transport.execute(request)
        headers = pyqwest.Headers(request.headers.items())
        headers[_CARRIER_HEADER] = base64.b64encode(raw).decode("ascii").rstrip("=")
        return await self._transport.execute(
            pyqwest.Request(request.method, request.url, headers, request.content)
        )


class _KessProvider:
    def __init__(self) -> None:
        self._client: OwnedClient | None = None
        self._lock = asyncio.Lock()
        self._closed = False

    async def acquire(self) -> AgentKServiceClient:
        if self._closed:
            raise RuntimeError("agentk client: client is closed")
        async with self._lock:
            if self._closed:
                raise RuntimeError("agentk client: client is closed")
            if self._client is None:
                native = _native_transport(follow_redirects=False)
                routing = Transport(_SERVICE_NAME, transport=native)
                try:
                    await routing.__aenter__()
                    try:
                        rpc = AgentKServiceClient(
                            f"http://{_SERVICE_NAME}",
                            protocol=ProtocolType.GRPC,
                            http_client=pyqwest.Client(routing),
                        )
                    except BaseException:
                        await routing.close()
                        raise
                except BaseException:
                    await native.aclose()
                    raise
                self._client = OwnedClient(rpc, routing.close, native.aclose)
            return await self._client.acquire()

    async def aclose(self) -> None:
        async with self._lock:
            if self._closed:
                return
            self._closed = True
            if self._client is not None:
                await self._client.aclose()


def create_client(options: Options) -> AsyncClient:
    """Create an AgentK gRPC client using an override or KESS discovery.

    Args:
        options: Endpoint options shared by AgentK client transports.

    Returns:
        An asynchronous AgentK client. Use it as an asynchronous context manager
        or call aclose when it is no longer needed.

    Raises:
        ValueError: If options has an invalid endpoint.
    """
    if options.endpoint:
        provider = _new_owned_client(_validate_endpoint(options.endpoint))
    else:
        provider = _KessProvider()
    return AsyncClient(provider)


def _new_owned_client(endpoint: str) -> OwnedClient:
    native = _native_transport()
    rpc = AgentKServiceClient(
        endpoint,
        protocol=ProtocolType.GRPC,
        http_client=pyqwest.Client(_CarrierTransport(native)),
    )
    return OwnedClient(rpc, native.aclose)


__all__ = ["create_client"]
