from __future__ import annotations

from collections.abc import Callable
from datetime import datetime
from typing import Any, Protocol

from agentk.api.v1 import model_pb, profile_pb, service_pb
from protobuf import Oneof
from protobuf.wkt import struct_pb

from agentk_capability_demo.agent100.narrato.agentk_client import create_agentk_client
from agentk_capability_demo.agent100.narrato.agentk_core import (
    DISCOVERABLE_CORE_CAPABILITY_IDS,
    REQUIRED_CORE_CAPABILITY_IDS,
    TTS_CORE_CAPABILITY_IDS,
)
from agentk_capability_demo.agent100.narrato.settings import Settings
from agentk_capability_demo.agent100.narrato.workflows import (
    CREATE_CAPABILITY_ID,
    GENERATE_CAPABILITY_ID,
    RENDER_CAPABILITY_ID,
)

PIPELINE_PROFILES = (
    (CREATE_CAPABILITY_ID, "documentary_video", "纪录片解说一键成片"),
    (GENERATE_CAPABILITY_ID, "documentary_script", "生成纪录片解说脚本"),
    (RENDER_CAPABILITY_ID, "documentary_render", "按脚本渲染纪录片解说视频"),
)
NARRATO_CAPABILITY_IDS = frozenset(item[0] for item in PIPELINE_PROFILES)


class ConsoleService(Protocol):
    async def get_capability_status(self) -> list[dict[str, Any]]: ...

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
        client_factory: Callable[[str | None, str | None], Any] = create_agentk_client,
    ) -> None:
        self.settings = settings
        self.client_factory = client_factory

    def _client(self) -> Any:
        return self.client_factory(self.settings.agentk_base_url, self.settings.agentk_api_key)

    async def get_capability_status(self) -> list[dict[str, Any]]:
        found: dict[str, Any] = {}
        page_token = ""
        async with self._client() as client:
            while True:
                response = await client.list_capabilities(
                    service_pb.ListCapabilitiesRequest(page_size=100, page_token=page_token)
                )
                found.update((item.id, item) for item in response.capabilities)
                page_token = response.next_page_token
                if not page_token:
                    break
        rows = [
            {
                "id": capability_id,
                "published": capability_id in found,
                "required": True,
                "group": "pipeline",
            }
            for capability_id in sorted(NARRATO_CAPABILITY_IDS)
        ]
        rows.extend(
            {
                "id": capability_id,
                "published": capability_id in found,
                "required": capability_id in REQUIRED_CORE_CAPABILITY_IDS,
                "group": "dependency",
                **(
                    {"inputSchema": _schema_to_dict(found[capability_id])}
                    if capability_id in TTS_CORE_CAPABILITY_IDS and capability_id in found
                    else {}
                ),
            }
            for capability_id in sorted(DISCOVERABLE_CORE_CAPABILITY_IDS)
        )
        rows.append(
            {
                "id": "agentk.concat_videos",
                "published": False,
                "required": True,
                "activityOnly": True,
                "group": "dependency",
            }
        )
        return rows
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
            created_at = _iso_timestamp(getattr(started.execution, "created_at", None))
            return {
                "sessionId": resolved_session_id,
                "executionId": started.execution.id,
                "status": _status_name(started.execution.status),
                **({"createdAt": created_at} if created_at else {}),
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


def _schema_to_dict(capability: Any) -> dict[str, Any]:
    schema = getattr(capability, "input_schema", None)
    converter = getattr(schema, "to_python", None)
    value = converter() if callable(converter) else schema
    return value if isinstance(value, dict) else {}


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
    created_at = _iso_timestamp(getattr(event, "created_at", None))
    if created_at:
        result["createdAt"] = created_at
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


def _iso_timestamp(value: Any) -> str:
    if value is None:
        return ""
    to_datetime = getattr(value, "to_datetime", None)
    if callable(to_datetime):
        try:
            value = to_datetime()
        except (OverflowError, TypeError, ValueError):
            return ""
    candidate = value.isoformat() if hasattr(value, "isoformat") else str(value)
    if not isinstance(candidate, str) or not candidate.strip():
        return ""
    candidate = candidate.strip()
    try:
        datetime.fromisoformat(candidate)
    except (TypeError, ValueError):
        return ""
    return candidate
