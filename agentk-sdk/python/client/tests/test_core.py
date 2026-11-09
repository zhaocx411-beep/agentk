from __future__ import annotations

import asyncio
from collections.abc import AsyncIterator
from typing import Any, cast

import pytest
from agentk.api.v1 import event_pb, model_pb, profile_pb, service_pb
from agentk.api.v1.service_connect import AgentKServiceClient
from ksresolve.rpc import with_lane
from protobuf import Oneof
from protobuf.wkt import struct_pb

from agentk.client import AsyncClient


class FakeRPC:
    def __init__(self) -> None:
        self.calls: list[tuple[str, object, object]] = []
        self.responses: dict[str, object] = {}
        self.stream: AsyncIterator[service_pb.WatchEventsResponse] | None = None

    def __getattr__(self, name: str) -> Any:
        async def invoke(request: object, *, headers: object) -> object:
            self.calls.append((name, request, headers))
            response = self.responses[name]
            if isinstance(response, Exception):
                raise response
            return response

        return invoke

    def watch_events(
        self,
        request: service_pb.WatchEventsRequest,
        *,
        headers: object,
    ) -> AsyncIterator[service_pb.WatchEventsResponse]:
        self.calls.append(("watch_events", request, headers))
        assert self.stream is not None
        return self.stream


class FakeProvider:
    def __init__(self, rpc: FakeRPC) -> None:
        self.rpc = rpc
        self.acquire_count = 0
        self.close_count = 0

    async def acquire(self) -> AgentKServiceClient:
        self.acquire_count += 1
        return cast(AgentKServiceClient, self.rpc)

    async def aclose(self) -> None:
        self.close_count += 1


_UNARY_CASES = [
    (
        "create_session",
        service_pb.CreateSessionRequest(
            title="new session", profile=profile_pb.Profile(default_model="test-model")
        ),
        service_pb.CreateSessionResponse(
            session=model_pb.Session(id="created", title="new session")
        ),
    ),
    (
        "list_sessions",
        service_pb.ListSessionsRequest(page_size=7, page_token="sessions-before"),
        service_pb.ListSessionsResponse(
            sessions=[model_pb.Session(id="listed")], next_page_token="sessions-after"
        ),
    ),
    (
        "start_execution",
        service_pb.StartExecutionRequest(
            session_id="start-session",
            input=Oneof("message", service_pb.AgentMessageExecutionInput(text="hello")),
        ),
        service_pb.StartExecutionResponse(execution=model_pb.Execution(id="started")),
    ),
    (
        "cancel_execution",
        service_pb.CancelExecutionRequest(session_id="cancel-session"),
        service_pb.CancelExecutionResponse(),
    ),
    (
        "list_events",
        service_pb.ListEventsRequest(
            session_id="events-session", page_size=8, page_token="events-before"
        ),
        service_pb.ListEventsResponse(next_page_token="events-after"),
    ),
    (
        "list_pipelines",
        service_pb.ListPipelinesRequest(
            session_id="pipelines-session", page_size=9, page_token="pipelines-before"
        ),
        service_pb.ListPipelinesResponse(
            pipelines=[model_pb.Pipeline(name="render")],
            next_page_token="pipelines-after",
        ),
    ),
    (
        "list_executions",
        service_pb.ListExecutionsRequest(
            session_id="executions-session",
            page_size=10,
            page_token="executions-before",
        ),
        service_pb.ListExecutionsResponse(
            executions=[model_pb.Execution(id="listed-execution")],
            next_page_token="executions-after",
        ),
    ),
    (
        "get_execution",
        service_pb.GetExecutionRequest(
            session_id="get-session", execution_id="get-execution"
        ),
        service_pb.GetExecutionResponse(
            execution=model_pb.Execution(id="get-execution")
        ),
    ),
    (
        "list_capabilities",
        service_pb.ListCapabilitiesRequest(
            page_size=11, page_token="capabilities-before", include_variants=True
        ),
        service_pb.ListCapabilitiesResponse(
            capabilities=[model_pb.Capability(id="contrib.test.render")],
            next_page_token="capabilities-after",
        ),
    ),
    (
        "invoke_capability",
        service_pb.InvokeCapabilityRequest(
            capability_id="contrib.test.render",
            input=struct_pb.Struct.from_python({"prompt": "draw"}),
        ),
        service_pb.InvokeCapabilityResponse(
            output=struct_pb.Value.from_python("rendered")
        ),
    ),
    ("who_am_i", service_pb.WhoAmIRequest(), service_pb.WhoAmIResponse(id="u1")),
]

_IDENTITY_HEADERS = {
    "kws-bin": "ChlhZ2VudGsuc2RrLmZpeHR1cmUv5Y6f5qC3",
    "kwsCredential": "test-credential",
}


@pytest.fixture(autouse=True)
def _identity_environment(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("KWS_SERVICE_NAME", "agentk.sdk.fixture/原样")
    monkeypatch.setenv("KWS_SERVICE_CREDENTIAL", "test-credential")
    monkeypatch.delenv("KWS_SERVICE_STAGE", raising=False)


@pytest.mark.parametrize(("method", "input_message", "response"), _UNARY_CASES)
async def test_unary_wrappers_add_identity_headers(
    method: str,
    input_message: object,
    response: object,
) -> None:
    rpc = FakeRPC()
    rpc.responses[method] = response
    provider = FakeProvider(rpc)
    client = AsyncClient(provider)

    actual = await getattr(client, method)(input_message)

    assert actual == response
    assert rpc.calls == [
        (method, input_message, _IDENTITY_HEADERS),
    ]


async def test_unary_error_is_not_wrapped() -> None:
    error = RuntimeError("rpc failed")
    rpc = FakeRPC()
    rpc.responses["who_am_i"] = error
    client = AsyncClient(FakeProvider(rpc))

    with pytest.raises(RuntimeError) as caught:
        await client.who_am_i(service_pb.WhoAmIRequest())

    assert caught.value is error


async def test_client_adds_no_lane_header_of_its_own(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    # The lane travels in the transport's trace carrier, never in AgentK
    # headers, and the deployment lane never colors requests.
    monkeypatch.setenv("KWS_SERVICE_STAGE", "PROD_REGRESSION_TEST")
    monkeypatch.setenv("KWS_LANE_ID", "deployment")
    rpc = FakeRPC()
    rpc.responses["who_am_i"] = service_pb.WhoAmIResponse()
    client = AsyncClient(FakeProvider(rpc))

    async def stream() -> AsyncIterator[service_pb.WatchEventsResponse]:
        yield service_pb.WatchEventsResponse()

    rpc.stream = stream()
    async with client:
        with with_lane("dev_lane"):
            await client.who_am_i(service_pb.WhoAmIRequest())
            assert (
                len(
                    [
                        frame
                        async for frame in client.watch_events(
                            service_pb.WatchEventsRequest()
                        )
                    ]
                )
                == 1
            )
        await client.who_am_i(service_pb.WhoAmIRequest())
    for _, _, headers in rpc.calls:
        assert headers == _IDENTITY_HEADERS


async def test_invoke_capability_propagates_caller_cancellation() -> None:
    started = asyncio.Event()
    cancelled = asyncio.Event()

    class BlockingRPC(FakeRPC):
        async def invoke_capability(
            self,
            request: service_pb.InvokeCapabilityRequest,
            *,
            headers: object,
        ) -> service_pb.InvokeCapabilityResponse:
            self.calls.append(("invoke_capability", request, headers))
            started.set()
            try:
                await asyncio.Future()
            finally:
                cancelled.set()

    rpc = BlockingRPC()
    client = AsyncClient(FakeProvider(rpc))
    task = asyncio.create_task(
        client.invoke_capability(
            service_pb.InvokeCapabilityRequest(capability_id="contrib.test.render")
        )
    )
    await started.wait()
    task.cancel()

    with pytest.raises(asyncio.CancelledError):
        await task
    assert cancelled.is_set()


async def test_watch_events_uses_one_rpc_for_entire_stream() -> None:
    frames = [
        service_pb.WatchEventsResponse(
            frame=Oneof("delta", event_pb.Delta(execution_id="exe_1", text="hello"))
        ),
        service_pb.WatchEventsResponse(
            frame=Oneof("stream_end", event_pb.StreamEnd(execution_id="exe_1"))
        ),
    ]

    async def stream() -> AsyncIterator[service_pb.WatchEventsResponse]:
        for frame in frames:
            yield frame

    rpc = FakeRPC()
    rpc.stream = stream()
    provider = FakeProvider(rpc)
    client = AsyncClient(provider)
    request = service_pb.WatchEventsRequest(session_id="s1", execution_id="exe_1")

    actual = [frame async for frame in client.watch_events(request)]

    assert actual == frames
    assert provider.acquire_count == 1
    assert rpc.calls == [
        ("watch_events", request, _IDENTITY_HEADERS),
    ]


async def test_watch_events_propagates_stream_error() -> None:
    error = RuntimeError("stream failed")

    async def stream() -> AsyncIterator[service_pb.WatchEventsResponse]:
        yield service_pb.WatchEventsResponse()
        raise error

    rpc = FakeRPC()
    rpc.stream = stream()
    client = AsyncClient(FakeProvider(rpc))

    with pytest.raises(RuntimeError) as caught:
        async for _ in client.watch_events(service_pb.WatchEventsRequest()):
            pass

    assert caught.value is error


async def test_watch_events_early_close_closes_inner_stream() -> None:
    closed = False

    async def stream() -> AsyncIterator[service_pb.WatchEventsResponse]:
        nonlocal closed
        try:
            while True:
                yield service_pb.WatchEventsResponse()
        finally:
            closed = True

    rpc = FakeRPC()
    rpc.stream = stream()
    client = AsyncClient(FakeProvider(rpc))
    events = client.watch_events(service_pb.WatchEventsRequest())

    await anext(events)
    await events.aclose()

    assert closed


async def test_context_manager_closes_provider() -> None:
    provider = FakeProvider(FakeRPC())
    client = AsyncClient(provider)

    async with client as entered:
        assert entered is client

    assert provider.close_count == 1


async def test_identity_is_captured_when_client_is_constructed(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    rpc = FakeRPC()
    response = service_pb.WhoAmIResponse()
    rpc.responses["who_am_i"] = response
    provider = FakeProvider(rpc)
    client = AsyncClient(provider)
    monkeypatch.setenv("KWS_SERVICE_NAME", "changed-after-construction")
    monkeypatch.setenv("KWS_SERVICE_CREDENTIAL", "changed-credential")

    assert await client.who_am_i(service_pb.WhoAmIRequest()) == response
    assert rpc.calls[0][2] == _IDENTITY_HEADERS
