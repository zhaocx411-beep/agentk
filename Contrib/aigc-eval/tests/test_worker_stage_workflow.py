"""在真 Temporal 上验证阶段编排：顺序、缓存短路、进度中继、按阶段续跑。

阶段正文用假实现（毫秒级返回），所以这里测的是**编排**，不是判定——判定的等价性由
``tests/test_worker_stages.py`` 用录制响应逐字段对完。

最关键的一条是 [4]：某个阶段重跑时，它**前面的阶段不重跑**。这正是把质检从一个
activity 拆成八个所换来的东西，也是唯一无法靠读代码确认的性质——它取决于 Temporal
把已完成 activity 的结果记进历史、重放时直接取用。

关于"怎么模拟崩溃"：真实的进程丢失要等心跳超时（5 分钟）才会被 Temporal 判定，
测试里等不起。所以让某个阶段的第一次尝试抛一个**可重试**异常——从 Temporal 的角度，
进程丢失与可重试失败进的是同一条路径：为该 activity 起新的一次尝试，历史里已完成的
其他 activity 不受影响。真机上杀实例的验证在泳道实例上单独做。

前置：本机有 Temporal（``temporal server start-dev``），或设 ``TEMPORAL_ADDRESS``。
运行：``uv run python tests/test_worker_stage_workflow.py``
"""

from __future__ import annotations

import asyncio
import os
import sys
import uuid
from collections import Counter
from pathlib import Path
from typing import Any

REPO = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPO / "src"))
sys.path.insert(0, str(REPO / "worker"))

# 泳道变量：让 worker 侧的基线判定把这里当联调，别去连真 KESS。
os.environ.setdefault("KWS_LANE_ID", "dev/stage-workflow-test")

import workflow as workflow_module  # noqa: E402
from abi import (  # noqa: E402
    PROGRESS_SIGNAL,
    STAGE_ACTIVITIES,
    ModelStep,
    ModelStepInput,
    StageInput,
    StageResult,
    VideoJudgeInput,
    VideoJudgeOutput,
)
from aigc_eval.providers.model_call import (  # noqa: E402
    UNDERSTAND_MEDIA_CAPABILITY,
    ModelRequest,
)
from temporalio import activity  # noqa: E402
from temporalio.client import Client  # noqa: E402
from temporalio.contrib.pydantic import pydantic_data_converter  # noqa: E402
from temporalio.exceptions import ApplicationError  # noqa: E402
from temporalio.worker import UnsandboxedWorkflowRunner, Worker  # noqa: E402
from workflow import AigcEvalVideoJudge  # noqa: E402

FAILURES: list[str] = []

# 各阶段被执行的次数。断言"前面的阶段没重跑"就看这张表。
CALLS: Counter[str] = Counter()
# 阶段实际拿到的 fromStage，用来确认状态是按链条一段接一段读的。
CHAIN: list[tuple[str, str]] = []
# 被 workflow 转发出去的进度。
FORWARDED: list[tuple[str, str]] = []
# workflow 经 invoke_capability 发起的模型调用。
INVOKED: list[tuple[str, dict]] = []

# 测试开关：让某个阶段的第一次尝试失败，模拟"跑到这一段时实例丢了"。
FAIL_ONCE: set[str] = set()
# prepare 是否报告"命中整份报告缓存"。
CACHED = False


def check(label: str, ok: bool, detail: object = "") -> None:
    print(f"{'PASS' if ok else 'FAIL'}  {label}" + (f" — {detail}" if not ok else ""))
    if not ok:
        FAILURES.append(label)


# 一次正常运行里每个阶段被执行的次数。含模型调用的阶段是"模型调用次数 + 1"：
# 每次调用之前跑一遍（跑到调用点为止），最后再跑一遍收尾。这是重放的代价，也是
# 判定代码能一行不改的原因。
#
# 下面几组"某段失败后重跑"的断言都以这张表为基准比增量，而不是直接比绝对次数——
# 比绝对次数的话，任何一处调用次数变化都会让四组断言一起红，看不出到底是重放变了
# 还是断点续跑坏了。
BASE_CALLS = {
    "prepare": 1,
    "compile": 2,
    "observe": 2,
    "scan": 2,
    "recheck": 4,  # 三次模型调用
    "aggregate": 1,
    "evidence": 2,
    "report": 1,
}


def _extra_calls() -> dict[str, int]:
    """相对正常运行多跑了几次。断点续跑的断言看这个。"""
    return {
        name: CALLS[name] - base
        for name, base in BASE_CALLS.items()
        if CALLS[name] != base
    }


def _stage_order() -> list[str]:
    """阶段首次出现的顺序。

    按首次出现取，而不是原样列出 CHAIN：含模型调用的阶段会被重入，原样列出里
    同一个名字会连着出现好几次，看的就不是"顺序"了。
    """
    order: list[str] = []
    for name, _ in CHAIN:
        if name not in order:
            order.append(name)
    return order


def reset() -> None:
    CALLS.clear()
    CHAIN.clear()
    FORWARDED.clear()
    INVOKED.clear()
    FAIL_ONCE.clear()


def _maybe_fail(stage: str) -> None:
    """第一次尝试抛可重试异常，此后正常。"""
    if stage in FAIL_ONCE and CALLS[stage] == 1:
        raise ApplicationError(f"模拟实例丢失于 {stage}", type="WorkerLost")


# ---------------------------------------------------------------------------
# 假阶段实现。名字必须与 abi.STAGE_ACTIVITIES 完全一致——activity 名是
# workflow 历史的一部分，名字对不上就是"没有 worker 能处理这一步"。
# ---------------------------------------------------------------------------


@activity.defn(name=STAGE_ACTIVITIES["prepare"])
async def fake_prepare(args: VideoJudgeInput) -> StageResult:
    CALLS["prepare"] += 1
    CHAIN.append(("prepare", ""))
    _maybe_fail("prepare")
    run_key = activity.info().workflow_run_id
    return StageResult(
        runKey=run_key, stage="prepare", summary={"cached": CACHED, "frames": 12}
    )


def _stage_impl(name: str, *, signal_progress: bool = False) -> Any:
    """不含模型调用的阶段：一次跑完。"""

    async def run(args: StageInput) -> StageResult:
        CALLS[name] += 1
        CHAIN.append((name, args.fromStage))
        _maybe_fail(name)
        if signal_progress:
            await _signal_progress(name)
        return StageResult(runKey=args.runKey, stage=name)

    run.__name__ = f"fake_{name}"
    return activity.defn(name=STAGE_ACTIVITIES[name])(run)


def _model_stage_impl(name: str, *, calls: int = 1, signal_progress: bool = False) -> Any:
    """含模型调用的阶段：可重入。

    模拟 ``model_relay`` 的行为——第一遍跑到需要模型调用时把请求交回 workflow，
    等响应到手再跑一遍。``calls`` 是这个阶段要发起几次模型调用（4c 真实是三次），
    所以这个 activity 一共会被调 ``calls + 1`` 次。

    进度事件每遍都发，这正是重放的代价，也正是 workflow 侧要按 stepId 去重的原因。
    """

    async def run(args: ModelStepInput) -> ModelStep:
        CALLS[name] += 1
        CHAIN.append((name, args.fromStage))
        _maybe_fail(name)
        if signal_progress:
            await _signal_progress(name)
        done = sum(len(items) for items in args.responses.values())
        if done < calls:
            return ModelStep(
                request=ModelRequest(
                    capabilityId=UNDERSTAND_MEDIA_CAPABILITY,
                    payload={
                        "model": "fake-model",
                        "mediaUrls": ["https://example.invalid/m.mp4"],
                        "prompt": f"{name} 第 {done + 1} 次",
                    },
                    # digest 必须逐次不同，否则第二次调用会被当成第一次的重试。
                    digest=f"{name}-{done + 1}",
                    stage=name,
                    title=name,
                ).model_dump(mode="json")
            )
        return ModelStep(result=StageResult(runKey=args.runKey, stage=name))

    run.__name__ = f"fake_{name}"
    return activity.defn(name=STAGE_ACTIVITIES[name])(run)


async def _signal_progress(name: str) -> None:
    info = activity.info()
    handle = _CLIENT.get_workflow_handle(info.workflow_id, run_id=info.workflow_run_id)
    await handle.signal(
        PROGRESS_SIGNAL,
        [{"stepId": f"{name}.done", "message": f"{name} 完成", "data": None}],
    )


@activity.defn(name=STAGE_ACTIVITIES["report"])
async def fake_report(args: StageInput) -> VideoJudgeOutput:
    CALLS["report"] += 1
    CHAIN.append(("report", args.fromStage))
    _maybe_fail("report")
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
        cachedResult=bool(CACHED),
    )


_CLIENT: Client = None  # type: ignore[assignment]


async def _forward_spy(step_id: str, message: str, data: Any = None) -> None:
    """替掉 report_progress。

    真的那个内部会 ``execute_activity`` 去核心队列，测试里没有那一侧，不替就会挂在
    "找不到 activity"上无限重试。
    """
    FORWARDED.append((step_id, message))


async def _invoke_spy(capability_id: str, payload: Any, *, result_type: Any) -> Any:
    """替掉 invoke_capability。

    真的那个是 ``execute_child_workflow`` 到核心的 ``agentk`` 队列，测试里没有核心
    worker，不替就会一直等一个永远不会被取走的子 workflow。
    """
    INVOKED.append((capability_id, dict(payload)))
    return result_type(text='{"ok": true}', model="fake-model")


def _args() -> VideoJudgeInput:
    return VideoJudgeInput(videoUrl="https://example.invalid/a.mp4", prompt="测试")


async def run_case(
    client: Client, *, fail_once: set[str] | None = None, cached: bool = False
) -> VideoJudgeOutput:
    global CACHED
    reset()
    FAIL_ONCE.update(fail_once or set())
    CACHED = cached
    queue = f"test-stage-{uuid.uuid4().hex[:8]}"
    activities = [
        fake_prepare,
        _model_stage_impl("compile"),
        _model_stage_impl("observe", signal_progress=True),
        _model_stage_impl("scan"),
        # 4c 真实会发起三次调用（疑点复核、音频复听、音频反证），且第三次有条件依赖。
        # 通用驱动循环不需要知道这件事，这里就是验它真的能带任意次数。
        _model_stage_impl("recheck", calls=3),
        _stage_impl("aggregate"),
        _model_stage_impl("evidence"),
        fake_report,
    ]
    async with Worker(
        client,
        task_queue=queue,
        workflows=[AigcEvalVideoJudge],
        activities=activities,
        # 不用沙箱：测试要把 report_progress 换成替身，沙箱会重新导入模块、
        # 把替身还原回去。
        workflow_runner=UnsandboxedWorkflowRunner(),
    ):
        return await client.execute_workflow(
            AigcEvalVideoJudge.run,
            _args(),
            id=f"test-stage-wf-{uuid.uuid4().hex[:8]}",
            task_queue=queue,
        )


async def main() -> int:
    global _CLIENT
    address = os.getenv("TEMPORAL_ADDRESS", "127.0.0.1:7233").strip()
    print(f"连 Temporal：{address}")
    try:
        _CLIENT = await Client.connect(
            address, data_converter=pydantic_data_converter
        )
    except Exception as exc:  # noqa: BLE001
        print(f"跳过：连不上 Temporal（{exc}）。先跑 temporal server start-dev")
        return 0
    workflow_module.report_progress = _forward_spy  # type: ignore[assignment]
    workflow_module.invoke_capability = _invoke_spy  # type: ignore[assignment]

    print("\n[1] 八个阶段按顺序跑，含模型调用的五个被重入")
    result = await run_case(_CLIENT)
    check("workflow 完成", result.analysisStatus == "ok")
    expected = [
        "prepare",
        "compile",
        "observe",
        "scan",
        "recheck",
        "aggregate",
        "evidence",
        "report",
    ]
    check("阶段顺序正确", _stage_order() == expected, _stage_order())
    check("各阶段执行次数符合重入预期", dict(CALLS) == BASE_CALLS, dict(CALLS))

    print("\n[1b] 模型调用真的经 invoke_capability 发出去了")
    check("发起了 7 次模型调用", len(INVOKED) == 7, len(INVOKED))
    check(
        "全部走 understand_media",
        all(capability == UNDERSTAND_MEDIA_CAPABILITY for capability, _ in INVOKED),
        {capability for capability, _ in INVOKED},
    )
    check(
        "载荷里只有 URL，没有素材字节",
        all(
            set(payload) <= {"model", "mediaUrls", "prompt"} for _, payload in INVOKED
        ),
        [sorted(payload) for _, payload in INVOKED][:1],
    )

    print("\n[2] 状态按链条一段接一段读")
    # 每段读的都是**上一段**写的键，而不是固定读 prepare。
    reads = []
    for name, src in CHAIN:
        if src and (name, src) not in reads:
            reads.append((name, src))
    check(
        "fromStage 逐段递进",
        reads == list(zip(expected[1:], expected[:-1])),
        reads,
    )

    print("\n[3] 进度经 signal 中继到 report_progress")
    check("转发了 observe 的进度", ("observe.done", "observe 完成") in FORWARDED, FORWARDED)

    print("\n[4] 某段重跑时，前面的阶段不重跑")
    result = await run_case(_CLIENT, fail_once={"scan"})
    check("workflow 仍然完成", result.analysisStatus == "ok")
    check("只有 scan 多跑了一次", _extra_calls() == {"scan": 1}, _extra_calls())
    check(
        "scan 重跑后不必重发模型调用（前一次的响应还在 workflow 历史里）",
        len(INVOKED) == 7,
        len(INVOKED),
    )

    print("\n[5] 最后一段重跑也不牵连前面")
    result = await run_case(_CLIENT, fail_once={"report"})
    check("workflow 完成", result.analysisStatus == "ok")
    check("只有 report 多跑了一次", _extra_calls() == {"report": 1}, _extra_calls())

    print("\n[6] 命中整份报告缓存时直接出报告，中间六段一个都不起")
    result = await run_case(_CLIENT, cached=True)
    check("标了 cachedResult", result.cachedResult is True)
    check("只跑了 prepare 与 report", [name for name, _ in CHAIN] == ["prepare", "report"], CHAIN)
    check("report 读的是 prepare 的状态", CHAIN[-1][1] == "prepare", CHAIN)

    print()
    if FAILURES:
        print(f"FAILED: {len(FAILURES)} 项 — {FAILURES}")
        return 1
    print("ALL PASSED")
    return 0


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
