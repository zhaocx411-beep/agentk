from __future__ import annotations

import asyncio
import time
from collections.abc import Callable, Iterable
from typing import Any

from opentelemetry import metrics, trace
from temporalio import activity, workflow
from temporalio.exceptions import CancelledError
from temporalio.worker import (
    ActivityInboundInterceptor,
    ExecuteActivityInput,
    ExecuteWorkflowInput,
    Interceptor,
    WorkflowInboundInterceptor,
    WorkflowInterceptorClassInput,
)

from . import telemetry
from .capability import Capability
from .invocation import Meta, _activity_meta, current_meta

_METER = metrics.get_meter("agentk.worker")

_executions = _METER.create_counter(
    telemetry.METRIC_WORKER_EXECUTIONS,
    unit="{execution}",
    description="Capability activities and workflows executed by this worker.",
)
_duration = _METER.create_histogram(
    telemetry.METRIC_WORKER_DURATION,
    unit="ms",
    description="Wall-clock duration of capability executions.",
)
_meta_missing = _METER.create_counter(
    telemetry.METRIC_WORKER_META_MISSING,
    unit="{execution}",
    description="Capability executions that arrived without agentk invocation meta.",
)
_heartbeats = _METER.create_counter(
    telemetry.METRIC_WORKER_HEARTBEATS,
    unit="{heartbeat}",
    description="Activity heartbeats recorded through the SDK.",
)
_progress_reports = _METER.create_counter(
    telemetry.METRIC_WORKER_PROGRESS_REPORTS,
    unit="{report}",
    description="Progress reports sent to agentk.",
)
_announces = _METER.create_counter(
    telemetry.METRIC_WORKER_ANNOUNCES,
    unit="{announce}",
    description="Capability manifest announcements attempted.",
)

# (kind, temporal_type) -> whether this worker published it as a capability.
CapabilityLookup = Callable[[str, str], bool]


def record_execution(
    kind: str,
    temporal_type: str,
    meta: Meta | None,
    status: str,
    elapsed_ms: int,
) -> None:
    attributes: dict[str, str] = {
        telemetry.ATTR_KIND: kind,
        telemetry.ATTR_TEMPORAL_TYPE: temporal_type,
        telemetry.ATTR_STATUS: status,
    }
    if meta is not None:
        attributes[telemetry.ATTR_APP] = meta.app_id
    else:
        _meta_missing.add(
            1,
            {telemetry.ATTR_KIND: kind, telemetry.ATTR_TEMPORAL_TYPE: temporal_type},
        )
    _executions.add(1, attributes)
    _duration.record(elapsed_ms, attributes)


def record_heartbeat(meta: Meta | None) -> None:
    _heartbeats.add(1, _app_attributes(meta))


def record_progress_report(meta: Meta, status: str) -> None:
    _progress_reports.add(
        1, {**_app_attributes(meta), telemetry.ATTR_STATUS: status}
    )


def record_announce(status: str) -> None:
    _announces.add(1, {telemetry.ATTR_STATUS: status})


def status_of(error: BaseException | None) -> str:
    return telemetry.STATUS_FAILED if error is not None else telemetry.STATUS_COMPLETED


def _app_attributes(meta: Meta | None) -> dict[str, str]:
    if meta is None:
        return {}
    return {telemetry.ATTR_APP: meta.app_id}


def _span_attributes(meta: Meta) -> dict[str, str]:
    attributes = {
        telemetry.APP_ID: meta.app_id,
        telemetry.INVOCATION_ID: meta.invocation_id,
        telemetry.INVOCATION_KIND: meta.invocation_kind.value,
        telemetry.INVOCATION_NAME: meta.invocation_name,
    }
    if meta.session is not None:
        attributes[telemetry.SESSION_ID] = meta.session
    return attributes


class TelemetryInterceptor(Interceptor):
    """Records agentk.worker.executions and agentk.worker.duration for the
    published capabilities, attributed to the calling app through the agentk
    invocation meta header, and annotates activity spans with the invocation
    identifiers.

    Worker installs this interceptor automatically. Construct it directly only
    for a plain Temporal Worker that hosts capabilities without publishing them
    through Worker.

    Args:
        workflows: Capability declarations implemented as workflows.
        activities: Capability declarations implemented as activities.
    """

    def __init__(
        self,
        *,
        workflows: Iterable[Capability] = (),
        activities: Iterable[Capability] = (),
    ) -> None:
        types: set[tuple[str, str]] = set()
        types.update(("workflow", item.temporal_type) for item in workflows)
        types.update(("activity", item.temporal_type) for item in activities)
        self._is_capability: CapabilityLookup = (
            lambda kind, temporal_type: (kind, temporal_type) in types
        )

    @classmethod
    def _lazy(cls, is_capability: CapabilityLookup) -> TelemetryInterceptor:
        interceptor = cls()
        interceptor._is_capability = is_capability
        return interceptor

    def intercept_activity(
        self, next: ActivityInboundInterceptor
    ) -> ActivityInboundInterceptor:
        return _ActivityInbound(next, self._is_capability)

    def workflow_interceptor_class(
        self, input: WorkflowInterceptorClassInput
    ) -> type[WorkflowInboundInterceptor] | None:
        is_capability = self._is_capability

        class _BoundWorkflowInbound(_WorkflowInbound):
            _is_capability = staticmethod(is_capability)

        return _BoundWorkflowInbound


class _ActivityInbound(ActivityInboundInterceptor):
    def __init__(
        self, next: ActivityInboundInterceptor, is_capability: CapabilityLookup
    ) -> None:
        super().__init__(next)
        self._is_capability = is_capability

    async def execute_activity(self, input: ExecuteActivityInput) -> Any:
        temporal_type = activity.info().activity_type
        if not self._is_capability("activity", temporal_type):
            return await self.next.execute_activity(input)
        meta = _activity_meta()
        if meta is not None:
            trace.get_current_span().set_attributes(_span_attributes(meta))
        start = time.monotonic()
        status = telemetry.STATUS_COMPLETED
        try:
            return await self.next.execute_activity(input)
        except (asyncio.CancelledError, CancelledError):
            status = telemetry.STATUS_CANCELLED
            raise
        except BaseException:
            status = telemetry.STATUS_FAILED
            raise
        finally:
            record_execution(
                telemetry.KIND_ACTIVITY,
                temporal_type,
                meta,
                status,
                int((time.monotonic() - start) * 1000),
            )


class _WorkflowInbound(WorkflowInboundInterceptor):
    _is_capability: CapabilityLookup

    async def execute_workflow(self, input: ExecuteWorkflowInput) -> Any:
        temporal_type = workflow.info().workflow_type
        if not self._is_capability("workflow", temporal_type):
            return await self.next.execute_workflow(input)
        meta = current_meta()
        start = workflow.now()
        status = telemetry.STATUS_COMPLETED
        try:
            return await self.next.execute_workflow(input)
        except (asyncio.CancelledError, CancelledError):
            status = telemetry.STATUS_CANCELLED
            raise
        except workflow.ContinueAsNewError:
            raise
        except BaseException:
            status = telemetry.STATUS_FAILED
            raise
        finally:
            # A run reaches this point outside replay exactly once, on the
            # worker that completes it; replays of a finished history must not
            # count again.
            if not workflow.unsafe.is_replaying():
                elapsed = workflow.now() - start
                record_execution(
                    telemetry.KIND_WORKFLOW,
                    temporal_type,
                    meta,
                    status,
                    int(elapsed.total_seconds() * 1000),
                )


def with_telemetry_interceptor(
    interceptors: Iterable[Interceptor],
    is_capability: CapabilityLookup,
) -> list[Interceptor]:
    output = list(interceptors)
    if not any(isinstance(item, TelemetryInterceptor) for item in output):
        output.append(TelemetryInterceptor._lazy(is_capability))
    return output
