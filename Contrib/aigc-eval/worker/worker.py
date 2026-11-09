"""Worker 入口：连 Temporal、注册 workflow 与 activity、announce capability。

生产环境走 ``connect_temporal()``：由 KESS 发现 ``klingai-muse-temporal-server``
并落在 ``agentk`` namespace。**KESS 依赖运行环境的 kess-agent，因此 worker 只能
在公司容器或物理机上启动，没有 kess-agent 的开发机起不来**——这不是配置问题，
换环境变量也绕不过去。

``TEMPORAL_ADDRESS`` / ``TEMPORAL_NAMESPACE`` 是显式覆盖，用来指向自己起的
Temporal（比如 ``temporal server start-dev``）做本机联调。namespace 默认
``default`` 是给这种本机实例的；**若把地址指向共享集群，必须同时设
``TEMPORAL_NAMESPACE=agentk``**，否则 worker 会连上一个空 namespace，
表现为"启动正常、日志干净、但永远收不到任务"。基线实例（有 ``KWS_SERVICE_STAGE``、
无 ``KWS_LANE_ID``）会忽略这个覆盖，理由见 ``_connect``。

不要手工设置 AGENTK_BASE_URL / KWS_SERVICE_CREDENTIAL——平台身份由 KESS 注入，
手工覆盖会让 RPC 返回未认证。
"""

from __future__ import annotations

import asyncio
import logging
import os
import signal

from abi import (
    CAPABILITY_DESCRIPTION,
    CAPABILITY_ID,
    PROGRESS_SIGNAL,
    WORKER_TASK_QUEUE,
    WORKFLOW_TYPE,
    VideoJudgeInput,
    VideoJudgeOutput,
)
from activities import VideoJudgeActivities
from agentk.worker import Capability, Tool, Worker, connect_temporal
from stage_activities import StageActivities
from temporalio.client import Client
from temporalio.contrib.pydantic import pydantic_data_converter
from workflow import INSPECT_TIMEOUT, AigcEvalVideoJudge

logger = logging.getLogger(__name__)


def _is_baseline() -> bool:
    """是否为接管正式流量的基线实例。

    判据来自 Worker SDK 的泳道规则：有 ``KWS_SERVICE_STAGE``、无 ``KWS_LANE_ID``
    即基线（见 deploy/run_worker.sh 里那段变量冲突说明）。本机联调两个都会设，
    所以只看 stage 会把联调误判成生产。
    """
    return bool(os.getenv("KWS_SERVICE_STAGE", "").strip()) and not os.getenv(
        "KWS_LANE_ID", ""
    ).strip()


def _check_media_upload() -> None:
    """基线实例缺对象存储配置就拒绝启动。

    质检的每一步都要先把视频和抽出的帧上传，再把 URL 交给 understand_media——
    没有 ``AIGC_EVAL_BLOB_ENDPOINT`` / ``AIGC_EVAL_BLOB_BUCKET``，capability 能
    announce、能接单，然后**每一次调用都在上传那步失败**。这两个变量不像 KWS_* 那样
    由平台注入，得在部署配置里显式给，正是最容易漏的一项。

    宁可起不来：容器反复重启一眼就能看见，而"能力在目录里、每次调用都失败"要等
    调用方来报障。联调实例只警告——那里本来就常缺配置，不该被拦着起不来。
    """
    from aigc_eval.providers.blob_store import load_blob_config

    if load_blob_config().configured:
        return
    detail = (
        "未配置素材上传：AIGC_EVAL_BLOB_ENDPOINT / AIGC_EVAL_BLOB_BUCKET 缺失，"
        "understand_media 只接受 HTTP URL，缺它每次质检都会在上传阶段失败"
    )
    if _is_baseline():
        logger.error("%s；基线实例拒绝启动", detail)
        raise SystemExit(2)
    logger.warning("%s；当前非基线实例，仅告警", detail)


async def _connect() -> Client:
    """显式地址用于本机联调，生产留给 KESS 发现。

    **基线实例忽略这个覆盖**（判据见 ``_is_baseline``），而不是照它连过去。

    值得为此写几行的原因：残留的 ``TEMPORAL_ADDRESS``（镜像里的旧 ENV、复制来的启动
    配置）会让 worker 静默连到一个空 namespace——启动正常、日志干净、能力也 announce
    了，但永远收不到任务。从外部看是"能力在目录里、调过去没反应"，排查代价极高。
    """
    address = os.getenv("TEMPORAL_ADDRESS", "").strip()
    if address and _is_baseline():
        logger.warning(
            "基线实例（有 KWS_SERVICE_STAGE、无 KWS_LANE_ID）忽略 TEMPORAL_ADDRESS=%s，"
            "改走 KESS 发现",
            address,
        )
        address = ""
    if address:
        namespace = os.getenv("TEMPORAL_NAMESPACE", "default").strip() or "default"
        logger.info("连接指定的 Temporal %s/%s", address, namespace)
        return await Client.connect(
            address,
            namespace=namespace,
            data_converter=pydantic_data_converter,
        )
    logger.info("经 KESS 发现 Temporal（需运行环境有 kess-agent）")
    return await connect_temporal(data_converter=pydantic_data_converter)


async def main() -> None:
    logging.basicConfig(
        level=os.getenv("LOG_LEVEL", "INFO").upper(),
        format="%(asctime)s %(levelname)s %(name)s %(message)s",
    )
    _check_media_upload()
    client = await _connect()
    # 两套 activity 同时注册：
    #
    # * 阶段 activity 是当前 workflow 用的（八段，可在阶段边界续跑）；
    # * 单体 inspect 仍留着，因为 activity 名是 workflow 历史的一部分——升级时还有在途
    #   的旧 run 会来要 ``inspect.v1``，摘掉它那些 run 会卡死在"没有 worker 能处理"。
    #   等在途 run 跑完再摘。
    stage_activities = StageActivities(client, PROGRESS_SIGNAL)
    legacy = VideoJudgeActivities(client)
    worker = Worker(
        client,
        task_queue=WORKER_TASK_QUEUE,
        workflows=[AigcEvalVideoJudge],
        activities=[
            stage_activities.prepare,
            stage_activities.compile_prompt,
            stage_activities.observe,
            stage_activities.scan,
            stage_activities.recheck,
            stage_activities.aggregate,
            stage_activities.evidence,
            stage_activities.report,
            legacy.inspect,
        ],
    )
    worker.publish_workflow(
        Capability(
            id=CAPABILITY_ID,
            temporal_type=WORKFLOW_TYPE,
            description=CAPABILITY_DESCRIPTION,
            input_schema=VideoJudgeInput.model_json_schema(),
            output_schema=VideoJudgeOutput.model_json_schema(),
            timeout=INSPECT_TIMEOUT,
            # 不重试：一次质检要花掉多次模型调用，失败静默重试会把成本翻倍
            # 而调用方看不到。让它一次失败、把原因交回去。
            tool=Tool(max_attempts=1),
        )
    )
    logger.info(
        "worker 就绪：queue=%s workflow=%s capability=%s",
        WORKER_TASK_QUEUE,
        WORKFLOW_TYPE,
        CAPABILITY_ID,
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
        logger.info("收到停止信号，开始优雅退场")
        await worker.shutdown()
        await worker_task
    finally:
        stop_task.cancel()
        await asyncio.gather(stop_task, return_exceptions=True)
        for sig in (signal.SIGINT, signal.SIGTERM):
            loop.remove_signal_handler(sig)


if __name__ == "__main__":
    asyncio.run(main())
