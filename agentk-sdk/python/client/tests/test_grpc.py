from __future__ import annotations

import asyncio
import base64
import threading
from collections.abc import AsyncIterator
from contextlib import nullcontext
from datetime import UTC, datetime
from types import SimpleNamespace
from urllib.parse import urlsplit

import pyqwest
import pytest
from agentk.api.v1 import service_pb
from agentk.api.v1.service_connect import AgentKServiceASGIApplication
from connectrpc.code import Code
from connectrpc.errors import ConnectError
from ksresolve import kess
from ksresolve.kws import caller_from_headers
from ksresolve.rpc import current_lane, with_lane
from pyqwest.testing import ASGITransport

from agentk.client import Options
from agentk.client.grpc import create_client


class Service:
    def __init__(self):
        self.identity = []
        self.lanes = []

    def __getattr__(self, name):
        async def unused(request, ctx):
            raise AssertionError(f"unexpected RPC: {name}")

        return unused

    def record(self, ctx):
        self.identity.append(
            (
                ctx.request_headers.get("kws-bin"),
                ctx.request_headers.get("kwsCredential"),
            )
        )
        self.lanes.append(ctx.request_headers.get("trace_context-bin"))

    async def who_am_i(self, request, ctx):
        self.record(ctx)
        return service_pb.WhoAmIResponse(id="user-1")

    async def watch_events(
        self, request, ctx
    ) -> AsyncIterator[service_pb.WatchEventsResponse]:
        self.record(ctx)
        yield service_pb.WatchEventsResponse()
        yield service_pb.WatchEventsResponse()


def endpoint(host="baseline", lane=""):
    return kess.Endpoint(host, host, "grpc", 8080, 1, shard="s0", lane=lane)


def carrier(lane):
    """Base64 trace carrier holding only lane, as ksresolve encodes it."""
    return (
        base64.b64encode(bytes([0x0A, len(lane)]) + lane.encode()).decode().rstrip("=")
    )


class Watch:
    def __init__(self):
        self.calls = 0
        self.closed = 0
        self.set(endpoint())

    def set(self, *endpoints, err=None):
        self.current = kess.State(
            kess.Snapshot(kess.Target("klingai-agentk-api", "grpc"), 1, endpoints),
            datetime.now(UTC),
            err,
        )

    def close(self):
        self.closed += 1


@pytest.fixture
def network(monkeypatch):
    watch = Watch()
    service = Service()
    requests = []
    natives = []
    options = []

    class Native:
        def __init__(self, **kwargs):
            options.append(kwargs)
            self.closed = 0
            self.asgi = ASGITransport(
                AgentKServiceASGIApplication(service),
                http_version=pyqwest.HTTPVersion.HTTP2,
            )
            self.entered = False
            natives.append(self)

        async def execute(self, request):
            requests.append(request)
            if not self.entered:
                await self.asgi.__aenter__()
                self.entered = True
            return await self.asgi.execute(request)

        async def aclose(self):
            self.closed += 1
            if self.entered:
                await self.asgi.__aexit__(None, None, None)

    class Discovery:
        def __init__(self, _config=None):
            pass

        def watch(self, target):
            assert target == kess.Target("klingai-agentk-api", "grpc")
            watch.calls += 1
            return watch

    monkeypatch.setattr(kess, "Client", Discovery)
    monkeypatch.setattr(pyqwest, "HTTPTransport", Native)
    monkeypatch.setenv("KWS_SERVICE_STAGE", "STAGING")
    monkeypatch.setenv("KWS_SERVICE_NAME", "agentk.test")
    monkeypatch.setenv("KWS_SERVICE_CREDENTIAL", " credential ")
    return SimpleNamespace(
        watch=watch,
        service=service,
        requests=requests,
        natives=natives,
        options=options,
    )


@pytest.mark.parametrize(
    ("lane", "registered", "expected"),
    [
        (None, ["", "prt"], ""),
        ("prt,PRT", ["", "PRT"], "PRT"),
        ("prt", ["", "prt"], "prt"),
        ("dev.child", ["", "dev"], "dev"),
        ("missing", ["", "dev"], ""),
        ("missing,dev", ["", "dev"], "dev"),
    ],
)
async def test_real_transport_routes_and_preserves_headers(
    network, monkeypatch, lane, registered, expected
):
    # The deployment lane must not color requests.
    monkeypatch.setenv("KWS_LANE_ID", "prt")
    network.watch.set(*(endpoint(name or "baseline", name) for name in registered))
    client = create_client(Options())
    assert network.watch.calls == 0
    with with_lane(lane) if lane else nullcontext():
        response = await client.who_am_i(service_pb.WhoAmIRequest())
        assert current_lane() == (lane or "")
    assert response.id == "user-1"
    request = network.requests[-1]
    assert urlsplit(request.url).hostname == (expected.lower() or "baseline")
    assert network.service.lanes == [carrier(lane) if lane else None]
    assert network.service.identity[0][1] == "credential"
    caller = caller_from_headers(request.headers.items())
    assert caller.service_name == "agentk.test"
    assert caller.stage == "STAGING"
    assert request.headers.get("rcc-bin")
    await client.aclose()
    await client.aclose()
    assert network.watch.closed == 1
    assert network.natives[0].closed == 1
    assert network.options[0]["enable_otel"] is True
    assert network.options[0]["http_version"] == pyqwest.HTTPVersion.HTTP2


@pytest.mark.parametrize("stage", ["PROD", "PRODUCTION", "PREONLINE"])
async def test_production_uses_baseline_but_propagates_lane(
    network, monkeypatch, stage
):
    monkeypatch.setenv("KWS_SERVICE_STAGE", stage)
    network.watch.set(endpoint(), endpoint("dev", "dev"))
    async with create_client(Options()) as client:
        with with_lane("dev"):
            await client.who_am_i(service_pb.WhoAmIRequest())
    assert urlsplit(network.requests[0].url).hostname == "baseline"
    assert network.service.lanes == [carrier("dev")]


async def test_concurrent_first_requests_initialize_once_and_isolate_lanes(network):
    network.watch.set(endpoint(), endpoint("dev", "dev"), endpoint("prt", "prt"))
    client = create_client(Options())

    async def call(lane):
        with with_lane(lane) if lane else nullcontext():
            await client.who_am_i(service_pb.WhoAmIRequest())

    await asyncio.gather(*(call(lane) for lane in ["dev", "prt", None] * 4))
    assert network.watch.calls == 1
    for request in network.requests:
        header = request.headers.get("trace_context-bin")
        lane = {carrier("dev"): "dev", carrier("prt"): "prt", None: ""}[header]
        assert urlsplit(request.url).hostname == (lane or "baseline")
    await asyncio.gather(client.aclose(), client.aclose())
    assert network.watch.closed == 1
    with pytest.raises(RuntimeError, match="closed"):
        await client.who_am_i(service_pb.WhoAmIRequest())


async def test_stream_survives_topology_change_and_new_calls_follow_it(network):
    client = create_client(Options())
    frames = client.watch_events(service_pb.WatchEventsRequest())
    await anext(frames)
    network.watch.set(endpoint("replacement"))
    await client.who_am_i(service_pb.WhoAmIRequest())
    assert len([frame async for frame in frames]) == 1
    assert [urlsplit(r.url).hostname for r in network.requests] == [
        "baseline",
        "replacement",
    ]
    network.watch.set(endpoint("ignored"), err=OSError("refresh failed"))
    await client.who_am_i(service_pb.WhoAmIRequest())
    assert urlsplit(network.requests[-1].url).hostname == "replacement"
    await client.aclose()


async def test_empty_topology_fails_without_replaying(network):
    network.watch.set()
    async with create_client(Options()) as client:
        with pytest.raises(Exception, match="endpoint"):
            await client.who_am_i(service_pb.WhoAmIRequest())
    assert network.requests == []


async def test_close_before_use_and_endpoint_override_do_not_discover(network):
    client = create_client(Options())
    await client.aclose()
    assert network.natives == []
    async with create_client(Options(endpoint="http://direct:8080")) as direct:
        await direct.who_am_i(service_pb.WhoAmIRequest())
        with with_lane("dev"):
            await direct.who_am_i(service_pb.WhoAmIRequest())
    assert network.watch.calls == 0
    assert network.service.identity[0][1] == " credential "
    assert urlsplit(network.requests[0].url).hostname == "direct"
    assert network.service.lanes == [None, carrier("dev")]
    assert network.natives[0].closed == 1


async def test_initial_discovery_failure_closes_native_and_can_retry(
    network, monkeypatch
):
    original = kess.Client.watch

    def fail(self, target):
        raise LookupError("discovery unavailable")

    monkeypatch.setattr(kess.Client, "watch", fail)
    client = create_client(Options())
    with pytest.raises(LookupError, match="discovery unavailable"):
        await client.who_am_i(service_pb.WhoAmIRequest())
    assert network.natives[0].closed == 1
    monkeypatch.setattr(kess.Client, "watch", original)
    await client.who_am_i(service_pb.WhoAmIRequest())
    await client.aclose()
    assert network.natives[1].closed == 1


async def test_cancelled_initial_discovery_releases_resources(network, monkeypatch):
    started = threading.Event()
    release = threading.Event()

    def delayed(self, target):
        started.set()
        assert release.wait(5)
        return network.watch

    monkeypatch.setattr(kess.Client, "watch", delayed)
    client = create_client(Options())
    task = asyncio.create_task(client.who_am_i(service_pb.WhoAmIRequest()))
    assert await asyncio.to_thread(started.wait, 5)
    task.cancel()
    release.set()
    with pytest.raises(asyncio.CancelledError):
        await task
    await client.aclose()
    assert network.watch.closed == 1
    assert network.natives[0].closed == 1


async def test_request_cancellation_restores_lane_and_does_not_retry(network):
    started = asyncio.Event()

    async def blocked(request, ctx):
        started.set()
        await asyncio.Event().wait()

    network.service.who_am_i = blocked
    client = create_client(Options())

    async def call():
        with with_lane("dev"):
            try:
                await client.who_am_i(service_pb.WhoAmIRequest())
            finally:
                assert current_lane() == "dev"

    task = asyncio.create_task(call())
    await started.wait()
    task.cancel()
    with pytest.raises(ConnectError) as caught:
        await task
    assert caught.value.code == Code.CANCELED
    assert len(network.requests) == 1
    await client.aclose()


@pytest.mark.parametrize("direct", [False, True])
async def test_real_pyqwest_transport_can_be_closed(monkeypatch, direct):
    watch = Watch()

    class Discovery:
        def __init__(self, config=None):
            pass

        def watch(self, target):
            return watch

    monkeypatch.setattr(kess, "Client", Discovery)
    options = Options(endpoint="http://127.0.0.1:1") if direct else Options()
    client = create_client(options)
    await client._provider.acquire()
    await client.aclose()
    await client.aclose()
    assert watch.closed == (0 if direct else 1)
