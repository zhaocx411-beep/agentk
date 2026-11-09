from __future__ import annotations

from collections.abc import Callable
from typing import Any, Protocol

from agentk.api.v1 import model_pb, profile_pb, service_pb
from protobuf import Oneof
from protobuf.wkt import struct_pb

from narrato_agentk.agentk_client import create_agentk_client
from narrato_agentk.settings import Settings
from narrato_agentk.workflows import (
    CREATE_CAPABILITY_ID,
    GENERATE_CAPABILITY_ID,
    RENDER_CAPABILITY_ID,
)

PIPELINE_PROFILES = (
    (CREATE_CAPABILITY_ID, "documentary_video", "纪录片解说一键成片"),
    (GENERATE_CAPABILITY_ID, "documentary_script", "生成纪录片解说脚本"),
    (RENDER_CAPABILITY_ID, "documentary_render", "按脚本渲染纪录片解说视频"),
)


class ConsoleService(Protocol):
    async def start_execution(
        self,
        pipeline: str,
        args: dict[str, Any],
        *,
        session_id: str | None,
        title: str,
    ) -> dict[str, Any]: ...

    async def get_execution(self, session_id: str, execution_id: str) -> dict[str, Any]: ...

    async def cancel_execution(self, session_id: str) -> dict[str, Any]: ...


class AgentKConsoleService:
    """Small browser-facing adapter over the official AgentK Consumer SDK."""

    def __init__(
        self,
        settings: Settings,
        *,
        client_factory: Callable[[str, str], Any] = create_agentk_client,
    ) -> None:
        self.settings = settings
        self.client_factory = client_factory

    def _client(self) -> Any:
        if not self.settings.agentk_api_key:
            raise ValueError("AGENTK_API_KEY is required to use the test console")
        return self.client_factory(self.settings.agentk_base_url, self.settings.agentk_api_key)

    async def start_execution(
        self,
        pipeline: str,
        args: dict[str, Any],
        *,
        session_id: str | None,
        title: str,
    ) -> dict[str, Any]:
        async with self._client() as client:
            resolved_session_id = session_id
            if not resolved_session_id:
                created = await client.create_session(
                    service_pb.CreateSessionRequest(
                        title=title,
                        profile=profile_pb.Profile(
                            default_model=self.settings.text_model,
                            pipelines=[
                                profile_pb.ProfilePipeline(
                                    capability=capability,
                                    name=name,
                                    display_name=display_name,
                                )
                                for capability, name, display_name in PIPELINE_PROFILES
                            ],
                        ),
                    )
                )
                if created.session is None or not created.session.id:
                    raise RuntimeError("AgentK returned no session")
                resolved_session_id = created.session.id

            started = await client.start_execution(
                service_pb.StartExecutionRequest(
                    session_id=resolved_session_id,
                    input=Oneof(
                        "pipeline",
                        service_pb.PipelineExecutionInput(
                            pipeline=pipeline,
                            args=struct_pb.Struct.from_python(args),
                        ),
                    ),
                )
            )
            if started.execution is None or not started.execution.id:
                raise RuntimeError("AgentK returned no execution")
            return {
                "sessionId": resolved_session_id,
                "executionId": started.execution.id,
                "status": _status_name(started.execution.status),
            }

    async def get_execution(self, session_id: str, execution_id: str) -> dict[str, Any]:
        async with self._client() as client:
            fetched = await client.get_execution(
                service_pb.GetExecutionRequest(
                    session_id=session_id,
                    execution_id=execution_id,
                )
            )
            execution = fetched.execution
            if execution is None:
                raise ValueError("AgentK returned no execution")
            events = await _list_execution_events(client, session_id, execution_id)
            output: Any = None
            if execution.kind is not None and execution.kind.field == "pipeline":
                raw_output = execution.kind.value.output
                output = raw_output.to_python() if raw_output is not None else None
            return {
                "sessionId": session_id,
                "executionId": execution_id,
                "status": _status_name(execution.status),
                "error": execution.error,
                "events": events,
                "output": output,
            }

    async def cancel_execution(self, session_id: str) -> dict[str, Any]:
        async with self._client() as client:
            await client.cancel_execution(service_pb.CancelExecutionRequest(session_id=session_id))
        return {"sessionId": session_id, "cancelRequested": True}


async def _list_execution_events(
    client: Any,
    session_id: str,
    execution_id: str,
) -> list[dict[str, Any]]:
    page_token = ""
    events: list[Any] = []
    while True:
        response = await client.list_events(
            service_pb.ListEventsRequest(
                session_id=session_id,
                page_size=100,
                page_token=page_token,
            )
        )
        events.extend(event for event in response.events if event.execution_id == execution_id)
        page_token = response.next_page_token
        if not page_token:
            break
    return [_event_to_dict(event) for event in reversed(events)]


def _event_to_dict(event: Any) -> dict[str, Any]:
    result: dict[str, Any] = {"id": event.event_id, "type": "event"}
    payload = event.payload
    if payload is None:
        return result
    if payload.field == "invocation_progress":
        progress = payload.value
        return {
            **result,
            "type": "progress",
            "step": progress.step_id,
            "message": progress.message,
            "data": progress.data.to_python() if progress.data is not None else {},
        }
    if payload.field == "execution_finished":
        finished = payload.value
        return {
            **result,
            "type": "finished",
            "status": _status_name(finished.status),
            "error": finished.error,
        }
    return {**result, "type": payload.field}


def _status_name(status: model_pb.ExecutionStatus) -> str:
    return status.name.lower()
