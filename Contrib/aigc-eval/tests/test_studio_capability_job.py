"""钉住 studio 与 worker 之间那两处必须对齐的约定。

studio 现在只是 video_judge capability 的调用方，两边隔着 AgentK 的事件流与对象
存储，没有任何编译期检查能保证它们对同一份数据的理解一致。会悄悄漂的有两处：

1. **阶段事件的压扁与还原**。worker 的 ``_stage_progress`` 把事件压成
   ``(step_id, message, data)``，studio 的 ``event_from_progress`` 再拆回来。两个
   函数在两个文件里，改了一边不会有任何报错——表现是界面上阶段行的标题、状态或
   备注静默丢失，而质检本身完全正常。

2. **报告里的素材地址**。报告是 worker 生成的，里面的 slug 是 worker 算的；studio
   得把它换成自己的，否则页面上的视频是死链。

全程离线，不建会话、不调模型。
"""

from __future__ import annotations

import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parent
sys.path.insert(0, str(REPO / "worker"))
sys.path.insert(0, str(REPO / "src"))

from stage_activities import _stage_progress

from aigc_eval.server.capability_job import event_from_progress, localize

PASS, FAIL = 0, 0


def check(ok: bool, label: str) -> None:
    global PASS, FAIL
    if ok:
        PASS += 1
        print(f"PASS  {label}")
    else:
        FAIL += 1
        print(f"FAIL  {label}")


def roundtrip(event: dict) -> dict | None:
    """走一遍 worker 压扁 → studio 还原。"""
    progress = _stage_progress(event)
    if progress is None:
        return None
    return event_from_progress(
        progress["stepId"], progress["message"], progress.get("data")
    )


# 前端 renderStages() 真正读的字段，逐个都要活着到对岸。
FRONTEND_FIELDS = ("type", "id", "title", "status", "kind")


# KESS 依赖的 infra.util 在被导入时就 signal.signal() 注册 SIGTERM/SIGINT，而信号
# 只能在主线程注册。studio 的每次质检都跑在后台线程里，所以生产上（gRPC 通道）必须
# 由 warm_up_transport() 在主线程先把 kess 导进来。
#
# 这条断言只能在**全新解释器**里做：一旦本进程已经导过 kess，子线程再导就是空操作，
# 测试会假通过。第一版预热就是这么漏掉的——它只导了 agentk.client.grpc，而 kess 藏在
# new_controller() 函数体内的延迟导入里，看着合理，实际一点用没有。
_WARMUP_PROBE = """
import os, sys, threading
os.environ["KWS_SERVICE_STAGE"] = "PROD"      # 逼出 grpc 通道
os.environ.pop("AGENTK_BASE_URL", None)       # 有它就退回 http，预热会直接跳过
sys.path.insert(0, {src!r})

from aigc_eval.server.capability_job import warm_up_transport
warm_up_transport()

failure = []


def build_in_thread():
    try:
        from agentk.client import Options
        from agentk.client.grpc import create_client
        create_client(Options())
    except ValueError as error:
        if "signal only works in main thread" in str(error):
            failure.append(str(error))
    except Exception:
        pass  # KESS 连不通不是这条测试关心的事


thread = threading.Thread(target=build_in_thread)
thread.start()
thread.join()
print("SIGNAL_ERROR" if failure else "OK")
"""


def grpc_warmup_survives_thread() -> tuple[bool, str]:
    import subprocess

    done = subprocess.run(
        [sys.executable, "-c", _WARMUP_PROBE.format(src=str(REPO / "src"))],
        capture_output=True,
        text=True,
        timeout=120,
        check=False,
    )
    if "OK" in done.stdout:
        return True, "预热之后，子线程建 gRPC 客户端不再撞信号"
    if "SIGNAL_ERROR" in done.stdout:
        return False, "预热没盖住 kess 导入，子线程仍报 signal only works in main thread"
    return False, f"探针没跑起来：{(done.stderr or done.stdout).strip()[-200:]}"


def main() -> None:
    print("[1] 阶段事件压扁再还原，前端要读的字段一个不少")
    samples = [
        {
            "type": "stage",
            "id": "compile",
            "kind": "tool",
            "title": "编译判据",
            "status": "running",
        },
        {
            "type": "stage",
            "id": "scan",
            "kind": "model",
            "title": "原生扫描",
            "status": "done",
            "output": "发现 3 个疑似问题",
        },
        {
            "type": "stage",
            "id": "report",
            "kind": "tool",
            "title": "生成报告",
            "status": "error",
            "detail": "上传失败",
        },
    ]
    for sample in samples:
        back = roundtrip(sample)
        check(back is not None, f"{sample['id']}.{sample['status']} 能还原")
        if back is None:
            continue
        for field in FRONTEND_FIELDS:
            check(
                back.get(field) == sample.get(field),
                f"  {sample['id']}.{sample['status']} 的 {field} 一致"
                f"（{back.get(field)!r} vs {sample.get(field)!r}）",
            )
        for extra in ("input", "output", "detail"):
            if sample.get(extra):
                check(back.get(extra) == sample[extra], f"  备注字段 {extra} 保住了")

    print("\n[2] 阶段 id 里带点号也不能被截断")
    dotted = {
        "type": "stage",
        "id": "scan.pass2",
        "kind": "model",
        "title": "二次扫描",
        "status": "running",
    }
    back = roundtrip(dotted)
    check(back is not None and back["id"] == "scan.pass2", f"id 完整（{back and back['id']!r}）")

    print("\n[3] 非阶段事件不会被当成阶段混进来")
    check(roundtrip({"type": "usage", "tokens": 10}) is None, "usage 事件被忽略")

    print("\n[4] workflow 内部的模型调用进度不当成阶段")
    # 这些只有 running、永不 done，收下就是几行永久「运行中」的假阶段。
    for step in ("compile.model.1", "scan.model.3", "evidence.model.2"):
        check(
            event_from_progress(step, "第 1/2 次调用", None) is None,
            f"{step} 被丢弃",
        )
    check(
        event_from_progress("compile.running", "拆解提示词要求", {"status": "running"})
        is not None,
        "真阶段不受影响",
    )

    print("\n[5] 报告里的 worker slug 换成 studio 自己的")

    class FakeCase:
        slug = "studio_slug"

    report = {
        "case": {"slug": "worker_slug"},
        "media": {
            "video_url": "./api/upload_media?slug=worker_slug&asset=video",
            "contact_sheet_url": "./runtime/worker_slug__upload_contact.jpg",
            "reference_urls": [
                {"url": "./api/upload_media?slug=worker_slug&asset=reference&ref_id=1"}
            ],
        },
    }
    out = localize(report, FakeCase(), {"reportUrl": "https://blob/report.html"})
    blob = str(out["media"])
    check("worker_slug" not in blob, "media 里已无 worker 的 slug")
    check(blob.count("studio_slug") == 3, f"三处地址都换了（实际 {blob.count('studio_slug')}）")
    check(out["report_view_url"] == "https://blob/report.html", "HTML 报告指向对象存储那份")

    print("\n[6] slug 相同则原样不动")
    same = {"case": {"slug": "studio_slug"}, "media": {"video_url": "./api/x?slug=studio_slug"}}
    kept = localize(same, FakeCase(), {})
    check(kept["media"]["video_url"] == "./api/x?slug=studio_slug", "地址未被改写")
    check("report_view_url" not in kept, "没有 reportUrl 时不塞空链接")

    print("\n[7] 生产通道预热：子线程里建客户端不能撞上信号注册")
    check(*grpc_warmup_survives_thread())

    print()
    if FAIL:
        raise SystemExit(f"{FAIL} FAILED / {PASS} passed")
    print(f"ALL PASSED ({PASS})")


main()
