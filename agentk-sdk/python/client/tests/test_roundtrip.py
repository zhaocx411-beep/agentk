from __future__ import annotations

from collections.abc import AsyncIterator
from typing import Any

import pyqwest
import pytest
from agentk.api.v1 import service_pb
from agentk.api.v1.service_connect import (
    AgentKServiceASGIApplication,
    AgentKServiceClient,
)
from connectrpc.protocol import ProtocolType
from connectrpc.request import RequestContext
from pyqwest.testing import ASGITransport

from agentk.client import AsyncClient


class _TestProvider:
    def __init__(self, rpc: AgentKServiceClient) -> None:
        self._rpc = rpc

    async def acquire(self) -> AgentKServiceClient:
        return self._rpc

    async def aclose(self) -> None:
        await self._rpc.close()


class Service:
    def __init__(self) -> None:
        self.identity: list[tuple[str | None, str | None, str | None]] = []

    def __getattr__(self, _name: str) -> Any:
        async def unused(_request: object, _ctx: object) -> object:
            raise AssertionError("unexpected RPC")

        return unused

    async def who_am_i(
        self,
        _request: service_pb.WhoAmIRequest,
        ctx: RequestContext[
            service_pb.WhoAmIRequest,
            service_pb.WhoAmIResponse,
        ],
    ) -> service_pb.WhoAmIResponse:
        self.identity.append(
            (
                ctx.request_headers.get("kws-bin"),
                ctx.request_headers.get("kwsCredential"),
                ctx.request_headers.get("authorization"),
            )
        )
        return service_pb.WhoAmIResponse(id="user-1", display_name="Test User")

    async def watch_events(
        self,
        _request: service_pb.WatchEventsRequest,
        ctx: RequestContext[
            service_pb.WatchEventsRequest,
            service_pb.WatchEventsResponse,
        ],
    ) -> AsyncIterator[service_pb.WatchEventsResponse]:
        self.identity.append(
            (
                ctx.request_headers.get("kws-bin"),
                ctx.request_headers.get("kwsCredential"),
                ctx.request_headers.get("authorization"),
            )
        )
        yield service_pb.WatchEventsResponse()
        yield service_pb.WatchEventsResponse()


@pytest.mark.parametrize("protocol", [ProtocolType.CONNECT, ProtocolType.GRPC])
async def test_unary_and_server_stream_round_trip(
    protocol: ProtocolType,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("KWS_SERVICE_NAME", "agentk.sdk.fixture/原样")
    monkeypatch.setenv("KWS_SERVICE_CREDENTIAL", "roundtrip-credential")
    service = Service()
    app = AgentKServiceASGIApplication(service)
    transport = ASGITransport(app, http_version=pyqwest.HTTPVersion.HTTP2)

    async with transport:
        http_client = pyqwest.Client(transport)
        rpc = AgentKServiceClient(
            "http://agentk.test",
            protocol=protocol,
            http_client=http_client,
        )
        client = AsyncClient(_TestProvider(rpc))

        identity = await client.who_am_i(service_pb.WhoAmIRequest())
        frames = [
            frame
            async for frame in client.watch_events(
                service_pb.WatchEventsRequest(
                    session_id="session-1", execution_id="execution-1"
                )
            )
        ]
        await client.aclose()

    assert identity.id == "user-1"
    assert identity.display_name == "Test User"
    assert len(frames) == 2
    expected = (
        "ChlhZ2VudGsuc2RrLmZpeHR1cmUv5Y6f5qC3",
        "roundtrip-credential",
        None,
    )
    assert service.identity == [expected, expected]
