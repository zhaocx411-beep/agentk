from datetime import timedelta

from agentk.worker import Artifact
from temporalio import workflow
from temporalio.common import RetryPolicy
from temporalio.exceptions import ApplicationError

from abi import (
    INFER_ACTIVITY,
    WORKFLOW_TYPE,
    FfmpegToolInput,
    FfmpegToolOutput,
    InferActivityOutput,
)

NON_RETRYABLE_ACTIVITY_ERRORS = [
    "InvalidArgument",
    "FfmpegInferFailed",
    "NotFound",
    "PermissionDenied",
]


@workflow.defn(name=WORKFLOW_TYPE)
class FfmpegTool:
    @workflow.run
    async def run(self, args: FfmpegToolInput) -> FfmpegToolOutput:
        # activity 与 workflow 同队列（本 worker 注册），不填 task_queue
        output = await workflow.execute_activity(
            INFER_ACTIVITY,
            args,
            start_to_close_timeout=timedelta(minutes=30),
            schedule_to_close_timeout=timedelta(hours=1),
            heartbeat_timeout=timedelta(minutes=2),
            retry_policy=RetryPolicy(
                initial_interval=timedelta(seconds=1),
                backoff_coefficient=2,
                maximum_interval=timedelta(seconds=30),
                maximum_attempts=3,
                non_retryable_error_types=NON_RETRYABLE_ACTIVITY_ERRORS,
            ),
            result_type=InferActivityOutput,
        )
        if output.kind is None and output.result is None:
            raise ApplicationError(
                "ffmpeg_tool: 服务既未返回媒体产物也未返回分析结果",
                type="InvalidCapabilityOutput",
                non_retryable=True,
            )

        artifacts: list[Artifact] = []
        if output.kind is not None and output.uri:
            artifacts.append(
                Artifact(
                    type=output.kind,
                    uri=output.uri,
                    description=f"ffmpeg {args.op_summary()} 产物",
                    params={
                        "blobKey": output.blobKey,
                        "ssoUri": output.ssoUri,
                        "outputSize": output.outputSize,
                    },
                )
            )
        return FfmpegToolOutput(artifacts=artifacts, result=output.result)
