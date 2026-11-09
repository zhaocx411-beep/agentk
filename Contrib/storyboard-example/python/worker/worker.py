import asyncio
import signal
from datetime import timedelta

from abi import (
    CAPABILITY_ID,
    WORKER_TASK_QUEUE,
    WORKFLOW_TYPE,
    StoryboardInput,
    StoryboardOutput,
)
from agentk.worker import (
    Capability,
    Tool,
    Worker,
    connect_temporal,
)
from temporalio.contrib.pydantic import pydantic_data_converter
from workflow import StoryboardFilmExample

PIPELINE_DESCRIPTION = "围绕主题生成分镜，并行生成各镜头视频片段，最后拼接成片。"


async def main() -> None:
    client = await connect_temporal(
        data_converter=pydantic_data_converter,
    )
    worker = Worker(
        client,
        task_queue=WORKER_TASK_QUEUE,
        workflows=[StoryboardFilmExample],
    )
    worker.publish_workflow(
        Capability(
            id=CAPABILITY_ID,
            temporal_type=WORKFLOW_TYPE,
            description=PIPELINE_DESCRIPTION,
            input_schema=StoryboardInput.model_json_schema(),
            output_schema=StoryboardOutput.model_json_schema(),
            timeout=timedelta(hours=24),
            tool=Tool(),
        )
    )
    print(
        f'worker started: task queue "{WORKER_TASK_QUEUE}", workflow "{WORKFLOW_TYPE}"',
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
