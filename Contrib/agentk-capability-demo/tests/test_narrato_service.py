from __future__ import annotations

from dataclasses import dataclass, field
from datetime import UTC, datetime
from types import SimpleNamespace
from typing import Any, Self

import pytest
from agentk.api.v1 import model_pb, service_pb
from protobuf import Oneof
from protobuf.wkt import struct_pb, timestamp_pb

from agentk_capability_demo.agent100.narrato.service import AgentKConsoleService
from agentk_capability_demo.agent100.narrato.settings import Settings


@dataclass
class FakeAgentKClient:
    calls: list[tuple[str, Any]] = field(default_factory=list)
    execution_status: model_pb.ExecutionStatus = model_pb.ExecutionStatus.RUNNING

    async def __aenter__(self) -> Self:
        return self

    async def __aexit__(self, *_: object) -> None:
        return None

    async def create_session(self, request: Any) -> Any:
        self.calls.append(("create_session", request))
        return SimpleNamespace(session=SimpleNamespace(id="session-new"))

    async def start_execution(self, request: Any) -> Any:
        self.calls.append(("start_execution", request))
        return SimpleNamespace(
            execution=SimpleNamespace(
                id="execution-new",
                status=self.execution_status,
                created_at=timestamp_pb.Timestamp.from_datetime(
                    datetime(2026, 8, 27, 4, 14, 59, tzinfo=UTC)
                ),
            )
        )

    async def get_execution(self, request: Any) -> Any:
        self.calls.append(("get_execution", request))
        output = struct_pb.Value.from_python({"artifact": "https://cdn.test/final.mp4"})
        return SimpleNamespace(
            execution=SimpleNamespace(
                status=model_pb.ExecutionStatus.COMPLETED,
                error="",
                kind=Oneof(
                    "pipeline",
                    SimpleNamespace(output=output),
                ),
            )
        )

    async def list_events(self, request: Any) -> Any:
        self.calls.append(("list_events", request))
        finished = SimpleNamespace(
            event_id="event-2",
            execution_id="execution-new",
            payload=Oneof(
                "execution_finished",
                SimpleNamespace(status=model_pb.ExecutionStatus.COMPLETED, error=""),
            ),
        )
        progress = SimpleNamespace(
            event_id="event-1",
            execution_id="execution-new",
            created_at=timestamp_pb.Timestamp.from_datetime(
                datetime(2026, 8, 27, 4, 15, 31, tzinfo=UTC)
            ),
            payload=Oneof(
                "invocation_progress",
                SimpleNamespace(
                    step_id="generate_script",
                    message="正在生成脚本",
                    data=struct_pb.Struct.from_python({"percent": 40}),
                ),
            ),
        )
        unrelated = SimpleNamespace(
            event_id="event-other",
            execution_id="another-execution",
            payload=None,
        )
        return SimpleNamespace(
            # AgentK returns newest events first; the adapter normalizes chronology.
            events=[finished, unrelated, progress],
            next_page_token="",
        )

    async def list_capabilities(self, request: Any) -> Any:
        self.calls.append(("list_capabilities", request))
        return service_pb.ListCapabilitiesResponse(
            capabilities=[
                model_pb.Capability(id="contrib.narrato.generate_documentary_script"),
                model_pb.Capability(id="agentk.probe_video"),
            ]
        )

    async def cancel_execution(self, request: Any) -> Any:
        self.calls.append(("cancel_execution", request))
        return SimpleNamespace()


def _service(client: FakeAgentKClient) -> AgentKConsoleService:
    return AgentKConsoleService(
        Settings(
            agentk_api_key="secret",
            agentk_base_url="https://agentk.test",
            text_model="text-fixed",
        ),
        client_factory=lambda base_url, api_key: client,
    )


@pytest.mark.asyncio
async def test_capability_status_distinguishes_pipelines_from_dependencies():
    rows = await _service(FakeAgentKClient()).get_capability_status()
    by_id = {row["id"]: row for row in rows}

    assert by_id["contrib.narrato.generate_documentary_script"] == {
        "id": "contrib.narrato.generate_documentary_script",
        "published": True,
        "required": True,
        "group": "pipeline",
    }
    assert by_id["contrib.narrato.create_documentary_video"]["published"] is False
    assert by_id["contrib.narrato.render_documentary_video"]["group"] == "pipeline"
    assert by_id["agentk.probe_video"]["published"] is True
    assert by_id["agentk.probe_video"]["group"] == "dependency"


@pytest.mark.asyncio
async def test_start_execution_creates_expected_profile_and_forwards_arguments():
    client = FakeAgentKClient()
    service = _service(client)
    arguments = {"videoUri": "https://cdn.test/source.mp4", "instructions": "语气克制"}

    result = await service.start_execution(
        "documentary_script",
        arguments,
        session_id=None,
        title="Console test",
    )

    assert result == {
        "sessionId": "session-new",
        "executionId": "execution-new",
        "status": "running",
        "createdAt": "2026-08-27T04:14:59+00:00",
    }
    create_request = client.calls[0][1]
    assert create_request.title == "Console test"
    assert create_request.profile.default_model == "text-fixed"
    assert [pipeline.name for pipeline in create_request.profile.pipelines] == [
        "documentary_video",
        "documentary_script",
        "documentary_render",
    ]
    start_request = client.calls[1][1]
    assert start_request.session_id == "session-new"
    assert start_request.input.field == "pipeline"
    assert start_request.input.value.pipeline == "documentary_script"
    assert start_request.input.value.args.to_python() == arguments


@pytest.mark.asyncio
async def test_existing_session_is_reused_without_creating_another_session():
    client = FakeAgentKClient()
    service = _service(client)

    await service.start_execution(
        "documentary_render",
        {"script": {"version": "2"}},
        session_id="session-existing",
        title="ignored",
    )

    assert [name for name, _ in client.calls] == ["start_execution"]
    assert client.calls[0][1].session_id == "session-existing"


@pytest.mark.asyncio
async def test_polling_normalizes_output_and_event_order():
    client = FakeAgentKClient()
    service = _service(client)

    result = await service.get_execution("session-new", "execution-new")

    assert result["status"] == "completed"
    assert result["output"] == {"artifact": "https://cdn.test/final.mp4"}
    assert [event["id"] for event in result["events"]] == ["event-1", "event-2"]
    assert result["events"][0] == {
        "id": "event-1",
        "type": "progress",
        "step": "generate_script",
        "message": "正在生成脚本",
        "data": {"percent": 40.0},
        "createdAt": "2026-08-27T04:15:31+00:00",
    }


@pytest.mark.asyncio
async def test_cancel_uses_session_scoped_agentk_operation():
    client = FakeAgentKClient()
    service = _service(client)

    result = await service.cancel_execution("session-new")

    assert result == {"sessionId": "session-new", "cancelRequested": True}
    name, request = client.calls[-1]
    assert name == "cancel_execution"
    assert request.session_id == "session-new"


@pytest.mark.asyncio
async def test_agentk_api_key_is_optional_when_server_auth_is_off():
    client = FakeAgentKClient()
    received = []
    service = AgentKConsoleService(
        Settings(agentk_api_key=""),
        client_factory=lambda base_url, api_key: received.append((base_url, api_key)) or client,
    )

    await service.get_execution("session", "execution")

    assert received == [(None, "")]
    assert "get_execution" in [name for name, _request in client.calls]
