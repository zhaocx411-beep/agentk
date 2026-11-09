"""真的 SIGKILL 掉 worker 实例，看质检从哪一段接着跑。

这条测试回答的是把质检从一个 activity 拆成八个的**唯一理由**：实例中途没了，已经跑完
的阶段还算不算跑完了。

做法：
1. 起 worker 进程 A，发起一次质检；
2. 等它跑到 ``scan``（第四段）并挂在那里；
3. 对 A 发 SIGKILL——不是优雅退出，是断电式的消失，连收尾都不做；
4. 起 worker 进程 B，等 workflow 跑完；
5. 看两个进程各跑了哪些阶段。

期望：``prepare`` / ``compile`` / ``observe`` 只在 A 里跑过一次（它们的结果在 workflow
历史里，B 不会重跑），``scan`` 在 B 里重跑一次，其后的阶段都在 B 里跑完。

阶段正文是假的（毫秒级），因为要验的是编排的持久性，不是判定；判定的等价性由
``tests/test_worker_stages.py`` 用录制响应逐字段对完。

心跳超时被压到 3 秒（真值 5 分钟），否则要干等五分钟才判定失联。缩短的只是判定延迟。

前置：本机有 Temporal（``temporal server start-dev``）。
运行：``uv run python tests/test_worker_kill_resume.py``
"""

from __future__ import annotations

import asyncio
import json
import os
import signal
import subprocess
import sys
import time
import uuid
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPO / "src"))
sys.path.insert(0, str(REPO / "worker"))

os.environ.setdefault("KWS_LANE_ID", "dev/kill-resume-test")

from abi import WORKFLOW_TYPE, VideoJudgeInput, VideoJudgeOutput  # noqa: E402
from temporalio.client import Client  # noqa: E402
from temporalio.contrib.pydantic import pydantic_data_converter  # noqa: E402

FAILURES: list[str] = []
KILL_AT = "scan"


def check(label: str, ok: bool, detail: object = "") -> None:
    print(f"{'PASS' if ok else 'FAIL'}  {label}" + (f" — {detail}" if not ok else ""))
    if not ok:
        FAILURES.append(label)


def read_log(path: Path) -> list[dict]:
    if not path.exists():
        return []
    rows = []
    for line in path.read_text(encoding="utf-8").splitlines():
        try:
            rows.append(json.loads(line))
        except json.JSONDecodeError:  # 被 kill 时可能留下半行
            continue
    return rows


def spawn(queue: str, log: Path, slow_stage: str) -> subprocess.Popen:
    """起一个 worker 子进程。``slow_stage`` 指定哪一段挂住不返回。

    只有先起的那个进程需要挂住（给父进程留出杀它的窗口）；接手的进程传一个不存在的
    阶段名，让它一路跑完。
    """
    return subprocess.Popen(
        [sys.executable, str(Path(__file__).parent / "_stage_worker_proc.py"),
         queue, str(log), slow_stage],
        cwd=str(REPO),
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )


def wait_for(log: Path, predicate, timeout: float, what: str) -> list[dict]:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        rows = read_log(log)
        if predicate(rows):
            return rows
        time.sleep(0.2)
    raise TimeoutError(f"等 {what} 超时（{timeout}s）。日志：{read_log(log)}")


async def main() -> int:
    address = os.getenv("TEMPORAL_ADDRESS", "127.0.0.1:7233").strip()
    print(f"连 Temporal：{address}")
    try:
        client = await Client.connect(address, data_converter=pydantic_data_converter)
    except Exception as exc:  # noqa: BLE001
        print(f"跳过：连不上 Temporal（{exc}）。先跑 temporal server start-dev")
        return 0

    queue = f"test-kill-{uuid.uuid4().hex[:8]}"
    log = REPO / "runtime" / f"kill_resume_{uuid.uuid4().hex[:8]}.jsonl"
    log.parent.mkdir(parents=True, exist_ok=True)
    proc_a = spawn(queue, log, KILL_AT)
    proc_b: subprocess.Popen | None = None
    try:
        print("\n[1] 进程 A 就绪并跑到 scan")
        wait_for(log, lambda rows: any(r["note"] == "ready" for r in rows), 30, "A 就绪")
        handle = await client.start_workflow(
            WORKFLOW_TYPE,
            VideoJudgeInput(videoUrl="https://example.invalid/a.mp4", prompt="测试"),
            id=f"test-kill-wf-{uuid.uuid4().hex[:8]}",
            task_queue=queue,
            # 按名字发起时 SDK 不知道回参类型，不声明会拿到裸 dict。
            result_type=VideoJudgeOutput,
        )
        wait_for(
            log,
            lambda rows: any(
                r["stage"] == KILL_AT and r["note"] == "stalling" for r in rows
            ),
            60,
            f"跑到 {KILL_AT}",
        )
        before = read_log(log)
        stages_a = [
            r["stage"] for r in before if r["stage"] not in {"__worker__", "__model__"}
        ]
        # 含模型调用的阶段每段跑两趟：第一趟交出请求，第二趟带着响应回来。
        check(
            f"A 按预期跑到了 {KILL_AT}",
            stages_a[:7]
            == ["prepare", "compile", "compile", "observe", "observe", "scan", "scan"],
            stages_a,
        )
        check("A 还没跑后面的阶段", "recheck" not in stages_a, stages_a)
        calls_a = [r for r in before if r["stage"] == "__model__"]
        check(
            "A 被杀前已经发生 3 次模型调用（compile/observe/scan）",
            len(calls_a) == 3,
            [r["note"] for r in calls_a],
        )

        print("\n[2] 对 A 发 SIGKILL（断电式消失）")
        proc_a.send_signal(signal.SIGKILL)
        proc_a.wait(timeout=10)
        check("A 已死", proc_a.poll() is not None, proc_a.poll())

        print("\n[3] 起进程 B，等 workflow 跑完")
        proc_b = spawn(queue, log, "__none__")
        result = await asyncio.wait_for(handle.result(), timeout=180)
        check("workflow 完成", result.analysisStatus == "ok")

        print("\n[4] 看谁跑了哪些阶段")
        rows = [r for r in read_log(log) if r["stage"] != "__worker__"]
        pid_a, pid_b = proc_a.pid, proc_b.pid
        by_stage: dict[str, list[dict]] = {}
        for row in rows:
            if row["note"] == "stalling":
                continue
            by_stage.setdefault(row["stage"], []).append(row)

        check(
            "prepare 只跑过一次，且在 A 里",
            len(by_stage.get("prepare") or []) == 1
            and by_stage["prepare"][0]["pid"] == pid_a,
            [(r["pid"], r["attempt"]) for r in by_stage.get("prepare") or []],
        )
        for stage in ("compile", "observe"):
            runs = by_stage.get(stage) or []
            check(
                f"{stage} 跑了两趟（请求 + 消费），都在 A 里",
                len(runs) == 2 and all(r["pid"] == pid_a for r in runs),
                [(r["pid"], r["attempt"]) for r in runs],
            )
        scan_runs = by_stage.get(KILL_AT) or []
        check(
            f"{KILL_AT} 跑了三趟：A 请求、A 消费（被杀）、B 消费",
            len(scan_runs) == 3
            and [r["pid"] for r in scan_runs] == [pid_a, pid_a, pid_b],
            [(r["pid"], r["attempt"]) for r in scan_runs],
        )
        check(
            f"{KILL_AT} 接手那趟是 attempt=2（同一个 activity task 的重试）",
            len(scan_runs) == 3 and scan_runs[2]["attempt"] == 2,
            [r["attempt"] for r in scan_runs],
        )
        for stage in ("recheck", "evidence"):
            runs = by_stage.get(stage) or []
            check(
                f"{stage} 跑了两趟，都在 B 里",
                len(runs) == 2 and all(r["pid"] == pid_b for r in runs),
                [(r["pid"], r["attempt"]) for r in runs],
            )
        for stage in ("aggregate", "report"):
            runs = by_stage.get(stage) or []
            check(
                f"{stage} 只跑过一次，且在 B 里",
                len(runs) == 1 and runs[0]["pid"] == pid_b,
                [(r["pid"], r["attempt"]) for r in runs],
            )

        # 这条是整次改造的核心收益：scan 的 activity 被杀后重跑了一趟，但它那次模型
        # 调用**不能**跟着重来——响应在 workflow 历史里，重放时必须被复用。
        calls = [r for r in rows if r["stage"] == "__model__"]
        check(
            "模型调用总数是 5（每个模型阶段一次），没有因为重跑而多付",
            len(calls) == 5,
            [r["note"] for r in calls],
        )
        by_call: dict[str, int] = {}
        for row in calls:
            by_call[row["note"]] = by_call.get(row["note"], 0) + 1
        check(
            f"{KILL_AT} 的模型调用只发生过一次（重跑没有重复调用）",
            by_call.get(KILL_AT) == 1,
            by_call,
        )
    finally:
        for proc in (proc_a, proc_b):
            if proc is not None and proc.poll() is None:
                proc.send_signal(signal.SIGKILL)
                proc.wait(timeout=10)
        log.unlink(missing_ok=True)

    print()
    if FAILURES:
        print(f"FAILED: {len(FAILURES)} 项 — {FAILURES}")
        return 1
    print("ALL PASSED")
    return 0


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
