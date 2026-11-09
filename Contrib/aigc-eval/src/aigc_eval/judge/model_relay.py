"""让 activity 里的判定代码把模型调用"交出去"，而判定代码本身一行不改。

## 为什么挂在这一层

与 ``model_checkpoint`` 完全同一个理由，那份 docstring 里的三条现在依然成立：一次
质检的全部模型调用都汇到同一个口子（各 provider 的 ``generate_json``），挂在这里
能一处覆盖七个切点，不必在 1200 行编排里插六个边界。

区别只在于命中不了的时候干什么：

* ``CheckpointedClient`` 未命中就**直连模型**；
* ``SuspendingClient`` 未命中就**抛出请求**，让 workflow 去调，然后重放这一段。

## 重放：为什么这样反而更安全

worker 路径上模型调用必须回到 workflow 里（``invoke_capability`` 只能在 workflow
上下文 await）。要做到这一点，教科书做法是把七个切点各拆成 build/consume 两个函数、
再在 workflow 里为 4c 那三次有条件依赖的调用写一个状态机——那是七处外科手术，而
``pipeline.py`` 的 4c 是一大段带 ``try/except/else`` 的内联过程式代码，拆它必然改动
判定逻辑，也就直接威胁到"报告逐字节不变"这条底线。

这里换一条路：**阶段函数原样跑，跑到需要模型时抛出去，拿到响应后从阶段开头重跑。**
Temporal 自己就是这么工作的。代价是每次调用之前的确定性工作会重算一遍，收益是：

* 判定代码零改动，报告不变几乎是免费的；
* workflow 侧不需要任何状态机——一个通用循环就能驱动任意数量、任意条件依赖的调用；
* activity 定义还是八个，不是十八个。

## 重放为什么是安全的

阶段函数必须可重入。已经核过：``judge/`` 下没有任何 ``random`` / ``time.time`` /
``uuid`` / 临时文件名依赖，帧采样与素材裁剪都由 ``media_bundle`` 决定性推出。副作用
只有三类，都可重复：

* **磁盘产物**（抽帧、标框图）：路径由内容与时间点决定，重写同一份字节；
* **对象存储上传**：内容寻址，进程级 URL 备忘，重放时不会真的重传；
* **usage / trace**：只有跑到阶段末尾的那一遍才会被 ``_close_model_stage`` 写进状态，
  中间几遍的累积随 client 实例一起丢弃。所以最终记下来的恰好是完整的一份，不重不漏。

唯一需要额外处理的是**进度事件**：中间几遍会重复 emit。workflow 侧按 ``stepId`` 去重
（AgentK 本身也用 step_id 做幂等），因此调用方看到的事件序列与改造前一致。
"""

from __future__ import annotations

import json
import logging
import time
from typing import Any

from aigc_eval.judge.model_checkpoint import request_digest
from aigc_eval.providers.model_call import ModelResponse, NeedsModelCall

logger = logging.getLogger(__name__)


class SuspendingClient:
    """把 ``generate_json`` 拆成"组装 → 抛出 → 重放时解析"，其余属性全部透传。

    未知属性转发给内层 client：编排里会读 ``max_media`` / ``supports_video`` 做素材
    预算，而 ``max_media`` 必须在**组装素材之前**就能读到——提示词说「以下 16 张高清帧」
    而实际只送了 9 张，模型会把缺的当"看不到"，凭空多出一批 not_observable。
    """

    def __init__(
        self,
        inner: Any,
        *,
        provider: str,
        responses: dict[str, list[dict[str, Any]]] | None = None,
        max_attempts: int = 4,
        store: Any | None = None,
        refresh: bool = False,
        on_event: Any | None = None,
    ) -> None:
        self._inner = inner
        self._provider = provider
        # digest → 该请求已经拿到的响应，按尝试顺序。由 workflow 逐轮递进来。
        self._responses = responses or {}
        self._max_attempts = max_attempts
        self._store = store
        self._refresh = refresh
        self._on_event = on_event
        self.checkpoint_hits = 0
        self.checkpoint_misses = 0

    def __getattr__(self, name: str) -> Any:
        return getattr(self._inner, name)

    def checkpoint_stats(self) -> dict[str, Any]:
        return {
            "store": getattr(self._store, "kind", "none"),
            "hits": self.checkpoint_hits,
            "misses": self.checkpoint_misses,
        }

    def _notice(self, text: str, level: str = "info") -> None:
        if self._on_event is None:
            return
        try:
            self._on_event({"type": "notice", "level": level, "detail": text})
        except Exception:  # noqa: BLE001 - 通知失败不影响判定
            pass

    def generate_json(
        self,
        parts: list[dict[str, Any]],
        *,
        system_instruction: str | None = None,
        temperature: float = 0.1,
        stage: str = "model_call",
        title: str = "model call",
        max_attempts: int = 4,
        max_output_tokens: int | None = None,
    ) -> dict[str, Any]:
        started = time.perf_counter()
        budget = min(max_attempts, self._max_attempts)
        digest = self._digest(
            parts=parts,
            stage=stage,
            system_instruction=system_instruction,
            temperature=temperature,
            max_output_tokens=max_output_tokens,
        )

        # 1. 跨 run 检查点。Temporal 在同一次 run 内已经免费提供了"崩溃不重复付费"
        #    （响应在 workflow 历史里），所以这一层现在的价值收窄为**跨 run 复用**：
        #    同一条视频重复质检、开发调试循环。命中时连素材上传都省掉。
        cached = self._checkpoint_get(digest)
        if cached is not None:
            self.checkpoint_hits += 1
            self._notice(f"{title}：命中检查点，跳过本次模型调用")
            self._record_reuse(
                stage=stage,
                title=title,
                parts=parts,
                system_instruction=system_instruction,
                temperature=temperature,
                parsed=cached,
                started=started,
            )
            return cached

        attempts = self._responses.get(digest) or []

        # 2. 组装。上传发生在这里，所以检查点命中的路径上不会走到。
        request = self._inner.build_request(
            parts,
            system_instruction=system_instruction,
            stage=stage,
            title=title,
            digest=digest,
        )

        # 3. 消费 workflow 已经送回来的响应。解不开就看还有没有下一次尝试——
        #    解析失败意味着要重新发起模型调用，而响应已经在 workflow 历史里了，
        #    Temporal 不会替你重来，必须由 workflow 显式再调一次。
        outcome = None
        for index, raw in enumerate(attempts):
            attempted = request.model_copy(update={"attempt": index + 1})
            outcome = self._inner.consume_response(
                attempted,
                ModelResponse.model_validate(raw),
                parts=parts,
                system_instruction=system_instruction,
                temperature=temperature,
                started=started,
            )
            if outcome.ok and outcome.parsed is not None:
                self.checkpoint_misses += 1
                self._checkpoint_put(digest, outcome.parsed, stage=stage, title=title)
                return outcome.parsed
            if not outcome.retryable:
                break

        # 4. 还需要一次调用（或者已经用完预算）。
        if outcome is not None and (len(attempts) >= budget or not outcome.retryable):
            self.checkpoint_misses += 1
            self._inner.record_failure(
                request.model_copy(update={"attempt": len(attempts)}),
                parts=parts,
                system_instruction=system_instruction,
                temperature=temperature,
                detail=outcome.error or "未知错误",
                raw_text=None,
                started=started,
                max_attempts=budget,
            )
            # 抛而不是返回：judge 侧有几处对模型失败是降级处理的（4c 疑点复核失败时
            # 保留 4b 主判、照常出报告），必须让那段 except 原样接住。
            raise _as_exception(outcome)

        raise NeedsModelCall(request.model_copy(update={"attempt": len(attempts) + 1}))

    # ---------- 检查点 ----------

    def _digest(
        self,
        *,
        parts: list[dict[str, Any]],
        stage: str,
        system_instruction: str | None,
        temperature: float,
        max_output_tokens: int | None,
    ) -> str:
        """键的构成与 ``CheckpointedClient`` 逐字节一致，否则已有缓存全部失效。

        特别注意用的是 ``config.model`` 而不是实际派发到的模型：没有素材的调用会走
        generate_text 的 chat_model，但键里仍然记 understand_media 的模型名——改这一点
        会让口径漂移，必须原样保留。
        """
        model = str(getattr(getattr(self._inner, "config", None), "model", "") or "")
        try:
            return request_digest(
                provider=self._provider,
                model=model,
                stage=stage,
                parts=parts,
                system_instruction=system_instruction,
                temperature=temperature,
                max_output_tokens=max_output_tokens,
            )
        except Exception:  # noqa: BLE001 - 算不出键就当没有检查点
            logger.warning("检查点键计算失败，本次照常调用模型", exc_info=True)
            return ""

    def _checkpoint_get(self, digest: str) -> dict[str, Any] | None:
        if not digest or self._store is None or self._refresh:
            return None
        from aigc_eval.judge.checkpoint_store import unwrap_payload

        try:
            envelope = self._store.get_json(digest)
        except Exception:  # noqa: BLE001 - 读失败一律降级为未命中
            logger.warning("检查点读取异常，按未命中处理", exc_info=True)
            return None
        return unwrap_payload(envelope) if envelope else None

    def _checkpoint_put(
        self, digest: str, parsed: dict[str, Any], *, stage: str, title: str
    ) -> None:
        """只在解析成功后写。

        反面必须点明：一旦把解不开的响应写进缓存，digest 不变意味着后续每次重试都会
        命中同一份坏响应并跳过调用，表现是永久卡死而不是报错。未收敛的响应
        （``_unfinished``）同理不写。
        """
        if not digest or self._store is None:
            return
        if not parsed or parsed.get("_unfinished"):
            return
        from aigc_eval.judge.checkpoint_store import wrap_payload

        try:
            self._store.put_json(
                digest,
                wrap_payload(
                    parsed,
                    meta={
                        "provider": self._provider,
                        "model": str(
                            getattr(getattr(self._inner, "config", None), "model", "")
                            or ""
                        ),
                        "stage": stage,
                        "title": title,
                    },
                ),
            )
        except Exception:  # noqa: BLE001 - 写失败只是少一次复用
            logger.warning("检查点写入异常，已跳过", exc_info=True)

    def _record_reuse(
        self,
        *,
        stage: str,
        title: str,
        parts: list[dict[str, Any]],
        system_instruction: str | None,
        temperature: float,
        parsed: dict[str, Any],
        started: float,
    ) -> None:
        """命中时补齐 trace 与成本记录，形状与 ``CheckpointedClient`` 保持一致。

        不补的话审计链上会出现空洞：报告里少一条模型步骤，看起来像这一环没执行。
        """
        inner = self._inner
        try:
            inner.usage.append(
                {
                    "stage": stage,
                    "title": f"{title}（检查点复用）",
                    "attempt": 1,
                    "ok": True,
                    "from_checkpoint": True,
                    "elapsed_ms": int((time.perf_counter() - started) * 1000),
                    "model": getattr(getattr(inner, "config", None), "model", ""),
                }
            )
        except Exception:  # noqa: BLE001
            pass
        trace = getattr(inner, "trace", None)
        if trace is None:
            return
        try:
            trace.record_step(
                stage=stage,
                title=f"{title}（检查点复用）",
                system_instruction=system_instruction,
                parts=parts,
                raw_output=json.dumps(parsed, ensure_ascii=False)[:8000],
                parsed_output=parsed,
                started_at=started,
                temperature=temperature,
                source=f"{self._provider}_checkpoint",
            )
        except Exception:  # noqa: BLE001
            pass


def _as_exception(outcome: Any) -> Exception:
    from aigc_eval.providers.agentk import AgentKCallError

    detail = getattr(outcome, "error", None) or "AgentK 调用失败"
    code = getattr(outcome, "errorCode", "")
    if code:
        return AgentKCallError(
            detail, code=code, retryable=bool(getattr(outcome, "retryable", False))
        )
    return RuntimeError(detail)


def suspending(
    inner: Any,
    *,
    provider: str,
    responses: dict[str, list[dict[str, Any]]] | None = None,
    max_attempts: int = 4,
    refresh: bool = False,
    on_event: Any | None = None,
) -> SuspendingClient:
    """按环境取检查点存储，包一层重放代理。"""
    from aigc_eval.judge.checkpoint_store import shared_checkpoint_store

    return SuspendingClient(
        inner,
        provider=provider,
        responses=responses,
        max_attempts=max_attempts,
        store=shared_checkpoint_store(),
        refresh=refresh,
        on_event=on_event,
    )
