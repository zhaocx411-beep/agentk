"""重试语义：确定性失败只跑一次，基础设施丢失才重跑。

为什么值得单独一个测试：workflow 的 ``RetryPolicy(maximum_attempts=2)`` 那次重试是
**留给进程/容器丢失**的（worker 崩溃、被驱逐、容器重启，心跳超时后 Temporal 重新
派发，模型调用级检查点让重跑跳过已完成的部分）。而代码 bug、契约校验失败这类确定性
错误必须一次就交回调用方——重跑只会再失败一次，白等一轮，还让调用方晚拿到原因。

拿第一个阶段（``prepare``）当靶子：重试策略对八个阶段是同一份，验一个就够，而
prepare 是唯一不需要前置状态的阶段，替成假实现最省事。"某一段重跑时前面的段不重跑"
是另一回事，由 ``tests/test_worker_stage_workflow.py`` 与
``tests/test_worker_kill_resume.py`` 负责。

这两种语义靠 activity 侧"把应用层异常转成 non_retryable 的 ApplicationError"来分开。
一旦那层转换漏了，症状极其隐蔽：一次注定失败的质检会悄悄跑两遍，调用方等两倍时间才
看到同一个错误；而如果反过来把重试整个关掉，崩溃就变成硬失败，检查点白做。所以两个
方向都要钉住：

1. 应用层异常（这里用 ValueError 代表代码 bug）→ activity 只执行 **1** 次；
2. 显式可重试的失败 → activity 执行 **2** 次（证明重试确实是开着的）。

真跑 Temporal，不 mock 重试逻辑——重试是服务端行为，mock 掉就什么都没验到。

    TEMPORAL_ADDRESS=127.0.0.1:7233 \
        PYTHONPATH=src uv run --no-sync python tests/test_worker_retry.py
"""

from __future__ import annotations

import asyncio
import os
import sys
import uuid
from pathlib import Path

WORKER_DIR = Path(__file__).resolve().parent.parent / "worker"
sys.path.insert(0, str(WORKER_DIR))
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

from temporalio import activity  # noqa: E402
from temporalio.client import Client, WorkflowFailureError  # noqa: E402
from temporalio.exceptions import ApplicationError  # noqa: E402
from temporalio.worker import Worker  # noqa: E402

from abi import STAGE_ACTIVITIES, VideoJudgeInput  # noqa: E402
from workflow import AigcEvalVideoJudge  # noqa: E402

FAILURES: list[str] = []


def check(label: str, ok: bool, detail: object = "") -> None:
    print(f"{'PASS' if ok else 'FAIL'}  {label}" + (f" — {detail}" if not ok else ""))
    if not ok:
        FAILURES.append(label)


ATTEMPTS: list[str] = []


@activity.defn(name=STAGE_ACTIVITIES["prepare"])
async def deterministic_failure(args: VideoJudgeInput) -> dict:
    """模拟代码 bug：activity 侧会把它转成 non_retryable。

    这里直接抛转换后的形态，因为真 activity 的转换发生在它自己的 except 里，
    而本测试要验的是「转换后的异常在服务端确实不被重试」这一段。
    """
    ATTEMPTS.append("deterministic")
    raise ApplicationError("native8 contract failed", type="ValueError", non_retryable=True)


@activity.defn(name=STAGE_ACTIVITIES["prepare"])
async def retryable_failure(args: VideoJudgeInput) -> dict:
    """模拟基础设施丢失：可重试，用来证明重试没被关掉。"""
    ATTEMPTS.append("retryable")
    raise ApplicationError("worker 丢失（模拟）", type="WorkerLost")


async def run_once(client: Client, queue: str, activity_fn) -> str:
    ATTEMPTS.clear()
    async with Worker(
        client,
        task_queue=queue,
        workflows=[AigcEvalVideoJudge],
        activities=[activity_fn],
    ):
        try:
            await client.execute_workflow(
                AigcEvalVideoJudge.run,
                VideoJudgeInput(videoUrl="https://example.invalid/v.mp4", prompt="测试"),
                id=f"retry-test-{uuid.uuid4().hex[:8]}",
                task_queue=queue,
            )
            return "没有失败（预期失败）"
        except WorkflowFailureError as err:
            return str(err.cause)[:80]


async def main() -> int:
    address = os.getenv("TEMPORAL_ADDRESS", "").strip()
    if not address:
        print("跳过：未设 TEMPORAL_ADDRESS（需要 temporal server start-dev）")
        return 0
    namespace = os.getenv("TEMPORAL_NAMESPACE", "default").strip() or "default"
    client = await Client.connect(address, namespace=namespace)
    queue = f"retry-test-{uuid.uuid4().hex[:8]}"
    print(f"Temporal {address}/{namespace}，队列 {queue}\n")

    print("[1] 应用层异常（代码 bug / 契约失败）只执行一次")
    detail = await run_once(client, queue, deterministic_failure)
    check("activity 只被执行 1 次", len(ATTEMPTS) == 1, f"{len(ATTEMPTS)} 次")
    print(f"      调用方拿到：{detail}")

    print("\n[2] 可重试失败会重跑一次（证明重试是开着的）")
    detail = await run_once(client, queue, retryable_failure)
    check("activity 被执行 2 次", len(ATTEMPTS) == 2, f"{len(ATTEMPTS)} 次")
    check("不超过 maximum_attempts=2", len(ATTEMPTS) <= 2, f"{len(ATTEMPTS)} 次")
    print(f"      调用方拿到：{detail}")

    if FAILURES:
        print(f"\nFAILED: {len(FAILURES)} 项 — {FAILURES}")
        return 1
    print("\nALL PASSED")
    return 0


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
