import asyncio
import os
import signal
from datetime import timedelta

from agentk.worker import (
    Capability,
    Pipeline,
    Tool,
    ToolMode,
    Worker,
)
from temporalio.client import Client
from temporalio.contrib.pydantic import pydantic_data_converter

from abi import (
    CAPABILITY_ID,
    PIPELINE_NAME,
    TOOL_NAME,
    WORKER_TASK_QUEUE,
    WORKFLOW_TYPE,
    FfmpegToolInput,
    FfmpegToolOutput,
)
from activities import FfmpegActivities
from kess_ffmpeg import KessFfmpegClient, KessFfmpegConfig
from workflow import FfmpegTool

CAPABILITY_DESCRIPTION = (
    "通用 ffmpeg 音视频处理工具。对已有媒体（https URL / blobstore key）"
    "执行白名单操作：抽帧、静图成片、切片、变速、拼接、音轨处理、混音、"
    "裁剪、缩放、贴图、花字、尾卡、烧字幕，以及 probe / 场景检测等分析，"
    "共 29 个 op；支持多步 pipeline（中间产物不经 blobstore）。"
    "媒体产物以可下载 URL 的 artifact 返回，分析结果以 JSON 返回。"
    "操作通常秒级完成。"
)


async def main() -> None:
    address = os.getenv("TEMPORAL_ADDRESS", "localhost:7233")
    namespace = os.getenv("TEMPORAL_NAMESPACE", "default")
    kess_client = KessFfmpegClient(KessFfmpegConfig.from_env())
    kess_client.warm_up()  # 必须在主线程（infra-kess 注册信号处理器）

    client = await Client.connect(
        address,
        namespace=namespace,
        data_converter=pydantic_data_converter,
    )
    activities = FfmpegActivities(kess_client)
    worker = Worker(
        client,
        task_queue=WORKER_TASK_QUEUE,
        workflows=[FfmpegTool],
        activities=[activities.infer],
    )
    worker.publish_workflow(
        Capability(
            id=CAPABILITY_ID,
            temporal_type=WORKFLOW_TYPE,
            description=CAPABILITY_DESCRIPTION,
            input_schema=FfmpegToolInput.model_json_schema(),
            output_schema=FfmpegToolOutput.model_json_schema(),
            timeout=timedelta(hours=1),
            tool=Tool(
                name=TOOL_NAME,
                mode=ToolMode.BLOCKING,
            ),
            pipeline=Pipeline(
                name=PIPELINE_NAME,
                description=CAPABILITY_DESCRIPTION,
            ),
        )
    )
    print(
        f'worker started: task queue "{WORKER_TASK_QUEUE}", '
        f'workflow "{WORKFLOW_TYPE}"',
        flush=True,
    )

    loop = asyncio.get_running_loop()
    stop_event = asyncio.Event()
    for sig in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(sig, stop_event.set)

    worker_task = asyncio.create_task(worker.run())
    stop_task = asyncio.create_task(stop_event.wait())
    try:
        done, _ = await asyncio.wait(
            (worker_task, stop_task),
            return_when=asyncio.FIRST_COMPLETED,
        )
        if worker_task in done:
            await worker_task
            return
        await worker.shutdown()
        await worker_task
    finally:
        stop_task.cancel()
        await asyncio.gather(stop_task, return_exceptions=True)
        for sig in (signal.SIGINT, signal.SIGTERM):
            loop.remove_signal_handler(sig)


if __name__ == "__main__":
    asyncio.run(main())
