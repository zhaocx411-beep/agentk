import asyncio
from types import SimpleNamespace
from typing import Any

import pytest
from ksresolve import rpc as ksrpc
from temporalio.converter import PayloadConverter

from agentk.worker._carrier import (
    HEADER_KEY,
    _ActivityInbound,
    _ClientOutbound,
    _decode,
    _encode,
    _WorkflowInbound,
    _WorkflowOutbound,
)

# Lane "dev_example" plus an opaque field the interceptor must forward untouched.
CARRIER = b"\x0a\x0bdev_example" + b"\x12\x03xyz"


def _lane_carrier(lane: str) -> bytes:
    return bytes([0x0A, len(lane)]) + lane.encode("ascii")


class _ClientNext:
    async def start_workflow(self, input: Any) -> Any:
        return input


class _ActivityNext:
    async def execute_activity(self, input: Any) -> bytes | None:
        raw = ksrpc.current_carrier()
        await asyncio.sleep(0)
        return raw


class _WorkflowNext:
    def __init__(self) -> None:
        self.outbound: Any = None

    def init(self, outbound: Any) -> None:
        self.outbound = outbound

    async def execute_workflow(self, input: Any) -> str:
        lane = ksrpc.current_lane()
        await asyncio.sleep(0)
        return lane


class _OutboundNext:
    def __init__(self) -> None:
        self.seen: list[Any] = []

    def continue_as_new(self, input: Any) -> None:
        self.seen.append(input)
        raise RuntimeError("continued")

    def start_activity(self, input: Any) -> Any:
        self.seen.append(input)
        return input

    async def start_child_workflow(self, input: Any) -> Any:
        self.seen.append(input)
        return input

    def start_local_activity(self, input: Any) -> Any:
        self.seen.append(input)
        return input


def _input(headers: dict[str, Any] | None = None) -> Any:
    return SimpleNamespace(headers=headers or {})


def test_temporal_payload_round_trip_and_validation() -> None:
    assert _decode({HEADER_KEY: _encode(CARRIER)}) == CARRIER
    assert _decode({}) is None

    wrong_type = PayloadConverter.default.to_payloads(["dev_example"])[0]
    with pytest.raises((TypeError, ValueError)):
        _decode({HEADER_KEY: wrong_type})


async def test_client_injects_current_carrier() -> None:
    interceptor = _ClientOutbound(_ClientNext())  # type: ignore[arg-type]
    with ksrpc.bind_carrier(CARRIER):
        result = await interceptor.start_workflow(_input())
    assert _decode(result.headers) == CARRIER
    assert ksrpc.current_carrier() is None


async def test_activity_and_workflow_contexts_are_isolated() -> None:
    activity = _ActivityInbound(_ActivityNext())  # type: ignore[arg-type]

    async def execute(lane: str) -> bytes | None:
        return await activity.execute_activity(
            _input({HEADER_KEY: _encode(_lane_carrier(lane))})
        )

    assert await asyncio.gather(execute("dev_a"), execute("dev_b")) == [
        _lane_carrier("dev_a"),
        _lane_carrier("dev_b"),
    ]

    next = _WorkflowNext()
    workflow = _WorkflowInbound(next)  # type: ignore[arg-type]
    result = await workflow.execute_workflow(_input({HEADER_KEY: _encode(CARRIER)}))
    assert result == "dev_example"
    assert ksrpc.current_carrier() is None


async def test_malformed_header_fails_instead_of_falling_back() -> None:
    activity = _ActivityInbound(_ActivityNext())  # type: ignore[arg-type]
    with pytest.raises(ksrpc.InvalidLaneError):
        await activity.execute_activity(_input({HEADER_KEY: _encode(b"\x0a\x01\xff")}))


async def test_missing_header_establishes_and_restores_uncolored() -> None:
    activity = _ActivityInbound(_ActivityNext())  # type: ignore[arg-type]
    workflow = _WorkflowInbound(_WorkflowNext())  # type: ignore[arg-type]

    with ksrpc.with_lane("dev_ambient"):
        assert await activity.execute_activity(_input()) is None
        assert await workflow.execute_workflow(_input()) == ""
        assert ksrpc.current_lane() == "dev_ambient"

    assert ksrpc.current_carrier() is None


async def test_workflow_outbound_propagates_every_temporal_edge() -> None:
    next = _OutboundNext()
    outbound = _WorkflowOutbound(next)  # type: ignore[arg-type]
    with ksrpc.bind_carrier(CARRIER):
        outbound.start_activity(_input())
        outbound.start_local_activity(_input())
        await outbound.start_child_workflow(_input())
        with pytest.raises(RuntimeError, match="continued"):
            outbound.continue_as_new(_input())

    assert len(next.seen) == 4
    assert all(_decode(item.headers) == CARRIER for item in next.seen)
