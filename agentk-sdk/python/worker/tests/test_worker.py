import asyncio
from datetime import timedelta
from typing import Any

import pytest
from temporalio import activity
from temporalio.worker import Worker as TemporalWorker

from agentk.worker import Capability, Tool, Worker
from agentk.worker import worker as worker_module
from agentk.worker._discovery import DESCRIBE_ACTIVITY_TYPE


class FakeClient:
    pass


class ExampleWorkflow:
    pass


@activity.defn(name="example.render")
async def example_activity() -> None:
    pass


@pytest.fixture(autouse=True)
def fake_native(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("KWS_LANE_ID", raising=False)
    monkeypatch.setenv("KWS_SERVICE_STAGE", "STAGING")

    def init(
        self: TemporalWorker,
        client: Any,
        *,
        task_queue: str,
        workflows: list[type],
        activities: list[Any],
        **options: Any,
    ) -> None:
        self._config = {
            "client": client,
            "task_queue": task_queue,
            "workflows": list(workflows),
            "activities": list(activities),
            **options,
        }
        self.run_calls = 0
        self.shutdown_calls = 0
        self._test_shutdown = asyncio.Event()
        self._test_run_task = None

    async def run(self: TemporalWorker) -> None:
        self.run_calls += 1
        await self._test_shutdown.wait()

    async def shutdown(self: TemporalWorker) -> None:
        self.shutdown_calls += 1
        self._test_shutdown.set()

    async def enter(self: TemporalWorker) -> TemporalWorker:
        self._test_run_task = asyncio.create_task(self.run())
        await asyncio.sleep(0)
        return self

    async def exit(self: TemporalWorker, *exc_info: object) -> None:
        await self.shutdown()
        await self._test_run_task

    monkeypatch.setattr(TemporalWorker, "__init__", init)
    monkeypatch.setattr(TemporalWorker, "run", run)
    monkeypatch.setattr(TemporalWorker, "shutdown", shutdown)
    monkeypatch.setattr(TemporalWorker, "__aenter__", enter)
    monkeypatch.setattr(TemporalWorker, "__aexit__", exit)


@pytest.fixture(autouse=True)
def stub_announcer(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(worker_module, "announce_forever", _never_returns)


async def _never_returns(*args: Any, **kwargs: Any) -> None:
    await asyncio.Event().wait()


def declare(capability_id: str = "contrib.example.generate") -> Capability:
    return Capability(
        id=capability_id,
        temporal_type=capability_id,
        description="Generate an example",
        input_schema={"type": "object"},
        output_schema={"type": "object"},
        timeout=timedelta(minutes=5),
    )


def build_worker(**options: Any) -> Worker:
    task_queue = options.pop("task_queue", "example-queue")
    return Worker(
        FakeClient(),
        task_queue=task_queue,
        workflows=[ExampleWorkflow],
        activities=[example_activity],
        **options,
    )


def describe_handler(worker: Worker) -> Any:
    return next(
        handler
        for handler in worker._config["activities"]
        if activity._Definition.must_from_callable(handler).name
        == DESCRIBE_ACTIVITY_TYPE
    )


def test_native_registration_is_passed_through_untouched() -> None:
    worker = build_worker()
    config = worker._config

    assert config["workflows"] == [ExampleWorkflow]
    assert example_activity in config["activities"]
    assert describe_handler(worker) is not None


def test_publishing_does_not_register_anything() -> None:
    worker = build_worker()
    before = list(worker._config["activities"])

    worker.publish_workflow(declare())

    assert worker._config["activities"] == before
    assert worker._config["workflows"] == [ExampleWorkflow]


async def test_publish_groups_inject_kind() -> None:
    worker = build_worker()
    worker.publish_workflow(declare("contrib.example.orchestration"))
    worker.publish_activity(declare("contrib.example.step"))

    async with worker:
        payload = await describe_handler(worker)()

    assert {item["id"]: item["kind"] for item in payload["capabilities"]} == {
        "contrib.example.orchestration": "workflow",
        "contrib.example.step": "activity",
    }


async def test_published_capabilities_ignore_later_caller_mutation() -> None:
    worker = build_worker()
    schema: dict[str, Any] = {
        "type": "object",
        "properties": {"topic": {"type": "string"}},
    }
    declared = Capability(
        id="contrib.example.generate",
        temporal_type="example.generate",
        description="Generate an example",
        input_schema=schema,
        output_schema={"type": "object"},
        timeout=timedelta(minutes=5),
        tool=Tool(
            max_attempts=3,
        ),
    )
    worker.publish_workflow(declared)

    schema["properties"]["topic"]["type"] = "integer"

    async with worker:
        payload = await describe_handler(worker)()
    assert payload["capabilities"][0]["inputSchema"]["properties"]["topic"] == {
        "type": "string"
    }
    assert payload["capabilities"][0]["tool"]["maxAttempts"] == 3


async def test_run_freezes_the_manifest_and_delegates() -> None:
    worker = build_worker()
    worker.publish_workflow(declare())

    run_task = asyncio.create_task(worker.run())
    await asyncio.sleep(0)
    await worker.shutdown()
    await run_task

    assert worker.run_calls == 1
    with pytest.raises(RuntimeError, match="frozen"):
        worker.publish_workflow(declare("contrib.example.late"))


@pytest.mark.parametrize(
    ("task_queue", "capability_id", "message"),
    [
        ("agentk", "contrib.example.generate", "task queue"),
        ("example-queue", "agentk.generate", r"contrib\.<owner>\.<name>"),
    ],
)
async def test_run_rejects_core_contracts_before_native_worker(
    task_queue: str,
    capability_id: str,
    message: str,
) -> None:
    worker = build_worker(task_queue=task_queue)
    worker.publish_workflow(declare(capability_id))

    with pytest.raises(ValueError, match=message):
        await worker.run()

    assert worker.run_calls == 0


async def test_context_manager_freezes_and_shuts_down() -> None:
    worker = build_worker()
    worker.publish_workflow(declare())

    async with worker:
        with pytest.raises(RuntimeError, match="frozen"):
            worker.publish_activity(declare("contrib.example.late"))

    assert worker.shutdown_calls == 1


async def test_describe_returns_the_frozen_payload_and_resists_mutation() -> None:
    worker = build_worker()
    worker.publish_workflow(declare())
    describe = describe_handler(worker)

    async with worker:
        first = await describe()
        first["capabilities"].clear()
        second = await describe()

    assert len(second["capabilities"]) == 1


async def test_describe_before_start_is_a_usage_error() -> None:
    worker = build_worker()

    with pytest.raises(RuntimeError, match="not frozen"):
        await describe_handler(worker)()


async def test_worker_starts_manifest_announcer(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    seen: dict[str, Any] = {}

    async def record(client: Any, **kwargs: Any) -> None:
        seen.update(kwargs)
        await _never_returns()

    monkeypatch.setattr(worker_module, "announce_forever", record)
    worker = build_worker()
    worker.publish_workflow(declare())

    async with worker:
        await asyncio.sleep(0)

    assert seen["worker_task_queue"] == "example-queue"
    assert seen["payload"]["taskQueue"] == "example-queue"


@pytest.mark.parametrize(
    ("stage", "raw_lane", "expected"),
    [
        ("STAGING", "dev_example", "dev_example"),
        ("PROD_REGRESSION_TEST", "PRT", "PRT"),
        ("PROD_REGRESSION_TEST", "dev_example", "dev_example"),
    ],
)
async def test_worker_lane_changes_poll_queue_but_announces_base_queue(
    monkeypatch: pytest.MonkeyPatch,
    stage: str,
    raw_lane: str,
    expected: str,
) -> None:
    monkeypatch.setenv("KWS_LANE_ID", raw_lane)
    monkeypatch.setenv("KWS_SERVICE_STAGE", stage)
    worker = build_worker()
    worker.publish_workflow(declare())

    assert worker._config["task_queue"] == f"example-queue@{expected}"
    async with worker:
        payload = await describe_handler(worker)()
    assert payload["taskQueue"] == "example-queue"
    assert payload["laneId"] == expected


async def test_local_worker_gets_an_isolated_lane(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.delenv("KWS_SERVICE_STAGE", raising=False)

    worker = build_worker()
    worker.publish_workflow(declare())

    assert worker._config["task_queue"].startswith("example-queue@local-")
    async with worker:
        payload = await describe_handler(worker)()
    assert payload["taskQueue"] == "example-queue"
    assert worker._config["task_queue"] == f"example-queue@{payload['laneId']}"


async def test_shutdown_stops_the_announcer_before_the_native_worker(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    events: list[str] = []
    started = asyncio.Event()
    native_shutdown = TemporalWorker.shutdown

    async def announce(*args: Any, **kwargs: Any) -> None:
        started.set()
        try:
            await _never_returns()
        finally:
            events.append("announcer stopped")

    async def shutdown(native: TemporalWorker) -> None:
        events.append("native shutdown")
        await native_shutdown(native)

    monkeypatch.setattr(worker_module, "announce_forever", announce)
    monkeypatch.setattr(TemporalWorker, "shutdown", shutdown)
    worker = build_worker()
    worker.publish_workflow(declare())

    async with worker:
        await asyncio.wait_for(started.wait(), 2)

    assert events == ["announcer stopped", "native shutdown"]


async def test_worker_refreshes_a_kess_temporal_client(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    started = asyncio.Event()
    stopped = asyncio.Event()

    async def record(worker: Worker) -> None:
        started.set()
        try:
            await _never_returns()
        finally:
            stopped.set()

    monkeypatch.setattr(worker_module, "KessTemporalClient", FakeClient)
    monkeypatch.setattr(worker_module, "refresh_worker_client_forever", record)
    worker = build_worker()

    async with worker:
        await asyncio.wait_for(started.wait(), 2)

    assert stopped.is_set()
