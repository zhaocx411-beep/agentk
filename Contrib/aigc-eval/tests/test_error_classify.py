"""平台错误分类的解析：钉住从 STAGING 采回来的真实样本。

为什么值得单独一个文件：`workflow._classify` 的输入不是我们自己的数据结构，而是
**平台错误文案**——一段没有 schema、随时可能变的字符串。判断"这次失败要不要重发"
全靠读它，读错的代价是真金白银：重发一次 `understand_media` 就是再付一次模型钱。

下面六个样本里前四个是 2026-08 对 STAGING 实调采回来的原文（服务身份
`klingai-agentk-aigc-eval-api`），一个字没改。它们共同暴露了一件必须防住的事：

    <msg> (type: InvalidArgument, retryable: false)
        : <msg> (type: permanentActivityError, retryable: true)

**同一个错误带两个互相矛盾的 retryable 标记**，而末尾那个恒为 true——四个样本里连
`permanentActivityError`（名字就叫"永久"）和 `wrapError` 都是 true。所以解析必须取
**第一个**。取最后一个（或者用 `in` 做子串匹配）会把每一个永久错误都重发一遍。

`docs/AGENTK接入自查.md` §3.1 记的"媒体类型不支持却被重试到超时"，机制正在这里。

直接 `uv run python tests/test_error_classify.py` 运行；不联网、不需要 Temporal。
"""

from __future__ import annotations

import os
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPO / "src"))
sys.path.insert(0, str(REPO / "worker"))

# 让 worker 侧的基线判定把这里当联调，别去连真 KESS。
os.environ.setdefault("KWS_LANE_ID", "dev/error-classify-test")

from workflow import _classify, _error_chain_text  # noqa: E402

FAILURES: list[str] = []


def check(label: str, ok: bool, detail: object = "") -> None:
    print(f"{'PASS' if ok else 'FAIL'}  {label}" + (f" — {detail}" if not ok else ""))
    if not ok:
        FAILURES.append(label)


# (标签, 错误原文, 期望分类, 期望是否重发)
SAMPLES: list[tuple[str, str, str, bool]] = [
    (
        "未知能力",
        'capability not found: resolve capability: capability catalog entry not found: '
        'capability "agentk.no_such_capability" (type: NotFound, retryable: false): '
        'capability catalog entry not found: capability "agentk.no_such_capability" '
        "(type: wrapError, retryable: true): capability catalog entry not found",
        "NotFound",
        False,
    ),
    (
        "generate_text 缺 model",
        "invalid capability invocation: capability generate_text: model and prompt are "
        "required (type: InvalidArgument, retryable: false): model and prompt are "
        "required (type: permanentActivityError, retryable: true): model and prompt "
        "are required",
        "InvalidArgument",
        False,
    ),
    (
        "generate_text 未知模型",
        'capability generate_text: text capability "not-a-real-model" is not available '
        '(type: Configuration, retryable: false): text capability "not-a-real-model" is '
        'not available (type: permanentActivityError, retryable: true): text capability '
        '"not-a-real-model" is not available',
        "Configuration",
        False,
    ),
    (
        "understand_media 空 mediaUrls",
        "invalid capability invocation: capability understand_media: mediaUrls is "
        "required (type: InvalidArgument, retryable: false): mediaUrls is required "
        "(type: permanentActivityError, retryable: true): mediaUrls is required",
        "InvalidArgument",
        False,
    ),
    # 以下两个不带平台标记：Temporal 自己的失败走兜底关键词。
    (
        "子 workflow 超时（无平台标记）",
        "child workflow execution failed: deadline exceeded",
        "deadline",
        True,
    ),
    (
        "彻底未知（无平台标记）",
        "something went wrong",
        "",
        False,
    ),
]


def main() -> int:
    print("\n[1] 真实样本逐条分类")
    for label, message, want_kind, want_retry in SAMPLES:
        kind, retry = _classify(Exception(message))
        check(f"{label}：type={want_kind or '<none>'}", kind == want_kind, kind)
        check(
            f"{label}：{'重发' if want_retry else '不重发'}",
            retry == want_retry,
            retry,
        )

    print("\n[2] 防回归：末尾那个 retryable: true 不能把判断带偏")
    # 这是最容易写错的一种实现：用 `in` 找 "retryable: true"。四个永久错误全都含它。
    permanent = [msg for label, msg, _, want in SAMPLES if not want and "type:" in msg]
    check(
        "四个永久错误的原文里确实都含 'retryable: true'（所以子串匹配必错）",
        all("retryable: true" in msg for msg in permanent) and len(permanent) == 4,
        len(permanent),
    )
    check(
        "但它们一个都没有被判成可重发",
        not any(_classify(Exception(msg))[1] for msg in permanent),
    )

    # 2026-09-08 线上实例：4a 参考音频对照那一段在报告里只显示
    # "Child Workflow execution failed"，平台到底说了什么完全看不到。
    # 原因是 invoke_capability 的失败被 Temporal 包成 ChildWorkflowError，
    # 而它自己的文案恒定，真实报错挂在 __cause__ 上（temporalio 的
    # converter/_failure_converter.py 在 from_failure 里按 failure.cause 逐层挂）。
    print("\n[3] 子 workflow 包壳：真实原因在 __cause__ 里，不能只读最外层")

    def wrapped(inner: str) -> Exception:
        """复现 Temporal 还原出来的形状：外壳文案恒定 + __cause__ 带平台原文。"""
        outer = Exception("Child Workflow execution failed")
        outer.__cause__ = Exception(inner)
        return outer

    platform_text = (
        "capability understand_media: media type not supported: audio/mpeg "
        "(type: InvalidArgument, retryable: false): media type not supported "
        "(type: permanentActivityError, retryable: true)"
    )
    wrapped_error = wrapped(platform_text)

    check(
        "只读最外层会拿到那句没有信息量的通用文案",
        str(wrapped_error) == "Child Workflow execution failed",
    )
    chain = _error_chain_text(wrapped_error)
    check("整条链里带上了平台原文", "media type not supported" in chain, chain[:80])
    check("外壳文案也保留（便于看出是子 workflow 失败）", "Child Workflow" in chain)
    kind, retry = _classify(wrapped_error)
    check("分类读到了平台标记而不是落进未知", kind == "InvalidArgument", kind)
    check("永久错误没有被判成可重发", retry is False, retry)

    # 反向：包壳里的可重试错误要能被认出来，否则真该重发的也不重发了。
    retryable_kind, retryable_flag = _classify(
        wrapped("capability understand_media: upstream unavailable "
                "(type: Unavailable, retryable: true)")
    )
    check("包壳里的可重试错误仍被认出", retryable_flag is True, retryable_flag)
    check("且分类正确", retryable_kind == "Unavailable", retryable_kind)

    print("\n[4] 链的边界：去重、深度上限、断链")
    same = Exception("同一句话")
    same.__cause__ = Exception("同一句话")
    check("重复文案只出现一次", _error_chain_text(same) == "同一句话")

    deep: Exception = Exception("L0")
    node = deep
    for i in range(1, 30):
        nxt = Exception(f"L{i}")
        node.__cause__ = nxt
        node = nxt
    text = _error_chain_text(deep)
    check("深度有上限，不会把 30 层全拼出来", text.count(" <- ") < 15, text.count(" <- "))
    check("最外层始终在", text.startswith("L0"))

    check("没有 cause 时退化成单层", _error_chain_text(Exception("孤立")) == "孤立")

    print()
    if FAILURES:
        print(f"FAILED: {len(FAILURES)} 项 — {FAILURES}")
        return 1
    print("ALL PASSED")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
