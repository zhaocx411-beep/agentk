"""数一次质检真正发起了多少次模型调用。

## 为什么必须查 Temporal 历史，而不是数日志

worker 日志不能用：activity 与 workflow 在 worker 重启后会**重放**，代码被重新执行
一遍，日志行也就重复打印。所以日志里的"调用了 N 次"包含了重放的假象。

Temporal 历史里的 ``StartChildWorkflowExecutionInitiated`` 只在**真正**发起子 workflow
时写入一条，重放不会重复写。``invoke_capability`` 底层就是
``workflow.execute_child_workflow``，因此这个事件数就是真实的模型调用次数。

这正是「杀 worker 重启后不重复付费」这条收益唯一站得住的证据。

用法（需要 kess-agent 在跑，用于服务发现）：

    uv run --no-sync python worker/count_model_calls.py            # 最近一条
    uv run --no-sync python worker/count_model_calls.py --limit 5  # 最近五条
"""

from __future__ import annotations

import argparse
import asyncio
import collections
import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

from abi import WORKFLOW_TYPE  # noqa: E402

# event_type 在当前 temporalio 版本里是裸 int，没有 .name。必须用枚举做映射，
# 不能靠字符串猜——猜错的表现是所有计数都是 0，看起来像"一次调用都没发生"。
_CHILD_STARTED = "EVENT_TYPE_START_CHILD_WORKFLOW_EXECUTION_INITIATED"
_ACTIVITY_SCHEDULED = "EVENT_TYPE_ACTIVITY_TASK_SCHEDULED"
_TASK_STARTED = "EVENT_TYPE_WORKFLOW_TASK_STARTED"
_SIGNALED = "EVENT_TYPE_WORKFLOW_EXECUTION_SIGNALED"


async def _run(opts: argparse.Namespace) -> int:
    from agentk.worker import connect_temporal

    client = await connect_temporal()

    executions = []
    query = f"WorkflowType = '{WORKFLOW_TYPE}'"
    async for desc in client.list_workflows(query=query):
        executions.append(desc)
        if len(executions) >= opts.limit:
            break

    if not executions:
        print(f"没找到 {WORKFLOW_TYPE} 的执行记录。", file=sys.stderr)
        return 1

    for desc in executions:
        await _report_one(client, desc)
    return 0


async def _report_one(client, desc) -> None:
    from temporalio.api.enums.v1 import EventType

    handle = client.get_workflow_handle(desc.id, run_id=desc.run_id)
    kinds: collections.Counter[str] = collections.Counter()
    children: list[str] = []

    async for event in handle.fetch_history_events():
        name = EventType.Name(int(event.event_type))
        kinds[name] += 1
        if name == _CHILD_STARTED:
            attrs = event.start_child_workflow_execution_initiated_event_attributes
            children.append(attrs.workflow_type.name)

    print(f"\n{'=' * 70}")
    print(f"workflow  {desc.id}")
    print(f"run       {desc.run_id}")
    print(f"状态      {desc.status.name}   开始 {desc.start_time:%Y-%m-%d %H:%M:%S}")
    print(f"{'-' * 70}")
    print(f"真实模型调用（子 workflow 发起数）  {len(children)}")
    for capability, count in collections.Counter(children).most_common():
        print(f"      {capability:<42} {count}")
    print(f"activity 调度次数                  {kinds[_ACTIVITY_SCHEDULED]}")
    print(f"进度信号数                         {kinds[_SIGNALED]}")
    # worker 每次接管都要处理 workflow task；重启后重放会让这个数明显变大，
    # 而上面的「真实模型调用」不该跟着变大——这正是要验证的那件事。
    print(f"workflow task 次数（含重放接管）    {kinds[_TASK_STARTED]}")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--limit", type=int, default=1, help="看最近几条执行")
    opts = parser.parse_args()
    raise SystemExit(asyncio.run(_run(opts)))


if __name__ == "__main__":
    main()
