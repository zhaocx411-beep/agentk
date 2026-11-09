"""worker 入口的启动冒烟：真实 SDK + 本地 Temporal。

验的是三件只有真跑才能发现的事：
1. ``publish_workflow`` 的 Capability 声明能通过 SDK 校验（字段名、类型、
   Tool 的 max_attempts 都得对上 SDK 当前版本，写错只在启动时才炸）；
2. workflow 与 activity 能注册进 Temporal 并开始轮询；
3. capability announce 打不通时不会把 worker 拖死。

本地不存在核心侧的 agentk 队列，所以 announce 预期失败并重试——这正是要确认
「不致命」的那条路径。

需要机器上有 ``temporal server start-dev``；连不上就跳过（返回 0），与
test_worker_kill_resume.py / test_worker_stage_workflow.py 的口径一致。

    uv sync --extra worker --python 3.12
    PYTHONPATH=src uv run --no-sync python tests/test_worker_boot.py

## 这个测试为什么必须有硬性的进程级上限

它有过一段漏进程的历史：四天里攒了 6 个永不退出的孤儿进程，还把一次全量测试卡住
40 分钟。查下来**两处**都会挂，而且第二处才是主因：

1. 连不上本机 Temporal 时会退回 ``WorkflowEnvironment.start_time_skipping()``，
   它要从 GitHub 下载测试服务器二进制，网络受限时静默挂死——不报错、不超时。
   这个冒烟要验的三件事都不需要时间跳跃，所以那条兜底整个删掉了，改成连不上就跳过。
2. **worker 退场卡在 Temporal 运行时的线程回收上。** 测试已经打印「通过」和
   「收尾：已取消」，日志停在 ``Beginning worker shutdown`` 就再也不动（实测 3 分钟
   以上）。也就是说结论早就有了，进程却退不出去——光看输出还以为跑完了。

教训是：逐点加超时追不上，因为总会有下一处。所以这里改成给**整个进程**一个硬上限
（看门狗），并且在结论确定后直接 ``os._exit``，根本不进解释器收尾那一段。
往这个文件里加新的等待时，别指望它会自己结束。
"""

import asyncio
import os
import sys
import threading
import time
from pathlib import Path

WORKER_DIR = Path(__file__).resolve().parent.parent / "worker"
sys.path.insert(0, str(WORKER_DIR))

from temporalio.client import Client

BOOT_OBSERVE_SECONDS = 8

# 探活与收尾的超时。给得短是故意的：本机 Temporal 要么就在那儿、要么就没起，
# 不存在"再等等就好了"的中间状态。
CONNECT_TIMEOUT_SECONDS = 5.0
CLEANUP_TIMEOUT_SECONDS = 5.0

# 整个进程的硬上限。正常路径是「连接 ≤5s + 观察 8s + 收尾 ≤5s」，给到 60s 是留足
# 冷启动余量；真超过就是卡住了，宁可被强杀也不要再留孤儿进程。
#
# 可用环境变量压低，只为**验证看门狗本身**：压到 2s 跑一次，正常路径必然被它打断，
# 能看到它真的会开枪。没被验过的兜底最容易悄悄失效，而这个兜底失效的后果就是
# 当初那 6 个孤儿进程。
WATCHDOG_SECONDS = float(os.getenv("AIGC_EVAL_BOOT_WATCHDOG_SECONDS", "60"))

# 被看门狗打断时的退出码。用一个独特的值，好让 CI 里一眼看出「不是断言失败，
# 是卡住了」——这两种情况的排查方向完全不同。
WATCHDOG_EXIT_CODE = 3


def _hard_exit(code: int) -> None:
    """立即结束进程，跳过解释器收尾。

    正常的 ``SystemExit`` 会走 asyncio 与 Temporal 运行时的收尾，而那一段实测会卡死
    （见模块开头第 2 条）。测试结论此刻已经定了，没有任何理由为收尾把进程搭在那儿。
    """
    sys.stdout.flush()
    sys.stderr.flush()
    os._exit(code)


def _arm_watchdog() -> None:
    """起一个守护线程，到点强制退出。

    daemon 线程即使主线程卡在不可中断的调用里也照样跑，所以它兜得住"任何一处"卡住，
    包括解释器收尾阶段——那正是历史上漏进程的地方。
    """

    def bark() -> None:
        time.sleep(WATCHDOG_SECONDS)
        print(
            f"看门狗：整体超过 {WATCHDOG_SECONDS:.0f}s 仍未结束，强制退出"
            f"（退出码 {WATCHDOG_EXIT_CODE}）",
            flush=True,
        )
        _hard_exit(WATCHDOG_EXIT_CODE)

    threading.Thread(target=bark, name="boot-watchdog", daemon=True).start()


async def _reachable(address: str) -> bool:
    """本机 Temporal 是否可连。带超时，连不上就是连不上，不许挂着。"""
    try:
        client = await asyncio.wait_for(
            Client.connect(address), timeout=CONNECT_TIMEOUT_SECONDS
        )
    except TimeoutError:
        print(f"跳过：连 Temporal 超时（{address}，{CONNECT_TIMEOUT_SECONDS:.0f}s）。"
              "先跑 temporal server start-dev")
        return False
    except Exception as exc:  # noqa: BLE001 - 连不上的原因五花八门，都是同一个结论
        print(f"跳过：连不上 Temporal（{exc}）。先跑 temporal server start-dev")
        return False
    del client
    return True


async def main() -> int:
    address = os.getenv("TEMPORAL_ADDRESS", "127.0.0.1:7233").strip()
    print(f"连 Temporal：{address}")
    if not await _reachable(address):
        return 0

    namespace = os.getenv("TEMPORAL_NAMESPACE", "default").strip() or "default"
    os.environ["TEMPORAL_ADDRESS"] = address
    os.environ["TEMPORAL_NAMESPACE"] = namespace
    # worker 的基线判据是「有 KWS_SERVICE_STAGE、无 KWS_LANE_ID」，命中就会忽略
    # TEMPORAL_ADDRESS 改走 KESS。跑测试的人可能 source 过线上凭证（那会带上
    # stage），显式给个泳道把自己钉在非基线身份上，否则这个测试会去连真集群。
    os.environ.setdefault("KWS_LANE_ID", "dev/worker-boot-test")
    print(f"本地 Temporal: {address} / {namespace}")

    import worker as worker_module

    task = asyncio.create_task(worker_module.main())
    await asyncio.sleep(BOOT_OBSERVE_SECONDS)

    if task.done():
        exc = task.exception()
        print(f"worker 提前退出：{type(exc).__name__ if exc else '正常返回'}")
        if exc is not None:
            print(f"  {exc}")
            return 1
        print("worker 不该在无人停止时自行返回")
        return 1

    print(
        f"worker 启动 {BOOT_OBSERVE_SECONDS} 秒后仍在运行："
        "连接 + publish_workflow + 轮询均通过"
    )
    task.cancel()
    # 收尾也要有超时：worker 卡在退场逻辑里同样会留下孤儿进程，而那时测试
    # 其实已经通过了，不该为收尾把整个进程搭在这儿。
    try:
        await asyncio.wait_for(task, timeout=CLEANUP_TIMEOUT_SECONDS)
    except asyncio.CancelledError:
        print("收尾：已取消")
    except TimeoutError:
        print(f"收尾：取消后 {CLEANUP_TIMEOUT_SECONDS:.0f}s 未退出，不再等待")
    except Exception as exc:  # noqa: BLE001
        print(f"收尾：{type(exc).__name__}: {exc}")

    # 起过 worker 的这条路径必须硬退：Temporal 运行时的收尾会卡死。
    # 跳过那几条路径没起 worker，正常返回即可，留着让解释器干干净净地退出。
    _hard_exit(0)


if __name__ == "__main__":
    _arm_watchdog()
    raise SystemExit(asyncio.run(main()))
