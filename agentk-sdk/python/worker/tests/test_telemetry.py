import json
from datetime import UTC, datetime, timedelta
from types import SimpleNamespace
from typing import Any

import pytest
from opentelemetry import metrics, trace
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import InMemoryMetricReader
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import (
    InMemorySpanExporter,
)
from temporalio.api.common.v1 import Payload

from agentk.worker import (
    Capability,
    InvocationKind,
    Meta,
    TelemetryInterceptor,
    telemetry,
)
from agentk.worker import _telemetry as telemetry_module
from agentk.worker import invocation as invocation_module

# The global providers can be installed once per process; every test in this
# module reads from the same in-memory reader and exporter.
_READER = InMemoryMetricReader()
_SPANS = InMemorySpanExporter()
metrics.set_meter_provider(MeterProvider(metric_readers=[_READER]))
_TRACER_PROVIDER = TracerProvider()
_TRACER_PROVIDER.add_span_processor(SimpleSpanProcessor(_SPANS))
trace.set_tracer_provider(_TRACER_PROVIDER)

META = Meta(
    app_id="app_telemetry",
    invocation_id="inv_telemetry",
    invocation_kind=InvocationKind.DIRECT,
    invocation_name="contrib.demo.probe",
)


def json_payload(value: Any) -> Payload:
    return Payload(
        metadata={"encoding": b"json/plain"},
        data=json.dumps(value, separators=(",", ":")).encode(),
    )


def meta_headers() -> dict[str, Payload]:
    return {invocation_module.META_HEADER_KEY: json_payload(META.to_payload())}


def data_points(name: str) -> list[Any]:
    data = _READER.get_metrics_data()
    points: list[Any] = []
    if data is None:
        return points
    for resource in data.resource_metrics:
        for scope in resource.scope_metrics:
            for metric in scope.metrics:
                if metric.name == name:
                    points.extend(metric.data.data_points)
    return points


def point_for(name: str, temporal_type: str) -> Any | None:
    for point in data_points(name):
        if point.attributes.get(telemetry.ATTR_TEMPORAL_TYPE) == temporal_type:
            return point
    return None


def declare(temporal_type: str) -> Capability:
    return Capability(
        id="contrib.demo.probe",
        temporal_type=temporal_type,
        description="probe",
        input_schema={"type": "object"},
        output_schema={"type": "object"},
        timeout=timedelta(minutes=1),
    )


class Terminal:
    def __init__(self, error: BaseException | None = None) -> None:
        self.error = error
        self.calls = 0

    async def execute_activity(self, input: Any) -> str:
        self.calls += 1
        if self.error is not None:
            raise self.error
        return "ok"

    async def execute_workflow(self, input: Any) -> str:
        return await self.execute_activity(input)


def activity_chain(
    temporal_type: str, terminal: Terminal, *, capability: bool = True
) -> Any:
    interceptor = TelemetryInterceptor(
        activities=[declare(temporal_type)] if capability else []
    )
    inner = interceptor.intercept_activity(terminal)  # type: ignore[arg-type]
    return invocation_module._InvocationActivityInbound(inner)


def install_activity(monkeypatch: pytest.MonkeyPatch, temporal_type: str) -> None:
    monkeypatch.setattr(
        telemetry_module.activity,
        "info",
        lambda: SimpleNamespace(activity_type=temporal_type),
    )


async def test_activity_execution_is_attributed_to_app(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    install_activity(monkeypatch, "probe.attributed")
    terminal = Terminal()
    tracer = trace.get_tracer("test")

    with tracer.start_as_current_span("RunActivity:probe") as span:
        result = await activity_chain("probe.attributed", terminal).execute_activity(
            SimpleNamespace(headers=meta_headers())
        )

    assert result == "ok"
    point = point_for(telemetry.METRIC_WORKER_EXECUTIONS, "probe.attributed")
    assert point is not None
    assert point.value == 1
    assert point.attributes[telemetry.ATTR_APP] == META.app_id
    assert point.attributes[telemetry.ATTR_KIND] == telemetry.KIND_ACTIVITY
    assert point.attributes[telemetry.ATTR_STATUS] == telemetry.STATUS_COMPLETED
    assert point_for(telemetry.METRIC_WORKER_DURATION, "probe.attributed") is not None
    assert point_for(telemetry.METRIC_WORKER_META_MISSING, "probe.attributed") is None

    ended = [s for s in _SPANS.get_finished_spans() if s.context == span.get_span_context()]
    assert len(ended) == 1
    attributes = dict(ended[0].attributes or {})
    assert attributes[telemetry.APP_ID] == META.app_id
    assert attributes[telemetry.INVOCATION_ID] == META.invocation_id
    assert attributes[telemetry.INVOCATION_KIND] == "direct"
    assert attributes[telemetry.INVOCATION_NAME] == META.invocation_name
    assert telemetry.SESSION_ID not in attributes


async def test_activity_without_meta_counts_missing_and_failure(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    install_activity(monkeypatch, "probe.missing")
    terminal = Terminal(RuntimeError("boom"))

    with pytest.raises(RuntimeError, match="boom"):
        await activity_chain("probe.missing", terminal).execute_activity(
            SimpleNamespace(headers={})
        )

    point = point_for(telemetry.METRIC_WORKER_EXECUTIONS, "probe.missing")
    assert point is not None
    assert telemetry.ATTR_APP not in point.attributes
    assert point.attributes[telemetry.ATTR_STATUS] == telemetry.STATUS_FAILED
    missing = point_for(telemetry.METRIC_WORKER_META_MISSING, "probe.missing")
    assert missing is not None and missing.value == 1


async def test_non_capability_activity_is_ignored(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    install_activity(monkeypatch, "probe.internal")
    terminal = Terminal()

    await activity_chain("probe.internal", terminal, capability=False).execute_activity(
        SimpleNamespace(headers=meta_headers())
    )

    assert terminal.calls == 1
    assert point_for(telemetry.METRIC_WORKER_EXECUTIONS, "probe.internal") is None


async def test_workflow_execution_is_recorded_once_outside_replay(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    clock = [datetime(2026, 1, 1, tzinfo=UTC)]
    replaying = [False]
    monkeypatch.setattr(
        telemetry_module.workflow,
        "info",
        lambda: SimpleNamespace(workflow_type="probe.workflow", headers=meta_headers()),
    )
    monkeypatch.setattr(
        invocation_module.workflow,
        "info",
        lambda: SimpleNamespace(workflow_type="probe.workflow", headers=meta_headers()),
    )
    monkeypatch.setattr(telemetry_module.workflow, "now", lambda: clock[0])
    monkeypatch.setattr(
        telemetry_module.workflow.unsafe, "is_replaying", lambda: replaying[0]
    )
    monkeypatch.setattr(telemetry_module.activity, "in_activity", lambda: False)

    interceptor = TelemetryInterceptor(workflows=[declare("probe.workflow")])
    inbound_class = interceptor.workflow_interceptor_class(SimpleNamespace())  # type: ignore[arg-type]
    assert inbound_class is not None

    class Ticking(Terminal):
        async def execute_workflow(self, input: Any) -> str:
            clock[0] = clock[0] + timedelta(seconds=3)
            return await super().execute_workflow(input)

    inbound = inbound_class(Ticking())  # type: ignore[arg-type]
    assert await inbound.execute_workflow(SimpleNamespace()) == "ok"

    replaying[0] = True
    assert await inbound.execute_workflow(SimpleNamespace()) == "ok"

    point = point_for(telemetry.METRIC_WORKER_EXECUTIONS, "probe.workflow")
    assert point is not None
    assert point.value == 1
    assert point.attributes[telemetry.ATTR_APP] == META.app_id
    assert point.attributes[telemetry.ATTR_KIND] == telemetry.KIND_WORKFLOW
    duration = point_for(telemetry.METRIC_WORKER_DURATION, "probe.workflow")
    assert duration is not None
    assert duration.sum == 3000
