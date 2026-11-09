import asyncio

from temporalio import activity

from abi import INFER_ACTIVITY, FfmpegToolInput, InferActivityOutput
from kess_ffmpeg import KessFfmpegClient

HEARTBEAT_INTERVAL_SECONDS = 15


class FfmpegActivities:
    def __init__(self, client: KessFfmpegClient) -> None:
        self._client = client

    @activity.defn(name=INFER_ACTIVITY)
    async def infer(self, args: FfmpegToolInput) -> InferActivityOutput:
        """在线程里跑阻塞的 kess 调用，期间定期心跳。"""
        task = asyncio.create_task(asyncio.to_thread(self._client.infer, args))
        while True:
            done, _ = await asyncio.wait(
                {task}, timeout=HEARTBEAT_INTERVAL_SECONDS
            )
            if done:
                return task.result()
            activity.heartbeat()
