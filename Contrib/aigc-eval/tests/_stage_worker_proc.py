"""被杀实验用的 worker 子进程。

由 ``tests/test_worker_kill_resume.py`` 拉起。它跑**真实的** workflow 编排，只把八个
阶段换成假实现——因为要验的是"实例被 SIGKILL 之后从哪里接着跑"，与判定内容无关。

每次阶段执行都往一个 JSONL 里追加一行（阶段名、第几次尝试、进程号），父进程据此判断
哪些阶段在哪个进程里跑过。用文件而不是管道，是因为进程要被 SIGKILL，管道里的缓冲会
一起丢。

用法：``python tests/_stage_worker_proc.py <task_queue> <log.jsonl> <slow_stage>``
"""

from __future__ import annotations

import asyncio
import json
import os
import sys
from datetime import timedelta
from pathlib import Path
from typing import Any

REPO = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPO / "src"))
sys.path.insert(0, str(REPO / "worker"))

os.environ.setdefault("KWS_LANE_ID", "dev/kill-resume-test")

import workflow as workflow_module  # noqa: E402
from abi import (  # noqa: E402
    STAGE_ACTIVITIES,
    ModelStep,
    ModelStepInput,
    StageInput,
    StageResult,
    VideoJudgeInput,
    VideoJudgeOutput,
)
from temporalio import activity, workflow  # noqa: E402
from temporalio.client import Client  # noqa: E402
from temporalio.contrib.pydantic import pydantic_data_converter  # noqa: E402
from temporalio.worker import UnsandboxedWorkflowRunner, Worker  # noqa: E402
from workflow import AigcEvalVideoJudge  # noqa: E402

from aigc_eval.providers.model_call import (
    UNDERSTAND_MEDIA_CAPABILITY,
    ModelRequest,
    ModelResponse,
)

LOG: Path
SLOW_STAGE: str

# 替掉 invoke_capability 的桩 activity 名。
FAKE_CAPABILITY_ACTIVITY = "test_fake_capability"


def record(stage: str, attempt: int, note: str = "") -> None:
    line = json.dumps(
        {"stage": stage, "attempt": attempt, "pid": os.getpid(), "note": note},
        ensure_ascii=False,
    )
    with LOG.open("a", encoding="utf-8") as handle:
        handle.write(line + "\n")
        handle.flush()
        os.fsync(handle.fileno())


async def _maybe_stall(stage: str) -> None:
    """被指定为"慢阶段"的那一段挂住并持续心跳，好让父进程有机会杀掉本进程。

    必须心跳：不心跳的话 Temporal 会按失联处理，就分不清"进程被杀"与"忘了心跳"。
    """
    if stage != SLOW_STAGE:
        return
    record(stage, activity.info().attempt, "stalling")
    for _ in range(600):
        activity.heartbeat()
        await asyncio.sleep(0.2)


@activity.defn(name=STAGE_ACTIVITIES["prepare"])
async def prepare(args: VideoJudgeInput) -> StageResult:
    record("prepare", activity.info().attempt)
    await _maybe_stall("prepare")
    return StageResult(
        runKey=activity.info().workflow_run_id,
        stage="prepare",
        summary={"cached": False},
    )


def _stage(name: str) -> Any:
    async def run(args: StageInput) -> StageResult:
        record(name, activity.info().attempt)
        await _maybe_stall(name)
        return StageResult(runKey=args.runKey, stage=name)

    run.__name__ = f"stage_{name}"
    return activity.defn(name=STAGE_ACTIVITIES[name])(run)


def _model_stage(name: str) -> Any:
    """含模型调用的阶段：可重入，每段发一次模型调用。

    每段真的发一次调用，是因为这条测试要验的核心不只是"从哪一段接着跑"，还有**已经
    付过钱的模型调用不会因为重跑而再付一次**。让阶段不发调用会把后者完全掩盖掉。

    停顿刻意放在**拿到响应之后**那一趟（``args.responses`` 里已经有本段的答案）。放在
    调用之前的话，被杀时这段还没花过钱，重跑自然不会重复，测不到任何东西。
    """
    digest = f"{name}-1"

    async def run(args: ModelStepInput) -> ModelStep:
        record(name, activity.info().attempt)
        if digest not in args.responses:
            # 第一趟：把请求交出去，等 workflow 调完再重放这一段。
            # request 在契约里是 dict（过 Temporal payload），与真实实现一致。
            return ModelStep(
                request=ModelRequest(
                    capabilityId=UNDERSTAND_MEDIA_CAPABILITY,
                    payload={"stage": name},
                    digest=digest,
                    stage=name,
                    title=name,
                ).model_dump(mode="json")
            )
        # 第二趟起：响应已经在手上，这里被杀就是要验的那个场景。
        await _maybe_stall(name)
        return ModelStep(result=StageResult(runKey=args.runKey, stage=name))

    run.__name__ = f"stage_{name}"
    return activity.defn(name=STAGE_ACTIVITIES[name])(run)


@activity.defn(name=FAKE_CAPABILITY_ACTIVITY)
async def fake_capability(request: ModelRequest) -> ModelResponse:
    """替掉 invoke_capability 的桩。

    **必须是 activity，不能是普通 async 函数。** workflow 代码在重放时会被重新执行一遍，
    普通函数会跟着被再调一次，于是"调用了几次"的计数里混进重放的假象——这正是本测试
    要排除的东西。activity 的结果落在 workflow 历史里，重放时从历史取，所以下面这行
    record 每次**真实**调用只写一行。
    """
    record("__model__", activity.info().attempt, request.stage)
    return ModelResponse(rawText="{}")


async def _invoke_model_stub(
    self: Any, stage: str, request: ModelRequest
) -> ModelResponse:
    """把 ``_invoke_model`` 换成上面那个 activity，其余驱动逻辑（``_model_stage``
    的循环、轮数上限、响应归集）全部保持真实——被验证的正是那段逻辑。
    """
    return await workflow.execute_activity(
        FAKE_CAPABILITY_ACTIVITY,
        request,
        start_to_close_timeout=timedelta(seconds=30),
        result_type=ModelResponse,
    )


@activity.defn(name=STAGE_ACTIVITIES["report"])
async def report(args: StageInput) -> VideoJudgeOutput:
    record("report", activity.info().attempt)
    await _maybe_stall("report")
    return VideoJudgeOutput(
        analysisStatus="ok",
        mode="T2V",
        dimensions=[],
        issues=[],
        violatedSubcriteria=0,
        unresolvedSubcriteria=0,
        applicableDimensions=8,
        resolvedDimensions=8,
        unverifiedDimensions=0,
        reviewRequiredCount=0,
    )


async def _noop_progress(step_id: str, message: str, data: Any = None) -> None:
    """替掉 report_progress：测试里没有核心侧那个 activity，不替会挂在重试上。"""


async def main() -> None:
    global LOG, SLOW_STAGE
    queue, log_path, SLOW_STAGE = sys.argv[1], sys.argv[2], sys.argv[3]
    LOG = Path(log_path)

    # 心跳超时压到 3 秒：真值是 5 分钟（要容纳一次超长模型调用），测试等不起。
    # 缩短的只是"多久判定失联"，被验证的行为——重新派发时前面的阶段不重跑——不变。
    workflow_module._HEARTBEAT_TIMEOUT = timedelta(seconds=3)
    workflow_module.report_progress = _noop_progress  # type: ignore[assignment]
    # 本机没有平台能力，把发起模型调用那一步换成桩 activity。换的是"怎么调"，
    # 不是"调几次"——次数由真实的 _model_stage 驱动循环决定，那才是被验证的对象。
    AigcEvalVideoJudge._invoke_model = _invoke_model_stub  # type: ignore[method-assign]

    client = await Client.connect(
        os.getenv("TEMPORAL_ADDRESS", "127.0.0.1:7233"),
        data_converter=pydantic_data_converter,
    )
    async with Worker(
        client,
        task_queue=queue,
        workflows=[AigcEvalVideoJudge],
        activities=[
            prepare,
            _model_stage("compile"),
            _model_stage("observe"),
            _model_stage("scan"),
            _model_stage("recheck"),
            _stage("aggregate"),
            _model_stage("evidence"),
            report,
            fake_capability,
        ],
        workflow_runner=UnsandboxedWorkflowRunner(),
    ):
        record("__worker__", 0, "ready")
        await asyncio.Event().wait()


if __name__ == "__main__":
    asyncio.run(main())
