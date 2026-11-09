from __future__ import annotations

from collections.abc import Iterable, Mapping
from typing import Any, NoReturn

from ksresolve import rpc as ksrpc
from temporalio.api.common.v1 import Payload
from temporalio.client import (
    Interceptor as ClientInterceptor,
)
from temporalio.client import (
    OutboundInterceptor as ClientOutboundInterceptor,
)
from temporalio.client import (
    StartWorkflowInput,
)
from temporalio.converter import PayloadConverter
from temporalio.worker import (
    ActivityInboundInterceptor,
    ContinueAsNewInput,
    ExecuteActivityInput,
    ExecuteWorkflowInput,
    StartActivityInput,
    StartChildWorkflowInput,
    StartLocalActivityInput,
    WorkflowInboundInterceptor,
    WorkflowInterceptorClassInput,
    WorkflowOutboundInterceptor,
)
from temporalio.worker import (
    Interceptor as WorkerInterceptor,
)

# Same key the carrier uses in gRPC metadata and HTTP headers.
HEADER_KEY = "trace_context-bin"
_CONVERTER = PayloadConverter.default


class CarrierInterceptor(ClientInterceptor, WorkerInterceptor):
    """Moves the KS trace carrier between the current context and Temporal headers.

    The lane reaching a workflow or activity is the one the originating request
    carried, and outgoing ksresolve calls made from an activity forward it.
    A missing header leaves the context uncolored; a malformed header fails.
    """

    def intercept_client(
        self, next: ClientOutboundInterceptor
    ) -> ClientOutboundInterceptor:
        return _ClientOutbound(next)

    def intercept_activity(
        self, next: ActivityInboundInterceptor
    ) -> ActivityInboundInterceptor:
        return _ActivityInbound(next)

    def workflow_interceptor_class(
        self,
        input: WorkflowInterceptorClassInput,
    ) -> type[WorkflowInboundInterceptor] | None:
        return _WorkflowInbound


class _ClientOutbound(ClientOutboundInterceptor):
    async def start_workflow(self, input: StartWorkflowInput) -> Any:
        input.headers = _inject_current(input.headers)
        return await self.next.start_workflow(input)


class _ActivityInbound(ActivityInboundInterceptor):
    async def execute_activity(self, input: ExecuteActivityInput) -> Any:
        with ksrpc.bind_carrier(_decode(input.headers)):
            return await self.next.execute_activity(input)


class _WorkflowInbound(WorkflowInboundInterceptor):
    def init(self, outbound: WorkflowOutboundInterceptor) -> None:
        self.next.init(_WorkflowOutbound(outbound))

    async def execute_workflow(self, input: ExecuteWorkflowInput) -> Any:
        with ksrpc.bind_carrier(_decode(input.headers)):
            return await self.next.execute_workflow(input)


class _WorkflowOutbound(WorkflowOutboundInterceptor):
    def continue_as_new(self, input: ContinueAsNewInput) -> NoReturn:
        input.headers = _inject_current(input.headers)
        return self.next.continue_as_new(input)

    def start_activity(self, input: StartActivityInput) -> Any:
        input.headers = _inject_current(input.headers)
        return self.next.start_activity(input)

    async def start_child_workflow(self, input: StartChildWorkflowInput) -> Any:
        input.headers = _inject_current(input.headers)
        return await self.next.start_child_workflow(input)

    def start_local_activity(self, input: StartLocalActivityInput) -> Any:
        input.headers = _inject_current(input.headers)
        return self.next.start_local_activity(input)


def _inject_current(headers: Mapping[str, Payload]) -> Mapping[str, Payload]:
    raw = ksrpc.current_carrier()
    if raw is None:
        return headers
    output = dict(headers)
    output[HEADER_KEY] = _encode(raw)
    return output


def _encode(raw: bytes) -> Payload:
    payloads = _CONVERTER.to_payloads([raw])
    if len(payloads) != 1:
        raise RuntimeError("trace carrier converter did not produce one payload")
    return payloads[0]


def _decode(headers: Mapping[str, Payload]) -> bytes | None:
    payload = headers.get(HEADER_KEY)
    if payload is None:
        return None
    values = _CONVERTER.from_payloads([payload], [bytes])
    if len(values) != 1 or not isinstance(values[0], bytes):
        raise TypeError("trace carrier: invalid Temporal header payload")
    return values[0]


def with_carrier_interceptor(
    interceptors: Iterable[WorkerInterceptor],
) -> list[WorkerInterceptor]:
    output = list(interceptors)
    if not any(isinstance(item, CarrierInterceptor) for item in output):
        output.append(CarrierInterceptor())
    return output
