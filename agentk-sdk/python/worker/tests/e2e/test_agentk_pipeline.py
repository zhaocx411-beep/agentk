import asyncio
import os
from datetime import timedelta
from urllib.error import URLError
from urllib.request import urlopen
from uuid import uuid4

import pytest
from agentk.api.v1 import model_pb, profile_pb, service_pb
from agentk.client import AsyncClient, Options
from agentk.client.http import create_client
from protobuf import Oneof
from protobuf.wkt import struct_pb
from temporalio.client import Client
from temporalio.service import RPCError

from agentk.worker import Capability, Worker, connect_temporal

from .echo_workflow import WORKFLOW_TYPE, EchoPipeline

PIPELINE_NAME = "python_echo"
DEFAULT_AGENTK_BASE_URL = "http://127.0.0.1:8080"
DEFAULT_MODEL = "deepseek-v4-pro"

pytestmark = pytest.mark.e2e


async def test_python_pipeline_runs_through_agentk() -> None:
    base_url = os.getenv("AGENTK_TEST_BASE_URL", DEFAULT_AGENTK_BASE_URL)
    model = os.getenv("AGENTK_TEST_MODEL", DEFAULT_MODEL)

    await _require_agentk(base_url)
    temporal = await _connect_temporal()

    suffix = uuid4().hex[:12]
    capability_id = f"contrib.e2e.python_echo_{suffix}"
    task_queue = f"agentk-python-e2e-{suffix}"
    value = f"hello-from-python-{suffix}"

    worker = Worker(
        temporal,
        task_queue=task_queue,
        workflows=[EchoPipeline],
    )
    worker.publish_workflow(
        Capability(
            id=capability_id,
            temporal_type=WORKFLOW_TYPE,
            description="Python E2E echo capability",
            input_schema={
                "type": "object",
                "additionalProperties": False,
                "required": ["value"],
                "properties": {"value": {"type": "string"}},
            },
            output_schema={
                "type": "object",
                "additionalProperties": False,
                "required": ["echoed"],
                "properties": {"echoed": {"type": "string"}},
            },
            timeout=timedelta(minutes=1),
        )
    )

    async with (
        worker,
        create_client(Options(endpoint=base_url)) as agentk,
    ):
        published = await _wait_for_capability(agentk, capability_id)
        assert published.id == capability_id

        created = await agentk.create_session(
            service_pb.CreateSessionRequest(
                title="Python worker E2E",
                profile=profile_pb.Profile(
                    default_model=model,
                    pipelines=[
                        profile_pb.ProfilePipeline(
                            capability=capability_id,
                            name=PIPELINE_NAME,
                            display_name="Python E2E Echo",
                        )
                    ],
                ),
            )
        )
        assert created.session is not None
        session_id = created.session.id

        pipelines = await agentk.list_pipelines(
            service_pb.ListPipelinesRequest(session_id=session_id)
        )
        assert len(pipelines.pipelines) == 1
        pipeline = pipelines.pipelines[0]
        assert pipeline.name == PIPELINE_NAME
        assert pipeline.display_name == "Python E2E Echo"
        assert pipeline.capability_id == capability_id
        assert pipeline.description == "Python E2E echo capability"
        assert pipeline.input_schema is not None
        assert pipeline.input_schema.to_python() == {
            "type": "object",
            "additionalProperties": False,
            "required": ["value"],
            "properties": {"value": {"type": "string"}},
        }
        assert pipeline.output_schema is not None
        assert pipeline.output_schema.to_python() == {
            "type": "object",
            "additionalProperties": False,
            "required": ["echoed"],
            "properties": {"echoed": {"type": "string"}},
        }

        started = await agentk.start_execution(
            service_pb.StartExecutionRequest(
                session_id=session_id,
                input=Oneof(
                    "pipeline",
                    service_pb.PipelineExecutionInput(
                        pipeline=PIPELINE_NAME,
                        args=struct_pb.Struct.from_python({"value": value}),
                    ),
                ),
            )
        )
        assert started.execution is not None
        execution_id = started.execution.id
        assert execution_id
        assert started.execution.status == model_pb.ExecutionStatus.RUNNING
        assert started.execution.kind is not None
        assert started.execution.kind.field == "pipeline"

        frames: list[service_pb.WatchEventsResponse] = []
        async with asyncio.timeout(60):
            async for frame in agentk.watch_events(
                service_pb.WatchEventsRequest(
                    session_id=session_id,
                    execution_id=execution_id,
                )
            ):
                frames.append(frame)

        assert frames[-1].frame is not None
        assert frames[-1].frame.field == "stream_end"
        assert frames[-1].frame.value.execution_id == execution_id

        committed = [
            frame.frame.value
            for frame in frames
            if frame.frame is not None and frame.frame.field == "committed_event"
        ]
        execution_events = [
            event
            for event in committed
            if event.execution_id == execution_id and event.payload is not None
        ]
        assert [event.payload.field for event in execution_events] == [
            "execution_started",
            "invocation_progress",
            "execution_finished",
        ]

        execution_started = execution_events[0].payload.value
        assert execution_started.kind is not None
        assert execution_started.kind.field == "pipeline"
        assert execution_started.kind.value.capability_id == capability_id

        progress = execution_events[1].payload.value
        assert progress.invocation_id == execution_id
        assert progress.invocation_name == PIPELINE_NAME
        assert progress.step_id == "received"
        assert progress.data is not None
        assert progress.data.to_python() == {"value": value}

        execution_finished = execution_events[2].payload.value
        assert execution_finished.status == model_pb.ExecutionStatus.COMPLETED
        assert execution_finished.result is not None
        assert execution_finished.result.field == "pipeline"
        assert execution_finished.result.value.output is not None
        assert execution_finished.result.value.output.to_python() == {"echoed": value}

        fetched = await agentk.get_execution(
            service_pb.GetExecutionRequest(
                session_id=session_id,
                execution_id=execution_id,
            )
        )
        assert fetched.execution is not None
        assert fetched.execution.status == model_pb.ExecutionStatus.COMPLETED
        assert fetched.execution.finished_at is not None
        assert fetched.execution.kind is not None
        assert fetched.execution.kind.field == "pipeline"
        assert fetched.execution.kind.value.output is not None
        assert fetched.execution.kind.value.output.to_python() == {"echoed": value}


async def _wait_for_capability(
    client: AsyncClient,
    capability_id: str,
) -> model_pb.Capability:
    deadline = asyncio.get_running_loop().time() + 30
    while asyncio.get_running_loop().time() < deadline:
        page_token = ""
        while True:
            response = await client.list_capabilities(
                service_pb.ListCapabilitiesRequest(
                    page_size=100,
                    page_token=page_token,
                )
            )
            for capability in response.capabilities:
                if capability.id == capability_id:
                    return capability
            page_token = response.next_page_token
            if not page_token:
                break
        await asyncio.sleep(0.2)
    raise AssertionError(f"timed out waiting for capability {capability_id}")


async def _require_agentk(base_url: str) -> None:
    explicit = "AGENTK_TEST_BASE_URL" in os.environ

    def check() -> bytes:
        with urlopen(f"{base_url.rstrip('/')}/healthz", timeout=5) as response:
            if response.status != 200:
                raise OSError(f"health check returned HTTP {response.status}")
            return response.read()

    try:
        body = await asyncio.to_thread(check)
    except (OSError, TimeoutError, URLError) as error:
        _skip_or_fail(explicit, f"agent-k is unavailable at {base_url}: {error}")
        return
    if body.strip() != b"ok":
        pytest.fail(
            f"agent-k health check at {base_url} returned: "
            f"{body.decode(errors='replace')}"
        )


async def _connect_temporal() -> Client:
    explicit = os.getenv("AGENTK_SDK_TEST_KESS") == "1"
    try:
        async with asyncio.timeout(20):
            return await connect_temporal()
    except (OSError, RuntimeError, TimeoutError, RPCError) as error:
        _skip_or_fail(
            explicit,
            f"AgentK Temporal is unavailable through KESS: {error}",
        )
        raise AssertionError("unreachable")


def _skip_or_fail(explicit: bool, message: str) -> None:
    if explicit:
        pytest.fail(message)
    pytest.skip(message)
