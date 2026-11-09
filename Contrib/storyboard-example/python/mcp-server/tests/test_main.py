from __future__ import annotations

from typing import Any, Self

import main
import pytest
from agentk.api.v1 import model_pb, service_pb
from mcp import Client
from mcp.client import advertise
from mcp.server.apps import APP_MIME_TYPE, EXTENSION_ID
from protobuf import Oneof
from protobuf.wkt import struct_pb

PYTHON_CAPABILITY_ID = "contrib.storyboard_example.storyboard_film_python"


class FakeAgentKClient:
    def __init__(
        self,
        executions: list[model_pb.Execution],
        *,
        error: Exception | None = None,
    ) -> None:
        self.executions = executions
        self.error = error
        self.create_requests: list[service_pb.CreateSessionRequest] = []
        self.start_requests: list[service_pb.StartExecutionRequest] = []
        self.get_requests: list[service_pb.GetExecutionRequest] = []

    async def __aenter__(self) -> Self:
        return self

    async def __aexit__(self, *_exc: object) -> None:
        return None

    async def create_session(
        self,
        request: service_pb.CreateSessionRequest,
    ) -> service_pb.CreateSessionResponse:
        if self.error is not None:
            raise self.error
        self.create_requests.append(request)
        return service_pb.CreateSessionResponse(
            session=model_pb.Session(id="ses_test"),
        )

    async def start_execution(
        self,
        request: service_pb.StartExecutionRequest,
    ) -> service_pb.StartExecutionResponse:
        self.start_requests.append(request)
        return service_pb.StartExecutionResponse(
            execution=pipeline_execution(model_pb.ExecutionStatus.RUNNING),
        )

    async def get_execution(
        self,
        request: service_pb.GetExecutionRequest,
    ) -> service_pb.GetExecutionResponse:
        self.get_requests.append(request)
        execution = self.executions.pop(0)
        return service_pb.GetExecutionResponse(execution=execution)


def install_client(
    monkeypatch: pytest.MonkeyPatch,
    executions: list[model_pb.Execution],
    *,
    error: Exception | None = None,
) -> FakeAgentKClient:
    client = FakeAgentKClient(executions, error=error)
    monkeypatch.setattr(main, "make_agentk_client", lambda: client)
    monkeypatch.setattr(main, "POLL_INTERVAL_SECONDS", 1)

    async def no_sleep(_seconds: float) -> None:
        return None

    monkeypatch.setattr(main.asyncio, "sleep", no_sleep)
    return client


def pipeline_execution(
    status: model_pb.ExecutionStatus,
    *,
    output: Any | None = None,
    error: str = "",
) -> model_pb.Execution:
    return model_pb.Execution(
        id="exe_test",
        session_id="ses_test",
        status=status,
        kind=Oneof(
            "pipeline",
            model_pb.PipelineExecution(
                name=main.PROFILE_PIPELINE_NAME,
                capability_id=PYTHON_CAPABILITY_ID,
                output=(
                    struct_pb.Value.from_python(output)
                    if output is not None
                    else None
                ),
            ),
        ),
        error=error,
    )


@pytest.mark.asyncio
async def test_generate_storyboard_film_reports_progress(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    sdk = install_client(
        monkeypatch,
        [
            pipeline_execution(model_pb.ExecutionStatus.RUNNING),
            pipeline_execution(
                model_pb.ExecutionStatus.COMPLETED,
                output={
                    "artifacts": [
                        {
                            "type": "video",
                            "uri": "https://video.example.com/film.mp4",
                            "description": "rain city film, 1 clip",
                            "model": "kling-3.0",
                            "params": {"duration": "5s"},
                        }
                    ],
                    "clipCount": 1,
                },
            ),
        ],
    )
    progress: list[tuple[float, float | None, str | None]] = []

    async def record_progress(
        current: float,
        total: float | None,
        message: str | None,
    ) -> None:
        progress.append((current, total, message))

    async with Client(main.mcp, raise_exceptions=True) as client:
        tools = await client.list_tools()
        assert [tool.name for tool in tools.tools] == ["generate_storyboard_film"]
        assert tools.tools[0].input_schema["required"] == ["topic"]
        result = await client.call_tool(
            "generate_storyboard_film",
            {"topic": " rain city ", "shots": 1},
            progress_callback=record_progress,
        )

    assert result.is_error is False
    assert result.structured_content == {
        "sessionId": "ses_test",
        "executionId": "exe_test",
        "status": "completed",
        "artifacts": [
            {
                "type": "video",
                "uri": "https://video.example.com/film.mp4",
                "description": "rain city film, 1 clip",
                "model": "kling-3.0",
                "params": {"duration": "5s"},
            }
        ],
        "clipCount": 1,
    }
    assert "https://video.example.com/film.mp4" in result.content[0].text
    assert len(sdk.create_requests) == 1
    profile = sdk.create_requests[0].profile
    assert profile is not None
    assert profile.default_model == "deepseek-v4-pro"
    assert profile.pipelines[0].capability == PYTHON_CAPABILITY_ID
    assert profile.pipelines[0].name == main.PROFILE_PIPELINE_NAME
    assert len(sdk.start_requests) == 1
    start = sdk.start_requests[0]
    assert start.session_id == "ses_test"
    assert start.input is not None
    assert start.input.field == "pipeline"
    assert start.input.value.args is not None
    assert start.input.value.args.to_python() == {
        "topic": "rain city",
        "shots": 1,
        "chatModel": "deepseek-v4-pro",
        "imageModel": "z-image-turbo",
        "videoModel": "kling-3.0",
    }
    assert len(sdk.get_requests) == 2
    assert [item[0] for item in progress] == sorted(item[0] for item in progress)
    assert len({item[0] for item in progress}) == len(progress)
    assert progress[-1][2] == "Film ready"


@pytest.mark.asyncio
async def test_generate_storyboard_film_returns_pipeline_failure(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    install_client(
        monkeypatch,
        [
            pipeline_execution(
                model_pb.ExecutionStatus.FAILED,
                error="image generation rejected",
            )
        ],
    )
    async with Client(main.mcp) as client:
        result = await client.call_tool(
            "generate_storyboard_film",
            {"topic": "rain city"},
        )

    assert result.is_error is True
    assert "image generation rejected" in result.content[0].text
    assert "exe_test" in result.content[0].text


@pytest.mark.asyncio
async def test_generate_storyboard_film_returns_pipeline_cancellation(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    install_client(
        monkeypatch,
        [pipeline_execution(model_pb.ExecutionStatus.CANCELLED)],
    )
    async with Client(main.mcp) as client:
        result = await client.call_tool(
            "generate_storyboard_film",
            {"topic": "rain city"},
        )

    assert result.is_error is True
    assert "pipeline cancelled" in result.content[0].text


@pytest.mark.asyncio
async def test_generate_storyboard_film_rejects_legacy_film_output(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    install_client(
        monkeypatch,
        [
            pipeline_execution(
                model_pb.ExecutionStatus.COMPLETED,
                output={
                    "film": "https://video.example.com/legacy.mp4",
                    "clipCount": 1,
                },
            )
        ],
    )
    async with Client(main.mcp) as client:
        result = await client.call_tool(
            "generate_storyboard_film",
            {"topic": "rain city"},
        )

    assert result.is_error is True
    assert "has invalid artifacts" in result.content[0].text


@pytest.mark.asyncio
async def test_generate_storyboard_film_returns_sdk_error(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    install_client(monkeypatch, [], error=RuntimeError("permission denied"))
    async with Client(main.mcp) as client:
        result = await client.call_tool(
            "generate_storyboard_film",
            {"topic": "rain city"},
        )

    assert result.is_error is True
    assert "permission denied" in result.content[0].text


@pytest.mark.asyncio
async def test_mcp_app_metadata_and_resource() -> None:
    extension = advertise(EXTENSION_ID, {"mimeTypes": [APP_MIME_TYPE]})
    async with Client(main.mcp, extensions=[extension]) as client:
        tools = await client.list_tools()
        resources = await client.list_resources()
        content = await client.read_resource(main.APP_RESOURCE_URI)

    assert tools.tools[0].meta == {
        "ui": {"resourceUri": main.APP_RESOURCE_URI},
    }
    assert len(resources.resources) == 1
    resource = resources.resources[0]
    assert str(resource.uri) == main.APP_RESOURCE_URI
    assert resource.mime_type == APP_MIME_TYPE
    assert resource.meta == {
        "ui": {
            "csp": {
                "resourceDomains": [
                    "https://bs3-hb1.staging.kuaishou.com",
                ],
            },
            "prefersBorder": True,
        },
    }
    assert len(content.contents) == 1
    assert content.contents[0].mime_type == APP_MIME_TYPE
    assert "<video" in content.contents[0].text
    assert "output?.artifacts?.[0]" in content.contents[0].text
    assert "output.film" not in content.contents[0].text
    assert "@modelcontextprotocol/ext-apps" not in content.contents[0].text


@pytest.mark.asyncio
async def test_generate_storyboard_film_rejects_invalid_shots() -> None:
    async with Client(main.mcp) as client:
        result = await client.call_tool(
            "generate_storyboard_film",
            {"topic": "rain city", "shots": 0},
        )

    assert result.is_error is True
    assert "shots must be an integer between 1 and 8" in result.content[0].text
