"""数一次质检 run 里真正发生了多少次模型调用。

判据取 Temporal 历史里的 ChildWorkflowExecutionStarted：``invoke_capability``
底层就是 ``execute_child_workflow``，一次模型调用对应一个子 workflow。这比看
进度事件可靠——进度事件在阶段重跑时会重复发出（workflow 侧按 stepId 去重），
数它只能证明去重生效，证明不了"没有重复付费"。

用法：uv run --no-sync python count_model_calls.py <workflow_id>
"""

import asyncio
import sys
from collections import Counter

from agentk.worker import connect_temporal


async def main(workflow_id: str) -> None:
    client = await connect_temporal()
    handle = client.get_workflow_handle(workflow_id)

    kinds: Counter[str] = Counter()
    children: list[str] = []
    activities: Counter[str] = Counter()

    async for event in handle.fetch_history_events():
        name = event.DESCRIPTOR.fields_by_name  # noqa: F841 - 仅为可读性保留
        kind = event.WhichOneof("attributes") or "?"
        kinds[kind] += 1
        if kind == "child_workflow_execution_started_event_attributes":
            attrs = event.child_workflow_execution_started_event_attributes
            children.append(attrs.workflow_type.name)
        elif kind == "activity_task_started_event_attributes":
            pass
        elif kind == "activity_task_scheduled_event_attributes":
            attrs = event.activity_task_scheduled_event_attributes
            activities[attrs.activity_type.name] += 1

    print(f"workflow: {workflow_id}")
    print(f"\n子 workflow（= invoke_capability 次数）：{len(children)}")
    for name, count in Counter(children).most_common():
        print(f"  {count:3d}  {name}")
    print(f"\nactivity 调度次数：{sum(activities.values())}")
    for name, count in activities.most_common():
        print(f"  {count:3d}  {name}")

    print("\n全部事件类型：")
    for kind, count in kinds.most_common():
        mark = (
            "  <<<"
            if any(w in kind for w in ("timed_out", "failed", "terminated", "cancel"))
            else ""
        )
        print(f"  {count:4d}  {kind.removesuffix('_event_attributes')}{mark}")


asyncio.run(main(sys.argv[1]))
