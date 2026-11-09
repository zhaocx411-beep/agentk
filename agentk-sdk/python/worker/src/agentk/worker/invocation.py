import contextvars
from collections.abc import Mapping, Sequence
from dataclasses import dataclass, replace
from datetime import timedelta
from enum import StrEnum
from typing import Any, NoReturn, TypeAlias

from temporalio import activity, workflow
from temporalio.api.common.v1 import Payload
from temporalio.common import RetryPolicy
from temporalio.converter import default
from temporalio.worker import (
    ActivityInboundInterceptor,
    ContinueAsNewInput,
    ExecuteActivityInput,
    Interceptor,
    StartActivityInput,
    StartChildWorkflowInput,
    StartLocalActivityInput,
    WorkflowInboundInterceptor,
    WorkflowInterceptorClassInput,
    WorkflowOutboundInterceptor,
)

from .failure import NON_RETRYABLE_TYPES

META_HEADER_KEY = "agentk-invocation-meta-v1"
REPORT_PROGRESS_ACTIVITY_TYPE = "agentk.invocation.progress.v1"

_ACTIVITY_META: contextvars.ContextVar["Meta | None"] = contextvars.ContextVar(
    "agentk_invocation_meta", default=None
)

JsonValue: TypeAlias = (
    None | bool | int | float | str | list["JsonValue"] | dict[str, "JsonValue"]
)


class InvocationKind(StrEnum):
    PIPELINE = "pipeline"
    TOOL = "tool"
    # A capability invoked straight from the API, outside any session. It has
    # no event log, so it carries no session fields and cannot report progress.
    DIRECT = "direct"


@dataclass(frozen=True, slots=True, kw_only=True)
class Meta:
    """Coordinates of one AgentK invocation, carried in a Temporal header.

    app_id, invocation_id, invocation_kind and invocation_name are always
    present. session_id, session and callback_task_queue are present together
    only when the invocation runs inside a session and can report progress.
    """

    app_id: str
    invocation_id: str
    invocation_kind: InvocationKind
    invocation_name: str
    session_id: int | None = None
    session: str | None = None
    callback_task_queue: str | None = None

    @property
    def has_session(self) -> bool:
        return (
            self.session_id is not None
            and self.session is not None
            and self.callback_task_queue is not None
        )

    def to_payload(self) -> dict[str, JsonValue]:
        payload: dict[str, JsonValue] = {
            "appId": self.app_id,
            "invocationId": self.invocation_id,
            "invocationKind": self.invocation_kind.value,
            "invocationName": self.invocation_name,
        }
        if self.session_id is not None:
            payload["sessionId"] = self.session_id
        if self.session is not None:
            payload["session"] = self.session
        if self.callback_task_queue is not None:
            payload["callbackTaskQueue"] = self.callback_task_queue
        return payload


@dataclass(frozen=True, slots=True)
class ReportProgressInput:
    session_id: int
    invocation_id: str
    invocation_kind: InvocationKind
    invocation_name: str
    step_id: str
    message: str = ""
    data: JsonValue = None

    @classmethod
    def from_meta(
        cls,
        meta: Meta,
        step_id: str,
        message: str = "",
        data: JsonValue = None,
    ) -> "ReportProgressInput":
        if meta.session_id is None:
            raise ValueError("invocation meta has no session to report progress to")
        return cls(
            session_id=meta.session_id,
            invocation_id=meta.invocation_id,
            invocation_kind=meta.invocation_kind,
            invocation_name=meta.invocation_name,
            step_id=step_id,
            message=message,
            data=data,
        )

    def to_payload(self) -> dict[str, JsonValue]:
        payload: dict[str, JsonValue] = {
            "sessionId": self.session_id,
            "invocationId": self.invocation_id,
            "invocationKind": self.invocation_kind.value,
            "invocationName": self.invocation_name,
            "stepId": self.step_id,
        }
        if self.message:
            payload["message"] = self.message
        if self.data is not None:
            payload["data"] = self.data
        return payload


def current_meta() -> Meta | None:
    """Return AgentK invocation coordinates for the running workflow or activity.

    Returns None when the code was not invoked by AgentK.
    """
    if activity.in_activity():
        return _activity_meta()
    return _meta_from_headers(workflow.info().headers)


def _activity_meta() -> Meta | None:
    return _ACTIVITY_META.get()


def _meta_from_headers(headers: Mapping[str, Payload]) -> Meta | None:
    payload = headers.get(META_HEADER_KEY)
    if payload is None:
        return None
    try:
        decoded = default().payload_converter.from_payload(payload)
    except (TypeError, ValueError, RuntimeError):
        return None
    return _decode_meta(decoded)


async def report_progress(
    step_id: str,
    message: str = "",
    data: JsonValue = None,
) -> None:
    meta = current_meta()
    if meta is None or not meta.has_session:
        return
    from ._telemetry import record_progress_report, status_of

    error: BaseException | None = None
    try:
        await _execute_report_progress(meta, step_id, message, data)
    except BaseException as exc:
        error = exc
        raise
    finally:
        if not workflow.unsafe.is_replaying():
            record_progress_report(meta, status_of(error))


async def _execute_report_progress(
    meta: Meta,
    step_id: str,
    message: str,
    data: JsonValue,
) -> None:
    await workflow.execute_activity(
        REPORT_PROGRESS_ACTIVITY_TYPE,
        ReportProgressInput.from_meta(meta, step_id, message, data).to_payload(),
        task_queue=meta.callback_task_queue,
        start_to_close_timeout=timedelta(seconds=30),
        schedule_to_close_timeout=timedelta(minutes=2),
        retry_policy=RetryPolicy(
            initial_interval=timedelta(seconds=1),
            backoff_coefficient=2,
            maximum_interval=timedelta(seconds=10),
            maximum_attempts=3,
            non_retryable_error_types=NON_RETRYABLE_TYPES,
        ),
    )


class _InvocationInterceptor(Interceptor):
    def intercept_activity(
        self, next: ActivityInboundInterceptor
    ) -> ActivityInboundInterceptor:
        return _InvocationActivityInbound(next)

    def workflow_interceptor_class(
        self,
        _input: WorkflowInterceptorClassInput,
    ) -> type[WorkflowInboundInterceptor]:
        return _InvocationInboundInterceptor


class _InvocationActivityInbound(ActivityInboundInterceptor):
    async def execute_activity(self, input: ExecuteActivityInput) -> Any:
        token = _ACTIVITY_META.set(_meta_from_headers(input.headers))
        try:
            return await self.next.execute_activity(input)
        finally:
            _ACTIVITY_META.reset(token)


class _InvocationInboundInterceptor(WorkflowInboundInterceptor):
    def init(self, outbound: WorkflowOutboundInterceptor) -> None:
        self.next.init(_InvocationOutboundInterceptor(outbound))


class _InvocationOutboundInterceptor(WorkflowOutboundInterceptor):
    def continue_as_new(self, input: ContinueAsNewInput) -> NoReturn:
        self.next.continue_as_new(_with_invocation_meta(input))

    def start_activity(
        self,
        input: StartActivityInput,
    ) -> workflow.ActivityHandle[Any]:
        return self.next.start_activity(_with_invocation_meta(input))

    async def start_child_workflow(
        self,
        input: StartChildWorkflowInput,
    ) -> workflow.ChildWorkflowHandle[Any, Any]:
        return await self.next.start_child_workflow(_with_invocation_meta(input))

    def start_local_activity(
        self,
        input: StartLocalActivityInput,
    ) -> workflow.ActivityHandle[Any]:
        return self.next.start_local_activity(_with_invocation_meta(input))


def _with_invocation_meta(input: Any) -> Any:
    payload = workflow.info().headers.get(META_HEADER_KEY)
    if payload is None:
        return input
    return replace(input, headers={**input.headers, META_HEADER_KEY: payload})


def _with_invocation_interceptor(
    interceptors: Sequence[Interceptor],
) -> list[Interceptor]:
    result = list(interceptors)
    if not any(isinstance(item, _InvocationInterceptor) for item in result):
        result.append(_InvocationInterceptor())
    return result


def _decode_meta(value: Any) -> Meta | None:
    if not isinstance(value, Mapping):
        return None
    try:
        app_id = value["appId"]
        invocation_id = value["invocationId"]
        invocation_kind = InvocationKind(value["invocationKind"])
        invocation_name = value["invocationName"]
    except (KeyError, TypeError, ValueError):
        return None
    if not all(
        isinstance(item, str) and item for item in (app_id, invocation_id, invocation_name)
    ):
        return None

    session_keys = ("sessionId", "session", "callbackTaskQueue")
    present = [key for key in session_keys if key in value]
    if present and len(present) != len(session_keys):
        return None
    session_id: int | None = None
    session: str | None = None
    callback_task_queue: str | None = None
    if present:
        session_id = value["sessionId"]
        session = value["session"]
        callback_task_queue = value["callbackTaskQueue"]
        if type(session_id) is not int or session_id < 1:
            return None
        if not all(
            isinstance(item, str) and item for item in (session, callback_task_queue)
        ):
            return None
    return Meta(
        app_id=app_id,
        invocation_id=invocation_id,
        invocation_kind=invocation_kind,
        invocation_name=invocation_name,
        session_id=session_id,
        session=session,
        callback_task_queue=callback_task_queue,
    )
