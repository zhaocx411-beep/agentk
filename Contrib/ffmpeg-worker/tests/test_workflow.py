from collections.abc import AsyncIterator
from uuid import uuid4

import pytest
from agentk.worker import Artifact
from pydantic import ValidationError
from temporalio import activity
from temporalio.client import WorkflowFailureError
from temporalio.contrib.pydantic import pydantic_data_converter
from temporalio.exceptions import ApplicationError
from temporalio.testing import WorkflowEnvironment
from temporalio.worker import Worker

from abi import (
    FFMPEG_OPS,
    INFER_ACTIVITY,
    FfmpegStep,
    FfmpegToolInput,
    FfmpegToolOutput,
    InferActivityOutput,
)
from workflow import FfmpegTool

TEST_TASK_QUEUE = "ffmpeg-tool-test"


class FakeInfer:
    def __init__(self, output: InferActivityOutput) -> None:
        self.output = output
        self.inputs: list[FfmpegToolInput] = []

    @activity.defn(name=INFER_ACTIVITY)
    async def infer(self, args: FfmpegToolInput) -> InferActivityOutput:
        self.inputs.append(args)
        return self.output


@pytest.fixture
async def temporal_env() -> AsyncIterator[WorkflowEnvironment]:
    async with await WorkflowEnvironment.start_time_skipping(
        data_converter=pydantic_data_converter
    ) as environment:
        yield environment


async def execute(
    temporal_env: WorkflowEnvironment,
    fake: FakeInfer,
    args: FfmpegToolInput,
) -> FfmpegToolOutput:
    worker = Worker(
        temporal_env.client,
        task_queue=TEST_TASK_QUEUE,
        workflows=[FfmpegTool],
        activities=[fake.infer],
    )
    async with worker:
        return await temporal_env.client.execute_workflow(
            FfmpegTool.run,
            args,
            id=f"ffmpeg-tool-{uuid4()}",
            task_queue=TEST_TASK_QUEUE,
        )


async def test_media_output_becomes_artifact(
    temporal_env: WorkflowEnvironment,
) -> None:
    fake = FakeInfer(
        InferActivityOutput(
            kind="video",
            blobKey="upload:ylab-stunt:out_key",
            uri="http://bs3-hb1.internal/upload-ylab-stunt/out_key",
            ssoUri="https://bs3.corp.kuaishou.com/upload-ylab-stunt/out_key",
            outputSize=12345,
        )
    )
    args = FfmpegToolInput(
        op="cut",
        params={"start": 1.5, "end": 4.0},
        video="db:table:input_key",
    )

    result = await execute(temporal_env, fake, args)

    assert result == FfmpegToolOutput(
        artifacts=[
            Artifact(
                type="video",
                uri="http://bs3-hb1.internal/upload-ylab-stunt/out_key",
                description="ffmpeg cut 产物",
                params={
                    "blobKey": "upload:ylab-stunt:out_key",
                    "ssoUri": (
                        "https://bs3.corp.kuaishou.com/upload-ylab-stunt/out_key"
                    ),
                    "outputSize": 12345,
                },
            )
        ],
    )
    assert fake.inputs == [args]


async def test_analysis_output_passes_result_through(
    temporal_env: WorkflowEnvironment,
) -> None:
    fake = FakeInfer(InferActivityOutput(result={"duration": 3.2}))
    args = FfmpegToolInput(op="probe", video="db:table:input_key")

    result = await execute(temporal_env, fake, args)

    assert result == FfmpegToolOutput(artifacts=[], result={"duration": 3.2})


async def test_pipeline_op_summary_in_artifact_description(
    temporal_env: WorkflowEnvironment,
) -> None:
    fake = FakeInfer(
        InferActivityOutput(
            kind="video",
            blobKey="a:b:c",
            uri="http://bs3-hb1.internal/a-b/c",
            ssoUri="https://bs3.corp.kuaishou.com/a-b/c",
            outputSize=1,
        )
    )
    args = FfmpegToolInput(
        pipeline=[
            FfmpegStep(op="freeze", params={"duration": 3}),
            FfmpegStep(op="overlay_text", params={"text": "标题"}),
        ],
        image="base64:aGk=",
    )

    result = await execute(temporal_env, fake, args)

    assert result.artifacts[0].description == "ffmpeg freeze+overlay_text 产物"


async def test_empty_response_fails_workflow(
    temporal_env: WorkflowEnvironment,
) -> None:
    fake = FakeInfer(InferActivityOutput())
    args = FfmpegToolInput(op="probe", video="db:table:key")

    with pytest.raises(WorkflowFailureError) as raised:
        await execute(temporal_env, fake, args)

    cause = raised.value.__cause__
    assert isinstance(cause, ApplicationError)
    assert cause.type == "InvalidCapabilityOutput"


@pytest.mark.parametrize(
    "raw",
    [
        {},
        {"op": "cut", "pipeline": [{"op": "probe"}]},
        {"op": "rm -rf"},
        {"pipeline": [{"op": "drawtext"}]},
        {"op": "cut", "params": "not-an-object"},
        {"op": "cut", "unexpected": 1},
    ],
)
def test_input_rejects_invalid_values(raw: dict) -> None:
    with pytest.raises(ValidationError):
        FfmpegToolInput.model_validate(raw)


def test_input_schema_contract() -> None:
    schema = FfmpegToolInput.model_json_schema()
    assert schema["additionalProperties"] is False
    op_schema = schema["properties"]["op"]["anyOf"][0]
    assert sorted(op_schema["enum"]) == sorted(FFMPEG_OPS)
    assert len(FFMPEG_OPS) == 29

    output_schema = FfmpegToolOutput.model_json_schema()
    assert output_schema["additionalProperties"] is False
    assert "artifacts" in output_schema["required"]
