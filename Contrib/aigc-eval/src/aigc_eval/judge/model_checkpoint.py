"""模型调用级检查点：按请求内容哈希复用上一次的模型响应。

## 为什么挂在这一层，而不是七个阶段的边界上

一次质检真花钱的是四到五次模型调用（3 拆解、4 观察、4b 主判、4c 复核、定位），
它们全部汇到同一个口子——各 provider 的 ``generate_json``（三个 provider 是鸭子类型
同签名）。挂在这里有三个别处拿不到的好处：

1. **一处改动覆盖全部模型调用**，不必在 1200 行编排里插六个边界；
2. **产出天然可序列化**：模型响应本身就是 JSON，而阶段产出里混着 ``Path``
   （``media_bundle["frames"]``）和 run 级路径（``_uni["_frames"]``）；
3. **磁盘产物不需要还原**。这是最关键的一点：跳过阶段的话，抽帧、放大帧、证据帧
   都得连带还原，而它们的路径里带 run_id、容器重启后文件也没了。挂在模型调用上，
   抽帧标注这些确定性工作照常执行（ffmpeg 和 CPU 不花钱），只有模型响应来自
   检查点，于是所有磁盘产物由原代码自然重建。

## 键的构成

键 = 整个请求的哈希：provider、模型名、stage、``parts``（文本与 base64 内联媒体
原样入哈希）、system_instruction、temperature、输出上限。``parts`` 直接哈希字节而
不是"素材来源 + 时间点"，因为 parts 不携带来源信息，而字节是精确的：素材、帧数预算、
提示词、镜头切分——任何一处变了，请求字节就变，检查点自然失效。宁可多跑一次，
不能让一次判定复用到不该复用的响应上。

哈希在**上传之前**算，所以 agentk 通道命中检查点时连素材上传都省掉了。

## 不缓存什么

失败的调用不写检查点（异常直接抛出），Gemini 的 ``_unfinished`` 未收敛响应也不写——
那不是一个可复用的结论。``refresh=True``（调用方要求重跑）只跳过读、照常写，与
``result_cache`` 的语义一致：用户要的是重新判一次，不是把上次的答案再拿出来。
"""

from __future__ import annotations

import hashlib
import json
import logging
import time
from typing import Any

from aigc_eval.judge.checkpoint_store import (
    CheckpointStore,
    unwrap_payload,
    wrap_payload,
)

logger = logging.getLogger(__name__)

# 键的口径版本。改了下面 request_digest 的构成就要加，否则新代码会读到旧口径的键。
DIGEST_VERSION = "mc1"


def request_digest(
    *,
    provider: str,
    model: str,
    stage: str,
    parts: list[dict[str, Any]],
    system_instruction: str | None,
    temperature: float,
    max_output_tokens: int | None,
) -> str:
    """整个请求的 sha256。

    用 ``sort_keys`` 的紧凑 JSON 做规范化：dict 插入顺序在不同代码路径下可能不同，
    不排序会让同一个请求算出两个键。
    """
    payload = {
        "v": DIGEST_VERSION,
        "provider": provider,
        "model": model,
        "stage": stage,
        "system_instruction": system_instruction or "",
        "temperature": round(float(temperature), 4),
        "max_output_tokens": int(max_output_tokens or 0),
        "parts": parts,
    }
    blob = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    return hashlib.sha256(blob.encode("utf-8")).hexdigest()


class CheckpointedClient:
    """给 provider client 套一层检查点，其余属性全部透传。

    用代理而不是继承或改三个 provider：三个 client 是鸭子类型（``generate_json``
    签名一致，都有 ``trace`` / ``usage`` / ``config``），编排里还会读 ``max_media``
    等属性做素材预算，所以未知属性一律转发给内层实例。
    """

    def __init__(
        self,
        inner: Any,
        *,
        store: CheckpointStore,
        provider: str,
        refresh: bool = False,
        on_event: Any | None = None,
    ):
        self._inner = inner
        self._store = store
        self._provider = provider
        self._refresh = refresh
        self._on_event = on_event
        self.checkpoint_hits = 0
        self.checkpoint_misses = 0

    def __getattr__(self, name: str) -> Any:
        # 只在本对象没有该属性时触发，所以不会截走上面那几个自有字段。
        return getattr(self._inner, name)

    def _notice(self, text: str) -> None:
        if self._on_event is None:
            return
        try:
            self._on_event({"type": "notice", "level": "info", "detail": text})
        except Exception:  # noqa: BLE001 - 通知失败不影响判定
            pass

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
        """命中时补齐 trace 与成本记录。

        不补的话审计链上会出现空洞：报告里少一条模型步骤，看起来像这一环没执行。
        成本记录标 ``from_checkpoint`` 且不带 token 数——这次确实没有计费。
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
        model = str(getattr(getattr(self._inner, "config", None), "model", "") or "")
        try:
            key = request_digest(
                provider=self._provider,
                model=model,
                stage=stage,
                parts=parts,
                system_instruction=system_instruction,
                temperature=temperature,
                max_output_tokens=max_output_tokens,
            )
        except Exception:  # noqa: BLE001 - 算不出键就当没有检查点
            logger.warning("检查点键计算失败，本次直连模型", exc_info=True)
            key = ""

        if key and not self._refresh:
            # 存储层自己也吞异常，这里再兜一层不是冗余：检查点的全部意义是省掉一次
            # 已经付过钱的调用，绝不能因为某个后端违约（或将来换了实现）而把一次
            # 正在跑的质检打断。读失败一律降级为未命中。
            try:
                envelope = self._store.get_json(key)
            except Exception:  # noqa: BLE001
                logger.warning("检查点读取异常，按未命中处理", exc_info=True)
                envelope = None
            parsed = unwrap_payload(envelope) if envelope else None
            if parsed:
                self.checkpoint_hits += 1
                self._notice(f"{title}：命中检查点，跳过本次模型调用")
                self._record_reuse(
                    stage=stage,
                    title=title,
                    parts=parts,
                    system_instruction=system_instruction,
                    temperature=temperature,
                    parsed=parsed,
                    started=started,
                )
                return parsed

        self.checkpoint_misses += 1
        result = self._inner.generate_json(
            parts,
            system_instruction=system_instruction,
            temperature=temperature,
            stage=stage,
            title=title,
            max_attempts=max_attempts,
            max_output_tokens=max_output_tokens,
        )
        # 未收敛的响应不是可复用的结论，存了会把一次坏结果固化下来。
        if key and isinstance(result, dict) and result and not result.get("_unfinished"):
            try:
                self._store.put_json(
                    key,
                    wrap_payload(
                        result,
                        meta={
                            "provider": self._provider,
                            "model": model,
                            "stage": stage,
                            "title": title,
                        },
                    ),
                )
            except Exception:  # noqa: BLE001 - 写失败只是少一次复用，结果照常返回
                logger.warning("检查点写入异常，已跳过", exc_info=True)
        return result

    def checkpoint_stats(self) -> dict[str, Any]:
        return {
            "store": getattr(self._store, "kind", "unknown"),
            "hits": self.checkpoint_hits,
            "misses": self.checkpoint_misses,
        }


def checkpointed(
    inner: Any,
    *,
    provider: str,
    refresh: bool = False,
    on_event: Any | None = None,
) -> Any:
    """按环境决定是否套检查点；未启用时原样返回，调用方无需分支。"""
    from aigc_eval.judge.checkpoint_store import shared_checkpoint_store

    store = shared_checkpoint_store()
    if store is None:
        return inner
    return CheckpointedClient(
        inner, store=store, provider=provider, refresh=refresh, on_event=on_event
    )
