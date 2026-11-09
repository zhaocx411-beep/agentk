"""编排 workflow：按阶段依次起 activity，并把它们 signal 回来的事件转成 report_progress。

**为什么是多个 activity 而不是一个。** Temporal 的续跑能力只作用在 activity 边界上：
已完成的 activity 结果进 workflow 历史，worker 重启后不会重跑；而一个正在执行的
activity 崩了，只能整个重来。质检是分钟到小时级的流程，塞在单个 activity 里意味着
"崩在第 90 分钟 → 从第 0 分钟重来"。拆成八段之后，崩溃只损失当前那一段。

**进度为什么要绕一圈。** ``report_progress`` 只能在 workflow 里调用（它内部走
``workflow.execute_activity``），而阶段事件只能在 activity 侧拿到。于是让 activity
signal 回来、由这里转发。signal 会进入 workflow 历史，重放时顺序一致，因此这个中继
是确定性的。

**超时预算怎么切。** 对外声明的能力超时是 2 小时（``INSPECT_TIMEOUT``），那是调用方
唯一能看到的数，所以各阶段预算之和必须落在它以内——否则就是声明了一个自己做不到的
数。每个阶段的 ``schedule_to_close`` 取自己的预算，它同时覆盖重试，所以"所有阶段预算
之和 + 派发余量"就是最坏情况总耗时。
"""

from __future__ import annotations

import asyncio
import re
from datetime import timedelta
from typing import Any

from abi import (
    MAX_MODEL_ROUNDS,
    MODEL_STAGES,
    PROGRESS_SIGNAL,
    STAGE_ACTIVITIES,
    WORKFLOW_TYPE,
    GenerateTextOutput,
    ModelStep,
    ModelStepInput,
    StageInput,
    StageResult,
    UnderstandMediaOutput,
    VideoJudgeInput,
    VideoJudgeOutput,
)
from agentk.worker import invoke_capability, report_progress
from temporalio import workflow
from temporalio.common import RetryPolicy
from temporalio.exceptions import ApplicationError

with workflow.unsafe.imports_passed_through():
    from aigc_eval.providers.model_call import (
        GENERATE_TEXT_CAPABILITY,
        UNDERSTAND_MEDIA_CAPABILITY,
        ModelRequest,
        ModelResponse,
    )

# 对外声明的 capability 最长执行时间（worker.py 里的 ``Capability(timeout=...)``）。
#
# **为什么是 6 小时而不是原来的 2 小时。** 改造前模型等待发生在 activity 内部，被
# 算进阶段预算里（scan 给 25 分钟就是为了容纳一次超长模型调用）。现在模型调用是
# 子 workflow，两者都算进 workflow 执行超时，但账完全变了（下面的断言会算给你看）：
#
#   activity 预算（只剩上传、抽帧、解析、组装）   = 72 分钟
# + 模型等待（15 次尝试 × 360 秒）                = 90 分钟
# + 派发余量                                      =  5 分钟
#                                                 = 167 分钟
#
# 取 6 小时而不是刚好盖住的 3 小时，是因为上面那笔账没算 activity 重试：
# ``_RESUME_ON_LOSS`` 允许每个阶段因实例丢失重跑，最坏情况 activity 那 72 分钟要翻倍，
# 合计约 239 分钟。6 小时能盖住一整轮阶段重跑，SDK 上限是 24 小时。
INSPECT_TIMEOUT = timedelta(hours=6)

# 派发排队余量：activity 从"排进队列"到"被 worker 取走"之间的时间也算在
# schedule_to_close 里，所以总预算要留出这一块。
_QUEUE_BUDGET = timedelta(minutes=5)

# 各阶段的 **activity** 预算。注意这里**不再包含模型等待**——模型调用已经移出
# activity 成了子 workflow，所以含模型的五个阶段预算大幅缩小，只需覆盖上传、抽帧、
# 解析、组装，以及重放时把这些确定性工作重算一遍的代价。
#
# * prepare 要下载素材 + ffprobe + V2V 逐时刻 SSIM + 抽帧，是唯一的重 IO 阶段，
#   也是唯一不重放的含 IO 阶段，预算不变；
# * scan（4b）要编码十几张高清帧并上传，重放会重算一遍，所以给得比其他模型阶段宽；
# * aggregate 是纯计算，秒级，给 3 分钟纯粹是防御；
# * report 只组装 + 生成 HTML + 传对象存储。
_STAGE_BUDGET: dict[str, timedelta] = {
    "prepare": timedelta(minutes=20),
    "compile": timedelta(minutes=4),
    "observe": timedelta(minutes=8),
    "scan": timedelta(minutes=12),
    "recheck": timedelta(minutes=10),
    "aggregate": timedelta(minutes=3),
    "evidence": timedelta(minutes=7),
    "report": timedelta(minutes=8),
}

# 一次模型调用最坏能等多久。这不是我们设的：``understand_media`` 在平台侧是
# Activity 型能力，manifest 里 ``activityStartToClose = 5min + 1min = 360s``，承载它的
# 容器 workflow 直接把这个值当 ScheduleToCloseTimeout 用（agentk/internal/pipeline/
# external.go）。也就是说走原生路径的天花板是 360 秒，比网关的 300 秒只宽 60 秒——
# 别把"消掉了 300 秒上限"当成这次改造的收益。
_MODEL_CALL_CEILING = timedelta(seconds=360)

# 每次调用允许的尝试次数。scan 是最重也最容易被输出长度截断的一次（一次吐八维 ×
# 28 子判据），多给一次；其余两次够了。
_MODEL_ATTEMPTS: dict[str, int] = {"scan": 3}
_DEFAULT_MODEL_ATTEMPTS = 2

# 心跳超时：实例被杀、容器被驱逐、网络断开之后，Temporal 要等这么久才认定这一段
# 失联并重新派发。改造后 activity 里不再有"等模型"这种长时间不产出事件的动作，
# 但抽帧与上传仍可能安静几分钟，所以保持 5 分钟。
_HEARTBEAT_TIMEOUT = timedelta(minutes=5)

# 最坏情况总账。**这个断言必须随预算一起改**，它是唯一能在启动时就发现"声明了一个
# 自己做不到的超时"的地方。
_ACTIVITY_BUDGET = sum(_STAGE_BUDGET.values(), timedelta())
_MODEL_WAIT_BUDGET = _MODEL_CALL_CEILING * sum(
    _MODEL_ATTEMPTS.get(stage, _DEFAULT_MODEL_ATTEMPTS)
    # 七个切点：compile 1、observe 1、scan 1、recheck 3、evidence 1。
    for stage in ("compile", "observe", "scan", "recheck", "recheck", "recheck", "evidence")
)
_TOTAL_BUDGET = _ACTIVITY_BUDGET + _MODEL_WAIT_BUDGET + _QUEUE_BUDGET
if _TOTAL_BUDGET > INSPECT_TIMEOUT:  # pragma: no cover - 启动即暴露
    raise RuntimeError(
        f"最坏情况合计 {_TOTAL_BUDGET}（activity {_ACTIVITY_BUDGET} + "
        f"模型等待 {_MODEL_WAIT_BUDGET} + 派发 {_QUEUE_BUDGET}）"
        f"超过对外声明的 {INSPECT_TIMEOUT}，"
        "这会让调用方按声明超时、而 workflow 仍在跑"
    )

# 能力 id → 回参类型。invoke_capability 要显式给 result_type，写在一处免得漂移。
_RESULT_TYPES: dict[str, type] = {
    UNDERSTAND_MEDIA_CAPABILITY: UnderstandMediaOutput,
    GENERATE_TEXT_CAPABILITY: GenerateTextOutput,
}

# 允许一次重试，专门用来接住"进程/容器丢失"。
#
# 心跳超时后 Temporal 会把该阶段重新派发。重派只影响当前阶段：前面阶段的结果在
# workflow 历史里，不会重跑；当前阶段内已完成的模型调用由模型级检查点
# （``aigc_eval.judge.model_checkpoint``）按请求哈希复用，因此重试的代价是"重做这一段
# 的确定性工作"，而不是"重花这一段的模型钱"。
#
# 应用层异常不会走到这条重试上：activity 侧把它们转成 non_retryable 的
# ApplicationError。那类失败重跑只会再失败一次，白等一轮。
_RESUME_ON_LOSS = RetryPolicy(maximum_attempts=2)

# 阶段执行顺序。report 单独处理（回参类型不同），所以不在这个序列里。
_CHAIN = ("prepare", "compile", "observe", "scan", "recheck", "aggregate", "evidence")


@workflow.defn(name=WORKFLOW_TYPE)
class AigcEvalVideoJudge:
    def __init__(self) -> None:
        self._pending: list[dict[str, Any]] = []
        self._seen_steps: set[str] = set()

    @workflow.signal(name=PROGRESS_SIGNAL)
    def on_progress(self, events: list[dict[str, Any]]) -> None:
        """只做入列。转发要 await，不能在 signal 处理器里做。"""
        self._pending.extend(events)

    @workflow.run
    async def run(self, args: VideoJudgeInput) -> VideoJudgeOutput:
        chain = asyncio.ensure_future(self._pipeline(args))

        # 边跑边转发。退出条件是「活儿干完了 **且** 队列排空」，
        # 少了后半句会漏掉最后一批事件——其中就包括「生成报告」完成。
        while True:
            await workflow.wait_condition(lambda: bool(self._pending) or chain.done())
            batch, self._pending = self._pending, []
            for event in batch:
                await self._forward(event)
            if chain.done() and not self._pending:
                break

        return await chain

    async def _pipeline(self, args: VideoJudgeInput) -> VideoJudgeOutput:
        """依次跑各阶段。每段只把"读哪个键"传给下一段，状态本体不进历史。"""
        result: StageResult = await self._stage("prepare", args)
        run_key = result.runKey
        previous = result.stage

        if result.summary.get("cached"):
            # 命中整份报告缓存：中间阶段一个都不起，直接投影历史结果。
            # 与单 activity 时代的语义一致（cachedResult=true、无模型开销）。
            return await self._report(run_key, previous)

        for stage in _CHAIN[1:]:
            if stage in MODEL_STAGES:
                result = await self._model_stage(stage, run_key, previous)
            else:
                result = await self._stage(
                    stage, StageInput(runKey=run_key, fromStage=previous)
                )
            previous = result.stage

        return await self._report(run_key, previous)

    async def _stage(self, stage: str, payload: Any) -> StageResult:
        budget = _STAGE_BUDGET[stage]
        return await workflow.execute_activity(
            STAGE_ACTIVITIES[stage],
            payload,
            # start_to_close 比 schedule_to_close 少留一分钟给派发；
            # 两者都给，是为了让"排队太久"和"跑得太久"在历史里能分辨。
            start_to_close_timeout=budget - timedelta(minutes=1),
            schedule_to_close_timeout=budget,
            heartbeat_timeout=_HEARTBEAT_TIMEOUT,
            retry_policy=_RESUME_ON_LOSS,
            result_type=StageResult,
        )

    # -- 含模型调用的阶段：build / invoke / consume 的通用驱动 ----------------

    async def _model_stage(
        self, stage: str, run_key: str, previous: str
    ) -> StageResult:
        """跑一个含模型调用的阶段。

        形状是一个循环，而不是"每个切点一段代码"：

            阶段 activity 跑 → 要模型吗？
              要 → invoke_capability → 把响应加进 responses → 再跑一遍
              不要 → 阶段跑完，拿结果

        这样一个循环就能驱动任意数量、任意条件依赖的调用。4c 那三次（疑点复核、音频
        复听、音频反证，第三次只在第二次判出 violated 时才发生）不需要 workflow 知道
        它们的依赖关系——阶段函数自己按原有逻辑决定要不要继续，需要时再抛一个请求
        出来。判定逻辑因此一行都不用改。

        ``responses`` 只装本阶段的响应，随每轮增长。它同时是重放的依据和"这次调用已经
        做过"的凭证：模型结果进了 workflow 历史，崩溃重启后重放会直接命中，不会重复付费。
        """
        responses: dict[str, list[dict[str, Any]]] = {}
        for _ in range(MAX_MODEL_ROUNDS):
            step = await self._model_step(stage, run_key, previous, responses)
            if step.request is None:
                if step.result is None:  # pragma: no cover - activity 契约违反
                    raise ApplicationError(
                        f"阶段 {stage} 既没给请求也没给结果",
                        type="InvalidStageStep",
                        non_retryable=True,
                    )
                return step.result

            request = ModelRequest.model_validate(step.request)
            response = await self._invoke_model(stage, request)
            responses.setdefault(request.digest, []).append(
                response.model_dump(mode="json")
            )

        raise ApplicationError(
            f"阶段 {stage} 的模型调用轮数超过 {MAX_MODEL_ROUNDS}，疑似重放未收敛",
            type="ModelRoundsExhausted",
            non_retryable=True,
        )

    async def _model_step(
        self,
        stage: str,
        run_key: str,
        previous: str,
        responses: dict[str, list[dict[str, Any]]],
    ) -> ModelStep:
        budget = _STAGE_BUDGET[stage]
        return await workflow.execute_activity(
            STAGE_ACTIVITIES[stage],
            ModelStepInput(runKey=run_key, fromStage=previous, responses=responses),
            start_to_close_timeout=budget - timedelta(minutes=1),
            schedule_to_close_timeout=budget,
            heartbeat_timeout=_HEARTBEAT_TIMEOUT,
            retry_policy=_RESUME_ON_LOSS,
            result_type=ModelStep,
        )

    async def _invoke_model(self, stage: str, request: ModelRequest) -> ModelResponse:
        """调一次平台能力。失败也带回去，不在这里抛。

        为什么不抛：judge 侧有几处对模型失败是**降级**处理的（4c 疑点复核失败时保留
        4b 主判、照常出报告）。在这里抛会让整次质检失败，报告内容随之改变。把失败当成
        一种响应送回 activity，那段既有的 except 就能原样接住，该重试的由重放代理按
        ``max_attempts`` 决定，该降级的照常降级。
        """
        result_type = _RESULT_TYPES.get(request.capabilityId)
        if result_type is None:  # pragma: no cover - 能力表写漏了
            raise ApplicationError(
                f"未登记的能力 {request.capabilityId}",
                type="UnknownCapability",
                non_retryable=True,
            )
        attempts = _MODEL_ATTEMPTS.get(stage, _DEFAULT_MODEL_ATTEMPTS)
        await self._forward(
            {
                "stepId": f"{stage}.model.{request.attempt}",
                "message": f"{request.title}（第 {request.attempt}/{attempts} 次调用）",
            }
        )
        # 用 workflow.now() 量耗时而不是 activity 里的 perf_counter：前者可重放，
        # 重放时得到的是同一个值，不会让历史与重放对不上。
        started = workflow.now()
        try:
            output = await invoke_capability(
                request.capabilityId, request.payload, result_type=result_type
            )
        except asyncio.CancelledError:
            # 取消是真信号。invoke_capability 用 WAIT_CANCELLATION_COMPLETED，
            # 在途的模型调用会真的被取消，这里原样抛给外层收尾。
            raise
        except Exception as error:  # noqa: BLE001 - 交给 activity 侧决定重试或降级
            return ModelResponse.from_error(
                # 整条链而不是 str(error)：子 workflow 的外壳文案没有信息量。
                _error_chain_text(error),
                code=_error_code(error),
                retryable=_is_retryable(error),
                elapsed_ms=_elapsed_ms(started),
            )
        return ModelResponse.from_output(
            request.capabilityId,
            output.model_dump(mode="json"),
            elapsed_ms=_elapsed_ms(started),
        )

    async def _report(self, run_key: str, previous: str) -> VideoJudgeOutput:
        budget = _STAGE_BUDGET["report"]
        return await workflow.execute_activity(
            STAGE_ACTIVITIES["report"],
            StageInput(runKey=run_key, fromStage=previous),
            start_to_close_timeout=budget - timedelta(minutes=1),
            schedule_to_close_timeout=budget,
            heartbeat_timeout=_HEARTBEAT_TIMEOUT,
            retry_policy=_RESUME_ON_LOSS,
            result_type=VideoJudgeOutput,
        )

    async def _forward(self, event: dict[str, Any]) -> None:
        step_id = str(event.get("stepId") or "").strip()
        if not step_id:
            return
        # 去重是重放的必要配套：含模型调用的阶段会被重跑数遍，每遍都从阶段开头
        # 重新 emit，同一个 stepId 因此会来好几次。AgentK 本身也用 step_id 做幂等，
        # 这里先挡一层，让调用方看到的事件序列与改造前逐条一致。
        if step_id in self._seen_steps:
            return
        self._seen_steps.add(step_id)
        try:
            await report_progress(
                step_id,
                str(event.get("message") or ""),
                event.get("data"),
            )
        except asyncio.CancelledError:
            # 取消是真信号，原样抛给外层收尾。
            raise
        except Exception:  # noqa: BLE001
            # 进度是旁路：丢一条不该让一次已经花掉模型开销的质检失败。
            workflow.logger.warning("进度上报失败：%s", step_id)


def _elapsed_ms(started: Any) -> int:
    """用 workflow.now() 的差值量模型耗时。

    不用 activity 里的 ``time.perf_counter()``：模型等待已经不在 activity 里了，而且
    只有 ``workflow.now()`` 在重放时给出同一个值，历史与重放才对得上。
    """
    return int((workflow.now() - started).total_seconds() * 1000)


# 平台把错误分类写在文案里。实测格式（STAGING，2026-08，四个样本一致）：
#
#   invalid capability invocation: capability generate_text: model and prompt are required
#     (type: InvalidArgument, retryable: false)
#     : model and prompt are required (type: permanentActivityError, retryable: true)
#     : model and prompt are required
#
# **只认第一个标记。** 末尾那个内层标记恒为 ``retryable: true``——四个样本里连
# ``permanentActivityError``（名字就叫"永久"）和 ``wrapError`` 都是 true。取最后一个
# 等于把每一个永久错误都重发一遍，而重发一次 understand_media 是真花钱。
# ``docs/AGENTK接入自查.md`` §3.1 记的"媒体类型不支持却被重试到超时"，机制正在这里。
_ERROR_CLASS_RE = re.compile(r"\(type:\s*(\w+),\s*retryable:\s*(true|false)\)")

# 没有分类标记时的兜底。Temporal 自己的失败（子 workflow 超时、任务队列不可用）不带
# 平台标记，只能按文案认。故意**不**把"未知错误"算成可重试：宁可少重试，不多花钱。
_RETRYABLE_MARKERS = ("deadline", "timeout", "unavailable", "resource_exhausted", "aborted")


def _error_chain_text(error: BaseException) -> str:
    """把异常链上每一层的文案拼起来。

    **只看最外层是不够的。** ``invoke_capability`` 的失败被 Temporal 包成
    ``ChildWorkflowError``，而它自己的文案恒为 ``Child Workflow execution failed``，
    平台真正说了什么在 ``__cause__`` 那一层（``converter/_failure_converter.py`` 在
    ``from_failure`` 里按 ``failure.cause`` 逐层挂 ``__cause__``）。

    只读最外层会同时坏掉两件事，而且都是静默的：

    1. 报告与进度事件里只剩那句通用文案，真实原因（音频类型不支持？模型没开放？
       素材取不到？）全部丢失——这三种的处理人完全不同，分不出来就只能猜。
    2. ``_ERROR_CLASS_RE`` 匹配不到平台标记，于是每个错误都落进"未知且不可重试"，
       连真正该重发的 unavailable / deadline 都不再重发。

    深度设上限并去重：异常链理论上可以很长，报告里也不需要同一句话重复三遍。
    """
    texts: list[str] = []
    current: BaseException | None = error
    for _ in range(10):
        if current is None:
            break
        text = str(current).strip()
        if text and text not in texts:
            texts.append(text)
        # 优先 __cause__（显式 raise from 与 Temporal 的失败还原都走它）；
        # 没有时才看 __context__，那是隐式的上下文，噪声更大但比没有好。
        current = current.__cause__ or current.__context__
    return " <- ".join(texts)


def _classify(error: BaseException) -> tuple[str, bool]:
    """从错误里读出 (分类, 是否值得重发)。

    判据取自整条异常链，理由见 ``_error_chain_text``。
    """
    chain = _error_chain_text(error)
    marks = _ERROR_CLASS_RE.findall(chain)
    if marks:
        kind, retryable = marks[0]
        return kind, retryable == "true"
    lowered = chain.lower()
    for marker in _RETRYABLE_MARKERS:
        if marker in lowered:
            return marker, True
    return "", False


def _error_code(error: BaseException) -> str:
    return _classify(error)[0]


def _is_retryable(error: BaseException) -> bool:
    return _classify(error)[1]
