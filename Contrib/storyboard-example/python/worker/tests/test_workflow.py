from collections.abc import AsyncIterator, Callable
from uuid import uuid4

import pytest
from abi import (
    COMPOSE_VIDEO_CAPABILITY,
    GENERATE_IMAGE_CAPABILITY,
    GENERATE_TEXT_CAPABILITY,
    GENERATE_VIDEO_CAPABILITY,
    ConcatVideosInput,
    ConcatVideosOutput,
    GenerateImageInput,
    GenerateImageOutput,
    GenerateTextInput,
    GenerateTextOutput,
    GenerateVideoInput,
    GenerateVideoOutput,
    StoryboardBoard,
    StoryboardInput,
    StoryboardOutput,
    StoryboardShot,
)
from agentk.worker import Artifact
from pydantic import ValidationError
from temporalio import activity
from temporalio.client import WorkflowFailureError
from temporalio.contrib.pydantic import pydantic_data_converter
from temporalio.exceptions import ApplicationError
from temporalio.testing import WorkflowEnvironment
from temporalio.worker import Worker
from workflow import StoryboardFilmExample

TEST_WORKFLOW_QUEUE = "storyboard-example-test-workflow"
CORE_TASK_QUEUE = "agentk"
RESOLVE_ACTIVITY_TYPE = "agentk.routing.resolve.v1"


class FakeCoreCapabilities:
    def __init__(
        self,
        shots: list[StoryboardShot],
        *,
        failed_motions: set[str] | None = None,
        fail_all_images: bool = False,
    ) -> None:
        self.shots = shots
        self.failed_motions = failed_motions or set()
        self.fail_all_images = fail_all_images
        self.text_input: GenerateTextInput | None = None
        self.image_inputs: list[GenerateImageInput] = []
        self.video_inputs: list[GenerateVideoInput] = []
        self.compose_input: ConcatVideosInput | None = None

    @activity.defn(name=GENERATE_TEXT_CAPABILITY)
    async def generate_text(
        self,
        args: GenerateTextInput,
    ) -> GenerateTextOutput:
        self.text_input = args
        return GenerateTextOutput(result=StoryboardBoard(shots=self.shots))

    @activity.defn(name=GENERATE_IMAGE_CAPABILITY)
    async def generate_image(
        self,
        args: GenerateImageInput,
    ) -> GenerateImageOutput:
        self.image_inputs.append(args)
        if self.fail_all_images:
            raise ApplicationError(
                "image rejected",
                type="UpstreamRequestRejected",
                non_retryable=True,
            )
        return GenerateImageOutput(
            artifacts=[
                Artifact(
                    type="image",
                    uri=f"https://image/{args.prompt}",
                    model=args.model,
                )
            ],
        )

    @activity.defn(name=GENERATE_VIDEO_CAPABILITY)
    async def generate_video(
        self,
        args: GenerateVideoInput,
    ) -> GenerateVideoOutput:
        self.video_inputs.append(args)
        if args.prompt in self.failed_motions:
            raise ApplicationError(
                "video rejected",
                type="UpstreamRequestRejected",
                non_retryable=True,
            )
        return GenerateVideoOutput(
            artifacts=[
                Artifact(
                    type="video",
                    uri=f"https://video/{args.prompt}",
                    model=args.model,
                )
            ],
        )

    @activity.defn(name=COMPOSE_VIDEO_CAPABILITY)
    async def concat_videos(
        self,
        args: ConcatVideosInput,
    ) -> ConcatVideosOutput:
        self.compose_input = args
        return ConcatVideosOutput(
            artifacts=[
                Artifact(
                    type="video",
                    uri="https://video/film.mp4",
                )
            ],
            clipCount=len(args.videoUrls),
        )

    @activity.defn(name=RESOLVE_ACTIVITY_TYPE)
    async def resolve_capability(
        self,
        request: dict[str, str],
    ) -> dict[str, object]:
        capability_id = request["capabilityId"]
        if capability_id not in {
            GENERATE_TEXT_CAPABILITY,
            GENERATE_IMAGE_CAPABILITY,
            GENERATE_VIDEO_CAPABILITY,
            COMPOSE_VIDEO_CAPABILITY,
        }:
            raise ApplicationError(
                f"unknown capability {capability_id!r}",
                type="NotFound",
                non_retryable=True,
            )
        return {
            "kind": "activity",
            "temporalType": capability_id,
            "taskQueue": CORE_TASK_QUEUE,
            "timeoutSeconds": 24 * 60 * 60,
        }

    def activities(self) -> list[Callable]:
        return [
            self.resolve_capability,
            self.generate_text,
            self.generate_image,
            self.generate_video,
            self.concat_videos,
        ]


@pytest.fixture
async def temporal_env() -> AsyncIterator[WorkflowEnvironment]:
    async with await WorkflowEnvironment.start_time_skipping(
        data_converter=pydantic_data_converter
    ) as environment:
        yield environment


async def execute(
    temporal_env: WorkflowEnvironment,
    capabilities: FakeCoreCapabilities,
    args: StoryboardInput,
) -> StoryboardOutput:
    workflow_worker = Worker(
        temporal_env.client,
        task_queue=TEST_WORKFLOW_QUEUE,
        workflows=[StoryboardFilmExample],
    )
    activity_worker = Worker(
        temporal_env.client,
        task_queue=CORE_TASK_QUEUE,
        activities=capabilities.activities(),
    )
    async with workflow_worker, activity_worker:
        return await temporal_env.client.execute_workflow(
            StoryboardFilmExample.run,
            args,
            id=f"storyboard-example-{uuid4()}",
            task_queue=TEST_WORKFLOW_QUEUE,
        )


def valid_input(shots: int | None) -> StoryboardInput:
    if shots is None:
        return StoryboardInput(
            topic="雨夜的猫",
            chatModel="chat-cap",
            imageModel="image-cap",
            videoModel="video-cap",
        )
    return StoryboardInput(
        topic="雨夜的猫",
        chatModel="chat-cap",
        imageModel="image-cap",
        videoModel="video-cap",
        shots=shots,
    )


@pytest.mark.asyncio
async def test_workflow_uses_public_abi_and_default_shots(
    temporal_env: WorkflowEnvironment,
) -> None:
    capabilities = FakeCoreCapabilities(
        [StoryboardShot(frame="猫在雨夜", motion="猫向前走")]
    )

    result = await execute(temporal_env, capabilities, valid_input(shots=None))

    assert result == StoryboardOutput(
        artifacts=[
            Artifact(
                type="video",
                uri="https://video/film.mp4",
                description="围绕「雨夜的猫」生成的分镜短片，共 1 个片段。",
                model="video-cap",
            )
        ],
        clipCount=1,
    )
    assert capabilities.text_input is not None
    assert capabilities.text_input.model == "chat-cap"
    assert "3 个连贯镜头" in capabilities.text_input.prompt
    assert capabilities.text_input.outputSchema == (
        StoryboardBoard.model_json_schema()
    )
    assert capabilities.image_inputs == [
        GenerateImageInput(
            model="image-cap",
            prompt="猫在雨夜",
            aspectRatio="16:9",
        )
    ]
    assert capabilities.video_inputs == [
        GenerateVideoInput(
            model="video-cap",
            firstFrame="https://image/猫在雨夜",
            prompt="猫向前走",
        )
    ]
    assert capabilities.compose_input == ConcatVideosInput(
        videoUrls=["https://video/猫向前走"]
    )


@pytest.mark.asyncio
async def test_workflow_caps_model_shots_to_requested_count(
    temporal_env: WorkflowEnvironment,
) -> None:
    capabilities = FakeCoreCapabilities(
        [
            StoryboardShot(frame="frame-0", motion="motion-0"),
            StoryboardShot(frame="frame-1", motion="motion-1"),
        ]
    )

    result = await execute(temporal_env, capabilities, valid_input(shots=1))

    assert result.clipCount == 1
    assert capabilities.compose_input == ConcatVideosInput(
        videoUrls=["https://video/motion-0"]
    )


@pytest.mark.asyncio
async def test_workflow_keeps_successful_clips_in_source_order(
    temporal_env: WorkflowEnvironment,
) -> None:
    capabilities = FakeCoreCapabilities(
        [
            StoryboardShot(frame="frame-0", motion="motion-0"),
            StoryboardShot(frame="frame-1", motion="motion-1"),
            StoryboardShot(frame="frame-2", motion="motion-2"),
        ],
        failed_motions={"motion-1"},
    )

    result = await execute(temporal_env, capabilities, valid_input(shots=3))

    assert result.clipCount == 2
    assert capabilities.compose_input == ConcatVideosInput(
        videoUrls=[
            "https://video/motion-0",
            "https://video/motion-2",
        ],
    )


@pytest.mark.asyncio
async def test_workflow_fails_without_composition_when_all_clips_fail(
    temporal_env: WorkflowEnvironment,
) -> None:
    capabilities = FakeCoreCapabilities(
        [StoryboardShot(frame="frame", motion="motion")],
        fail_all_images=True,
    )

    with pytest.raises(WorkflowFailureError) as raised:
        await execute(temporal_env, capabilities, valid_input(shots=1))

    cause = raised.value.__cause__
    assert isinstance(cause, ApplicationError)
    assert cause.type == "StoryboardAllClipsFailed"
    assert "all clips failed" in str(cause)
    assert capabilities.compose_input is None


@pytest.mark.parametrize(
    "changes",
    [
        {"topic": ""},
        {"topic": "   "},
        {"shots": -1},
        {"shots": 0},
        {"shots": 9},
        {"shots": True},
        {"shots": "3"},
        {"unexpected": "value"},
    ],
)
def test_storyboard_input_rejects_invalid_values(
    changes: dict[str, object],
) -> None:
    raw = {
        "topic": "雨夜的猫",
        "chatModel": "chat-cap",
        "imageModel": "image-cap",
        "videoModel": "video-cap",
    }
    raw.update(changes)

    with pytest.raises(ValidationError):
        StoryboardInput.model_validate(raw)


def test_storyboard_input_applies_defaults_and_trims_strings() -> None:
    args = StoryboardInput(
        topic="  雨夜的猫  ",
        chatModel="  chat-cap  ",
        imageModel="  image-cap  ",
        videoModel="  video-cap  ",
    )

    assert args.model_dump() == {
        "topic": "雨夜的猫",
        "chatModel": "chat-cap",
        "imageModel": "image-cap",
        "videoModel": "video-cap",
        "shots": 3,
    }


def test_generated_schemas_and_payload_contract() -> None:
    input_schema = StoryboardInput.model_json_schema()
    assert input_schema["additionalProperties"] is False
    assert input_schema["required"] == [
        "topic",
        "chatModel",
        "imageModel",
        "videoModel",
    ]
    assert input_schema["properties"]["shots"]["default"] == 3
    assert input_schema["properties"]["shots"]["minimum"] == 1
    assert input_schema["properties"]["shots"]["maximum"] == 8

    output_schema = StoryboardOutput.model_json_schema()
    assert output_schema["additionalProperties"] is False
    assert output_schema["required"] == ["artifacts", "clipCount"]

    board_schema = StoryboardBoard.model_json_schema()
    assert board_schema["additionalProperties"] is False
    assert board_schema["required"] == ["shots"]
    payloads = [
        GenerateTextInput(
            model="chat",
            prompt="prompt",
            outputSchema=board_schema,
        ),
        GenerateImageInput(
            model="image",
            prompt="prompt",
            aspectRatio="16:9",
        ),
        GenerateVideoInput(
            model="video",
            firstFrame="https://image",
            prompt="motion",
        ),
        ConcatVideosInput(videoUrls=["https://video"]),
    ]
    forbidden = {
        "session",
        "sessionId",
        "tenant",
        "tenantId",
        "execPublicId",
    }
    for payload in payloads:
        assert forbidden.isdisjoint(payload.model_dump())
