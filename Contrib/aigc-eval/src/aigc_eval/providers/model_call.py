"""一次模型调用被切成三段之后，三段之间传递的东西。

## 为什么要切

worker 路径上模型调用必须走 ``agentk.worker.invoke_capability``，而它底层是
``workflow.execute_child_workflow``，只能在 workflow 上下文里 await。activity 里
调不了，也没法让 activity 阻塞等 workflow 把结果送回来——Temporal 没有
workflow → 运行中 activity 的数据通道（只有取消信号）。

于是一次调用只能是：

* **build**（activity）：组装素材、上传对象存储、拼提示词 → 产出 ``ModelRequest``；
* **invoke**（workflow）：``invoke_capability`` → 产出 ``ModelResponse``；
* **consume**（activity）：解析响应、记 usage/trace → 产出判定要用的 dict。

studio 与批处理不在 Temporal 里跑，它们把这三段在进程内串起来（见
``HttpInvoker``），因此两条路径共用同一对 build/consume，判定逻辑只有一份。

## 尺寸

``ModelRequest.payload`` 与 ``ModelResponse`` 都要过 Temporal payload（上限 2MB）。
在 100 份真实 trace 上量过：最大请求 45.4 KB（4c 疑点复核）、最大响应 66.9 KB
（4b 主判），单次 run 全部模型阶段合计约 310 KB。素材本身不在里面——它们上传到
对象存储换成 URL，payload 里只有 URL。跨阶段的累积状态也不在里面，那个走
``worker/state.py`` 的对象存储。
"""

from __future__ import annotations

import json
from typing import Any, Protocol

from pydantic import BaseModel, ConfigDict, Field

# 平台能力 id。写在这里而不是各处硬编码，是因为 workflow 要按它选 result_type。
UNDERSTAND_MEDIA_CAPABILITY = "agentk.understand_media"
GENERATE_TEXT_CAPABILITY = "agentk.generate_text"


class ModelRequest(BaseModel):
    """一次待发起的模型调用。build 段的产出，workflow 侧的入参。

    刻意不带 ``parts``：那里面有 base64 内联媒体，几 MB 起，过 Temporal 必然超限。
    consume 段拿得到 parts，因为它与 build 跑在同一次重放里（见 ``model_relay``），
    不需要跨边界搬运。
    """

    model_config = ConfigDict(extra="forbid")

    capabilityId: str = Field(description="平台能力 id，workflow 据此选 result_type。")
    payload: dict[str, Any] = Field(description="能力入参，已按 schema 拼好。")
    digest: str = Field(default="", description="请求内容哈希；检查点键，也是响应的归属键。")
    stage: str = Field(default="", description="用于 usage / trace 归因。")
    title: str = Field(default="", description="面向人的步骤名，出现在报告与进度里。")
    attempt: int = Field(default=1, description="第几次尝试；重试时递增。")


def raw_text_from_output(capability_id: str, output: dict[str, Any]) -> str:
    """把两个能力形状不同的输出收敛成待解析的原始文本。

    ``understand_media`` 回 ``{text, model, params}``；``generate_text`` 回
    ``{result}``，未带 outputSchema 时 result 就是模型原文（实测直调返回
    ``{'result': '收到'}``）。它也可能已经是对象，那就原样序列化回去交给同一套解析。

    这个差异必须只有一处实现：workflow 侧和 studio 侧都要做同样的收敛，写两遍就会
    在某次改动里漂移，表现是其中一条路径突然解不出 JSON。
    """
    if capability_id == GENERATE_TEXT_CAPABILITY:
        result = output.get("result")
        if isinstance(result, str):
            return result.strip()
        if result is None:
            return ""
        return json.dumps(result, ensure_ascii=False)
    return str(output.get("text") or "").strip()


class ModelResponse(BaseModel):
    """一次模型调用的回参。workflow 侧的产出，consume 段的入参。

    ``error`` 非空表示这次调用在平台侧就失败了（``rawText`` 为空）。把失败也当成一种
    响应送回 activity，而不是让 workflow 直接抛：judge 侧有几处对模型失败是**降级**
    处理的（``pipeline.py`` 的 4c 疑点复核失败时保留 4b 主判、照常出报告），必须让
    那段 except 原样接住，否则报告内容就变了。
    """

    model_config = ConfigDict(extra="ignore")

    rawText: str = Field(default="", description="模型原文，待 extract_json_object 解析。")
    model: str = Field(default="", description="平台回报的实际模型名。")
    params: dict[str, Any] = Field(default_factory=dict)
    elapsedMs: int = Field(default=0, description="由 workflow 用 workflow.now() 前后相减得到。")
    error: str | None = Field(default=None, description="平台侧失败原因；成功时为 None。")
    errorCode: str = Field(default="", description="错误码，用于沿用原有的退避口径。")
    errorRetryable: bool = Field(default=False, description="平台侧失败是否值得重发。")

    @classmethod
    def from_output(
        cls, capability_id: str, output: dict[str, Any], *, elapsed_ms: int = 0
    ) -> ModelResponse:
        return cls(
            rawText=raw_text_from_output(capability_id, output),
            model=str(output.get("model") or ""),
            params=output.get("params") or {},
            elapsedMs=elapsed_ms,
        )

    @classmethod
    def from_error(
        cls,
        message: str,
        *,
        code: str = "",
        retryable: bool = False,
        elapsed_ms: int = 0,
    ) -> ModelResponse:
        return cls(
            error=message,
            errorCode=code,
            errorRetryable=retryable,
            elapsedMs=elapsed_ms,
        )


class ModelOutcome(BaseModel):
    """consume 段的产出：解出来了，还是要重来。

    「是否重试」的决定权不在这里——consume 只报告解析结果，由上层（重放代理或
    workflow）按 ``max_attempts`` 决定。这样传输失败与**输出解析失败**这两类完全
    不同的故障能共用一套重试账，而后者是最容易在重构中静默丢掉的语义：解析失败
    意味着需要重新发起模型调用，而响应已经落进 workflow 历史了，Temporal 不会
    替你重来。
    """

    model_config = ConfigDict(extra="forbid")

    ok: bool
    parsed: dict[str, Any] | None = None
    error: str | None = None
    errorCode: str = ""
    retryable: bool = False


class NeedsModelCall(BaseException):
    """判定代码要发起模型调用，而当前上下文发不了——把请求带出去。

    这不是错误，是控制流：``model_relay.SuspendingClient`` 用它把「阶段跑到这里
    需要一次模型调用」这件事抛给 activity 外壳，由 workflow 去调，再重放这一段。

    **必须继承 BaseException 而不是 Exception。** judge 侧有好几处 ``except
    Exception`` 是用来给模型失败做降级的（``pipeline.py`` 的 4c 疑点复核失败时保留
    4b 主判、``providers/agentk.py`` 的素材旁路）。继承 Exception 会让这个控制流信号
    被那些 handler 吞掉——表现是阶段"成功"返回了一份降级结论，模型调用从未发生，
    而且不会有任何报错。同理于 ``asyncio.CancelledError`` 在 3.8 之后的改法。
    """

    def __init__(self, request: ModelRequest):
        super().__init__(f"需要模型调用：{request.stage} / {request.title}")
        self.request = request


class ModelInvoker(Protocol):
    """把 ``ModelRequest`` 换成 ``ModelResponse`` 的东西。

    worker 路径不实现它——那条路上 invoke 发生在 workflow 里，activity 侧看到的是
    ``NeedsModelCall``。这个 Protocol 是给 studio 与批处理用的。
    """

    def invoke(self, request: ModelRequest) -> ModelResponse: ...


class JudgeClient(Protocol):
    """判定各阶段对模型客户端的全部要求：一个 ``generate_json``。

    阶段类实际会收到的东西有好几种——``AgentKClient``、包了检查点的
    ``CheckpointedClient``、worker 路径上包了重放的 ``SuspendingClient``、测试里的
    ``ReplayClient``。它们之间没有继承关系，靠鸭子类型共用同一个签名，所以这里用
    Protocol 而不是基类。

    签名必须完全一致：少一个关键字参数，调用方就是 TypeError。
    """

    def generate_json(
        self,
        parts: list[dict[str, Any]],
        *,
        system_instruction: str | None = None,
        temperature: float = 0.1,
        stage: str = "agentk_call",
        title: str = "AgentK understand_media",
        max_attempts: int = 4,
        max_output_tokens: int | None = None,
    ) -> dict[str, Any]: ...
