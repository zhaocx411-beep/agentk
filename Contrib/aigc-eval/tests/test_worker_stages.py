"""worker 阶段化路径与单体路径的等价性测试。

拆成八个 Temporal activity 之后，同一份素材有了两条执行路径：

* **单体**：``analyze_case`` 一口气跑完（批处理脚本走这条，在自己进程里直接调）；
* **阶段化**：八段各自一个 activity，状态经 JSON 在段间传递（worker 走这条）。

两条路径必须给出**同一份报告**。否则同一个视频在页面上和在平台调用里会得到不同结论，
而这种分歧极难发现——两边各自看都"正常"。

所以这里不比对存档基线，而是在同一个进程里把两条路径都跑一遍直接对比。比对项：

1. 报告逐字段一致（剔除 run_id、时间戳这类必然不同的字段）；
2. 前端看到的阶段事件序列一致（id + status 逐条相同）——进度流也是对外契约；
3. trace 是一条完整链：阶段化路径的步数与单体一致，run_id 全程不变；
4. 用量是整次质检的合计，不是最后一段的；
5. 状态每段都能 JSON 往返（真实 activity 边界就是这么传的）。

模型响应来自 ``runtime/traces`` 的真实录制，所以不联网、不花钱。
直接 ``uv run python tests/test_worker_stages.py`` 运行。
"""

from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Any

REPO = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPO / "src"))
sys.path.insert(0, str(REPO / "worker"))
sys.path.insert(0, str(Path(__file__).resolve().parent))

from baseline_pipeline import (  # noqa: E402
    REPLAY_CASE,
    ReplayClient,
    ReplayConfig,
    load_recorded_stages,
    recorded_responder,
    replay_prompt_text,
    strip_volatile,
)

FAILURES: list[str] = []


def check(label: str, ok: bool, detail: object = "") -> None:
    print(f"{'PASS' if ok else 'FAIL'}  {label}" + (f" — {detail}" if not ok else ""))
    if not ok:
        FAILURES.append(label)


def _replay_patch(stages: dict[str, Any], model: str) -> tuple[Any, Any]:
    """把 agentk 通道换成回放替身，返回原始表以便复原。"""
    import aigc_eval.judge.pipeline as jp

    def factory(config: Any, *, trace: Any = None, on_event: Any = None) -> ReplayClient:
        client = ReplayClient(config, trace=trace, on_event=on_event)
        client.stages = stages
        return client

    saved = (dict(jp._PROVIDER_CLIENTS), dict(jp._PROVIDER_CONFIGS))
    jp._PROVIDER_CLIENTS["agentk"] = factory  # type: ignore[assignment]
    jp._PROVIDER_CONFIGS["agentk"] = lambda: ReplayConfig(model)  # type: ignore[assignment]
    return saved


def _restore(saved: tuple[Any, Any]) -> None:
    import aigc_eval.judge.pipeline as jp

    clients, configs = saved
    jp._PROVIDER_CLIENTS.clear()
    jp._PROVIDER_CLIENTS.update(clients)
    jp._PROVIDER_CONFIGS.clear()
    jp._PROVIDER_CONFIGS.update(configs)


def _case_input() -> Any:
    """把回放素材包成 worker 的入参。

    ``videoUrl`` 用本地绝对路径——``media_intake`` 明确支持本机调试时传路径，
    这样就不必为测试起一个 HTTP 服务。
    """
    from abi import VideoJudgeInput

    manifest = json.loads((REPLAY_CASE / "manifest.json").read_text(encoding="utf-8"))
    return VideoJudgeInput(
        videoUrl=str(manifest["video"]),
        prompt=replay_prompt_text(),
        referenceImageUrls=[
            str(ref["path"])
            for ref in manifest.get("references") or []
            if ref.get("kind", "image") == "image"
        ],
        provider="agentk",
        refresh=True,  # 必须绕开整份报告缓存，否则测的是缓存不是编排
    )


def run_monolithic(args: Any, events: list[dict[str, Any]]) -> dict[str, Any]:
    """单体路径。source 取 worker 侧的 "upload"，否则两条路径的媒体链接不可比。"""
    from aigc_eval.dataset.upload_case import build_upload_case_files
    from aigc_eval.judge.pipeline import analyze_case

    references = [{"path": path, "kind": "image"} for path in args.referenceImageUrls]
    case_files = build_upload_case_files(
        prompt=args.prompt, video_path=Path(args.videoUrl), references=references
    )
    return analyze_case(
        case_files.case_id,
        case_files=case_files,
        use_vlm=True,
        provider="agentk",
        allow_fallback=False,
        source="upload",
        refresh=True,
        on_event=events.append,
    )


def _drive_model_stage(
    step: Any,
    state: dict[str, Any],
    events: list[dict[str, Any]],
    respond: Any,
) -> dict[str, Any]:
    """在进程内模拟 workflow 的 build / invoke / consume 驱动循环。

    与 ``worker/workflow.py`` 的 ``_model_stage`` 形状一致：阶段正文跑到需要模型时抛
    ``NeedsModelCall``，这里"调一次"（回放录制响应），把响应按 digest 攒起来，再跑一遍。
    阶段正文从头重放，走到同一个点时响应已经在手里，于是继续往下。

    ``NeedsModelCall`` 继承 BaseException，所以 judge 侧那些给模型失败做降级的
    ``except Exception`` 不会把它吞掉——这里用裸 except 名字接，接不到就说明继承关系
    被改回 Exception 了，测试会以"重放未收敛"的形式报出来。
    """
    from aigc_eval.providers.model_call import NeedsModelCall
    from abi import MAX_MODEL_ROUNDS

    responses: dict[str, list[dict[str, Any]]] = {}
    for _ in range(MAX_MODEL_ROUNDS):
        try:
            return step(state, events.append, responses)
        except NeedsModelCall as pending:
            request = pending.request
            # 响应过一次 JSON 往返：真实路径上它要经 Temporal payload。
            raw = json.loads(json.dumps(respond(request), ensure_ascii=False))
            responses.setdefault(request.digest, []).append(raw)
    raise SystemExit("模型阶段重放未收敛：请求 digest 每轮都在变？")


def run_staged(
    args: Any, events: list[dict[str, Any]], respond: Any
) -> dict[str, Any]:
    """阶段化路径：逐段调 activity 正文，段间把状态 JSON 往返一次。

    JSON 往返不是形式主义——真实 activity 边界就是这么传的。任何一段往状态里塞了
    Path 或其他非 JSON 值，这里会立刻炸掉，而不是等到线上某个阶段静默丢字段。

    含模型调用的五个阶段走驱动循环，其余三个（prepare / aggregate / report）一次跑完。
    这个分工与 ``abi.MODEL_STAGES`` 必须一致，否则测的就不是线上那条路径。
    """
    import stage_activities as sa
    from abi import MODEL_STAGES

    state = sa._do_prepare(args, events.append, "test-run-key")
    state = json.loads(json.dumps(state, ensure_ascii=False))
    for stage, step in (
        ("compile", sa._do_compile),
        ("observe", sa._do_observe),
        ("scan", sa._do_scan),
        ("recheck", sa._do_recheck),
        ("aggregate", sa._do_aggregate),
        ("evidence", sa._do_evidence),
        ("report", sa._do_report),
    ):
        if stage in MODEL_STAGES:
            state = _drive_model_stage(step, state, events, respond)
        else:
            state = step(state, events.append)
        state = json.loads(json.dumps(state, ensure_ascii=False))
    return state


def stage_stream(events: list[dict[str, Any]]) -> list[tuple[str, str]]:
    """调用方看到的阶段事件序列。只取 id + status：文案里带耗时之类的易变内容。

    **按 (id, status) 去重**，因为这正是 workflow 对外转发时做的事
    （``workflow.py`` 的 ``_forward`` 用 ``f"{step}.{status}"`` 记 ``_seen_steps``）。

    去重是重放的必要配套：含模型调用的阶段会被重跑数遍，每遍都从阶段开头重新 emit，
    所以 activity 侧的原始事件流里同一步会出现好几次。AgentK 本身也用 step_id 做幂等。
    这里对齐到"调用方看到的"那一层，才是在比对对外契约；比原始流等于在比实现细节。
    """
    seen: set[tuple[str, str]] = set()
    stream: list[tuple[str, str]] = []
    for event in events:
        if event.get("type") != "stage":
            continue
        key = (str(event.get("id")), str(event.get("status")))
        if key in seen:
            continue
        seen.add(key)
        stream.append(key)
    return stream


def first_diff(a: Any, b: Any, path: str = "") -> str | None:
    if isinstance(a, dict) and isinstance(b, dict):
        for key in sorted(set(a) | set(b)):
            if key not in a:
                return f"{path}.{key}（阶段化多出）"
            if key not in b:
                return f"{path}.{key}（阶段化缺失）"
            found = first_diff(a[key], b[key], f"{path}.{key}")
            if found:
                return found
        return None
    if isinstance(a, list) and isinstance(b, list):
        if len(a) != len(b):
            return f"{path} 长度 {len(a)} → {len(b)}"
        for index, (x, y) in enumerate(zip(a, b)):
            found = first_diff(x, y, f"{path}[{index}]")
            if found:
                return found
        return None
    return None if a == b else f"{path}: {str(a)[:70]} → {str(b)[:70]}"


def main() -> int:
    if not (REPLAY_CASE / "manifest.json").exists():
        raise SystemExit(f"缺回放素材：{REPLAY_CASE}")
    stages, model = load_recorded_stages()
    args = _case_input()

    # 阶段化路径会把素材复制进 worker 的 staging 目录，再按同一个内容哈希重写
    # 回放 case 的 manifest（视频路径指向 staging）。回放素材是别的测试共用的固定
    # 装置，跑完必须还原，否则下次回放会指到一个临时目录。
    fixture = {
        path: path.read_bytes()
        for path in (REPLAY_CASE / "manifest.json", REPLAY_CASE / "prompt.json")
    }

    print("\n[1] 两条路径各跑一遍")
    saved = _replay_patch(stages, model)
    try:
        mono_events: list[dict[str, Any]] = []
        mono = run_monolithic(args, mono_events)
        staged_events: list[dict[str, Any]] = []
        staged_state = run_staged(args, staged_events, recorded_responder(stages, model))
    finally:
        _restore(saved)
        for path, body in fixture.items():
            path.write_bytes(body)
    staged = staged_state["report"]
    check("单体路径跑完", bool(mono.get("schema_version")))
    check("阶段化路径跑完", bool(staged.get("schema_version")))

    print("\n[2] 报告逐字段一致")
    # 产物清单只在启用对象存储时出现，且内容是"这次拉了几个文件"，与判定无关。
    left = strip_volatile({k: v for k, v in mono.items() if k != "artifacts"})
    right = strip_volatile({k: v for k, v in staged.items() if k != "artifacts"})
    diff = first_diff(left, right)
    check("报告一致", diff is None, diff)
    for key in ("analysis_status", "issues", "rubric", "evidence", "review_funnel"):
        check(
            f"  {key} 一致",
            strip_volatile(mono.get(key)) == strip_volatile(staged.get(key)),
        )

    print("\n[3] 前端看到的阶段事件序列一致")
    mono_stream, staged_stream = stage_stream(mono_events), stage_stream(staged_events)
    # 音频复听必须真的在跑。它曾因回放素材缺 audio 阶段而被显式关掉，配置与线上默认
    # （settings.AUDIO_RECHECK 开）不一致；钉死这条，免得哪天配置一漂又安静地退回去。
    check(
        "4c 音频复听在两条路径上都跑了",
        any(step == "audio" for step, _ in mono_stream)
        and any(step == "audio" for step, _ in staged_stream),
        f"mono={[s for s, _ in mono_stream]}",
    )
    same = mono_stream == staged_stream
    check(f"事件序列一致（{len(mono_stream)} 条）", same)
    if not same:
        for index, (x, y) in enumerate(zip(mono_stream, staged_stream)):
            if x != y:
                check(f"  首处差异在第 {index} 条", False, f"{x} → {y}")
                break

    print("\n[4] trace 是一条完整链")
    mono_trace = mono.get("llm_trace") or {}
    staged_trace = staged.get("llm_trace") or {}
    check(
        "步数一致",
        mono_trace.get("step_count") == staged_trace.get("step_count"),
        f"{mono_trace.get('step_count')} → {staged_trace.get('step_count')}",
    )
    run_ids = {str(step.get("model") or "") for step in staged_trace.get("steps") or []}
    check("阶段化 trace 非空", bool(staged_trace.get("steps")), run_ids)
    check(
        "run_id 全程未变",
        staged_trace.get("run_id") == staged_state["trace"]["run_id"],
    )

    print("\n[5] 用量与产物是整次质检的合计")
    check(
        "用量累计了各阶段",
        len(staged_state.get("usage") or []) == len(mono.get("cost", {}).get("calls_detail") or [])
        or len(staged_state.get("usage") or []) >= 5,
        len(staged_state.get("usage") or []),
    )
    check(
        "成本调用数与单体一致",
        (mono.get("cost") or {}).get("calls") == (staged.get("cost") or {}).get("calls"),
        f"{(mono.get('cost') or {}).get('calls')} → {(staged.get('cost') or {}).get('calls')}",
    )

    print("\n[6] 状态可 JSON 往返（上面每段都做了，这里只钉住关键字段仍在）")
    for key in ("caseFiles", "mediaBundle", "promptGraph", "observationsBundle", "nativeDraft"):
        check(f"  {key} 在状态里", key in staged_state, sorted(staged_state)[:12])

    print()
    if FAILURES:
        print(f"FAILED: {len(FAILURES)} 项 — {FAILURES}")
        return 1
    print("ALL PASSED")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
