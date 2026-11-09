import asyncio

from abi import (
    COMPOSE_VIDEO_CAPABILITY,
    GENERATE_IMAGE_CAPABILITY,
    GENERATE_TEXT_CAPABILITY,
    GENERATE_VIDEO_CAPABILITY,
    WORKFLOW_TYPE,
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
from agentk.worker import Artifact, invoke_capability, report_progress
from temporalio import workflow
from temporalio.exceptions import ActivityError, ApplicationError


@workflow.defn(name=WORKFLOW_TYPE)
class StoryboardFilmExample:
    @workflow.run
    async def run(self, args: StoryboardInput) -> StoryboardOutput:
        shots = await generate_board(args)
        await report_agentk_progress(
            "board",
            f"分镜完成，共 {len(shots)} 镜",
        )

        results = await asyncio.gather(
            *(
                generate_clip(args, index, shot)
                for index, shot in enumerate(shots)
            ),
            return_exceptions=True,
        )
        successful_urls = [
            result
            for result in results
            if isinstance(result, str) and result
        ]
        if not successful_urls:
            first_error = next(
                (
                    result
                    for result in results
                    if isinstance(result, BaseException)
                ),
                None,
            )
            message = "storyboard: all clips failed"
            if first_error is not None:
                message = f"{message}: {first_error}"
            raise ApplicationError(
                message,
                type="StoryboardAllClipsFailed",
                non_retryable=True,
            )

        composed = await invoke_capability(
            COMPOSE_VIDEO_CAPABILITY,
            ConcatVideosInput(videoUrls=successful_urls),
            result_type=ConcatVideosOutput,
        )
        if not composed.artifacts or not composed.artifacts[0].uri:
            raise ApplicationError(
                "storyboard: concat_videos returned no artifacts",
                type="InvalidCapabilityOutput",
                non_retryable=True,
            )
        composed_artifact = composed.artifacts[0]
        return StoryboardOutput(
            artifacts=[
                Artifact(
                    type="video",
                    uri=composed_artifact.uri,
                    description=(
                        f"围绕「{args.topic}」生成的分镜短片，"
                        f"共 {composed.clipCount} 个片段。"
                    ),
                    model=args.videoModel,
                    params=composed_artifact.params,
                ),
            ],
            clipCount=composed.clipCount,
        )


async def generate_board(args: StoryboardInput) -> list[StoryboardShot]:
    output = await invoke_capability(
        GENERATE_TEXT_CAPABILITY,
        GenerateTextInput(
            model=args.chatModel,
            prompt=(
                f"围绕「{args.topic}」设计 {args.shots} 个连贯镜头。\n"
                "每个镜头包含 frame（静态首帧描述）和 motion（运动描述）。"
            ),
            outputSchema=StoryboardBoard.model_json_schema(),
        ),
        result_type=GenerateTextOutput,
    )

    if not output.result.shots:
        raise ApplicationError(
            "storyboard: board returned no shots",
            type="InvalidBoard",
            non_retryable=True,
        )
    return output.result.shots[: args.shots]


async def generate_clip(
    args: StoryboardInput,
    index: int,
    shot: StoryboardShot,
) -> str:
    frame = await invoke_capability(
        GENERATE_IMAGE_CAPABILITY,
        GenerateImageInput(
            model=args.imageModel,
            prompt=shot.frame,
            aspectRatio="16:9",
        ),
        result_type=GenerateImageOutput,
    )
    if not frame.artifacts or not frame.artifacts[0].uri:
        raise ApplicationError(
            f"storyboard: shot {index}: generate_image returned no artifacts",
            type="InvalidCapabilityOutput",
            non_retryable=True,
        )
    clip = await invoke_capability(
        GENERATE_VIDEO_CAPABILITY,
        GenerateVideoInput(
            model=args.videoModel,
            firstFrame=frame.artifacts[0].uri,
            prompt=shot.motion,
        ),
        result_type=GenerateVideoOutput,
    )
    if not clip.artifacts or not clip.artifacts[0].uri:
        raise ApplicationError(
            f"storyboard: shot {index}: generate_video returned no artifacts",
            type="InvalidCapabilityOutput",
            non_retryable=True,
        )
    await report_agentk_progress(f"clip/{index}", "片段完成")
    return clip.artifacts[0].uri


async def report_agentk_progress(step_id: str, message: str) -> None:
    try:
        await report_progress(step_id, message)
    except ActivityError as error:
        workflow.logger.warning(
            "storyboard example: report progress failed: %s",
            error,
        )
