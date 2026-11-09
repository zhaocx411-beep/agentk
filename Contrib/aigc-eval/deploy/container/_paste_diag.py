import asyncio
import sys
from datetime import datetime, timezone

sys.path.insert(0, "/home/web_server/aigc-eval/bin")
try:
    from container_env import announce_identity

    announce_identity()
except Exception as e:
    print("借身份失败:", e)

from agentk.worker import connect_temporal
from temporalio.contrib.pydantic import pydantic_data_converter


def ago(t):
    if not t:
        return "?"
    if not isinstance(t, datetime):
        t = t.ToDatetime(tzinfo=timezone.utc)
    if t.tzinfo is None:
        t = t.replace(tzinfo=timezone.utc)
    return f"{(datetime.now(timezone.utc) - t).total_seconds():.0f}s前"


def ev_name(ev):
    try:
        from temporalio.api.enums.v1 import EventType

        return EventType.Name(ev.event_type).replace("EVENT_TYPE_", "")
    except Exception:
        return str(ev.event_type)


async def main():
    c = await connect_temporal(data_converter=pydantic_data_converter)
    print(f"namespace={c.namespace}\n")
    n = 0
    async for wf in c.list_workflows('ExecutionStatus="Running"'):
        n += 1
        h = c.get_workflow_handle(wf.id, run_id=wf.run_id)
        print(f"=== {wf.id}")
        print(f"    type={wf.workflow_type}  启动于 {ago(wf.start_time)}")
        d = (await h.describe()).raw_description

        if not d.pending_activities:
            print("    无 pending activity ⇒ 正在等子 workflow（即模型调用）")
        for a in d.pending_activities:
            print(f"    activity={a.activity_type.name} state={a.state} 第{a.attempt}次尝试")
            if a.HasField("scheduled_time"):
                print(f"      排入队列 {ago(a.scheduled_time)}")
            if a.HasField("last_started_time"):
                print(f"      开始执行 {ago(a.last_started_time)}")
            if a.HasField("last_heartbeat_time"):
                print(f"      最后心跳 {ago(a.last_heartbeat_time)}")
            if a.last_failure.message:
                print(f"      上次失败: {a.last_failure.message[:300]}")

        for ch in d.pending_children:
            print(f"    子workflow={ch.workflow_type_name} id={ch.workflow_id}")

        evs = [ev async for ev in h.fetch_history_events()]
        print(f"    最近事件（共{len(evs)}条）:")
        for ev in evs[-14:]:
            print(f"      {ago(ev.event_time):>9}  {ev_name(ev)}")
        print()

    if not n:
        print("没有 Running 的 workflow ⇒ 页面上的“运行中”是前端状态没刷新")


asyncio.run(main())
