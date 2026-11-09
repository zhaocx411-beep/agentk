import asyncio
import base64
from datetime import timedelta
from typing import Any

import pytest

from agentk.worker import _announce
from agentk.worker._announce import (
    _failure_delay,
    _register_workflow_id,
    _success_delay,
    announce_forever,
)
from agentk.worker._discovery import REGISTER_WORKFLOW_TYPE

PAYLOAD: dict[str, Any] = {
    "protocolVersion": 1,
    "taskQueue": "example-queue",
    "capabilities": [],
}


class RecordingClient:
    def __init__(self, failures: int = 0) -> None:
        self.failures = failures
        self.calls: list[dict[str, Any]] = []
        self.called = asyncio.Event()

    async def execute_workflow(
        self,
        workflow: str,
        payload: dict[str, Any],
        **options: Any,
    ) -> None:
        self.calls.append({"workflow": workflow, "payload": payload, **options})
        self.called.set()
        if len(self.calls) <= self.failures:
            raise RuntimeError("agent-k unavailable")


@pytest.fixture
def instant_delays(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(_announce, "_success_delay", lambda: 0.0)
    monkeypatch.setattr(_announce, "_failure_delay", lambda attempt: 0.0)


async def run_until(client: RecordingClient, calls: int) -> None:
    task = asyncio.create_task(
        announce_forever(
            lambda: client,
            worker_task_queue="example-queue",
            payload=PAYLOAD,
        )
    )
    async with asyncio.timeout(5):
        while len(client.calls) < calls:
            await asyncio.sleep(0)
    task.cancel()
    with pytest.raises(asyncio.CancelledError):
        await task


async def test_announce_matches_the_discovery_wire_contract(
    instant_delays: None,
) -> None:
    client = RecordingClient()

    await run_until(client, 1)

    call = client.calls[0]
    assert call["workflow"] == REGISTER_WORKFLOW_TYPE
    assert call["payload"] == PAYLOAD
    assert call["task_queue"] == "agentk"
    assert call["execution_timeout"] == timedelta(minutes=2)
    assert call["id"].startswith("agentk-discovery-register/")


async def test_first_announce_is_immediate(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(_announce, "_success_delay", lambda: 3600.0)
    client = RecordingClient()

    await run_until(client, 1)

    assert len(client.calls) == 1


async def test_failures_are_retried_without_stopping(instant_delays: None) -> None:
    client = RecordingClient(failures=2)

    await run_until(client, 3)

    assert len(client.calls) >= 3


async def test_cancellation_stops_the_reporter(instant_delays: None) -> None:
    client = RecordingClient()
    task = asyncio.create_task(
        announce_forever(
            lambda: client,
            worker_task_queue="example-queue",
            payload=PAYLOAD,
        )
    )
    await client.called.wait()
    task.cancel()

    with pytest.raises(asyncio.CancelledError):
        await task
    assert task.cancelled()


async def test_announcer_uses_the_current_client(instant_delays: None) -> None:
    first = RecordingClient()
    second = RecordingClient()
    current = first

    task = asyncio.create_task(
        announce_forever(
            lambda: current,
            worker_task_queue="example-queue",
            payload=PAYLOAD,
        )
    )
    await first.called.wait()
    current = second
    await second.called.wait()
    task.cancel()

    with pytest.raises(asyncio.CancelledError):
        await task


def test_workflow_ids_encode_the_queue_and_stay_unique() -> None:
    first = _register_workflow_id("example-queue")
    second = _register_workflow_id("example-queue")

    prefix, queue, _ = first.rsplit("/", 2)
    assert prefix == "agentk-discovery-register"
    assert base64.urlsafe_b64decode(queue + "==") == b"example-queue"
    assert first != second


def test_success_delay_stays_inside_the_jitter_window() -> None:
    delays = [_success_delay() for _ in range(200)]

    assert all(720.0 <= delay <= 1080.0 for delay in delays)


def test_failure_delay_backs_off_and_caps_at_a_minute() -> None:
    assert 0.8 <= _failure_delay(1) <= 1.2
    assert 1.6 <= _failure_delay(2) <= 2.4
    assert 48.0 <= _failure_delay(20) <= 60.0
