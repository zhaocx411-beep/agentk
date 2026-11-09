"""重构护栏：跑出整份报告存成基线，重构后逐字段对比。

为什么需要它：接下来要把 `analyze_case` 的七个阶段拆成显式传状态的纯函数，好让
worker 侧包成七个 Temporal activity。这种体量的重构，唯一可靠的验收标准是"同样的
输入跑出同样的报告"——靠读代码确认等价是不现实的。

**两条基线，缺一不可：**

* `mock`（`use_vlm=False`）——确定性骨架。便宜、无依赖，但它绕过全部模型阶段，
  跑出来的 issue / 指令 / 证据都是空的。只有它的话，恰好挡不住这次重构的风险区。
* `replay`（`--case replay`）——**完整 live 编排**。模型响应来自 `runtime/traces/`
  里的真实录制（每步的 `output.parsed_json`），按 stage 回放；抽帧、4b 帧预算协商、
  4c 增量复核、聚合、证据图、报告组装全部真实执行。这条才覆盖 compile / observe /
  scan / recheck / ground 这五个要被拆成 activity 的阶段。

回放只拦 `generate_json` 这一层，所以它验的不是"模型答得对不对"，而是"编排把同样的
响应组装成同样的报告"——正是重构要保住的东西。

用法：

    # 重构前
    PYTHONPATH=src python tests/baseline_pipeline.py --save
    # 重构后
    PYTHONPATH=src python tests/baseline_pipeline.py --check

每次运行都会变的字段（run_id、耗时、时间戳、绝对路径）在对比时按名字剔除，
剔除清单显式写在下面，不做模糊匹配——否则真正的回归会被顺手忽略掉。
"""

from __future__ import annotations

import argparse
import json
import sys
from copy import deepcopy
from pathlib import Path
from typing import Any

REPO = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPO / "src"))

BASELINE_DIR = Path(__file__).resolve().parent
VIDEO = REPO / "runtime" / "uploads" / "staging_d87691904bc0" / "video.mp4"
PROMPT = "一只橘色的猫从画面左侧走到右侧，背景是草地，阳光明亮"

# 回放素材：这条 case 的 trace 里有全部五个模型阶段的真实响应。
REPLAY_CASE = REPO / "runtime" / "uploads" / "up_c87e9bf1c502d2a8"
TRACE_DIR = REPO / "runtime" / "traces"


def baseline_path(case: str) -> Path:
    return BASELINE_DIR / f"baseline_{BASELINE_FOR.get(case, case)}.json"

# 每次运行必然不同，与编排正确性无关。逐个列出而不是按前缀模糊匹配：
# 模糊匹配会把真正的回归（比如某个阶段产出整体消失）一起吞掉。
VOLATILE_KEYS = {
    "run_id",
    "saved_path",
    "saved_url",
    "report_html_path",
    "report_view_url",
    "trace_saved_path",
    "elapsed_ms",
    "elapsed_seconds",
    "started_at",
    "finished_at",
    "generated_at",
    "duration_ms",
    "from_cache",
    "checkpoint",
    "llm_trace",  # 逐步耗时与 run_id 混在一起，单独比步数
    "cost",  # mock 路径没有真实计费，数值无意义
}


def strip_volatile(node: Any) -> Any:
    """递归剔除易变字段，保留结构与其余值。"""
    if isinstance(node, dict):
        return {
            key: strip_volatile(value)
            for key, value in sorted(node.items())
            if key not in VOLATILE_KEYS
        }
    if isinstance(node, list):
        return [strip_volatile(item) for item in node]
    if isinstance(node, str) and str(REPO) in node:
        # 绝对路径里含临时目录/run_id，只保留文件名做形状比对。
        return f"<path:{Path(node).name}>"
    return node


def run_mock() -> dict[str, Any]:
    from aigc_eval.dataset.upload_case import build_upload_case_files
    from aigc_eval.judge.pipeline import analyze_case

    if not VIDEO.exists():
        raise SystemExit(f"缺素材：{VIDEO}")
    case_files = build_upload_case_files(prompt=PROMPT, video_path=VIDEO, references=[])
    return analyze_case(
        case_files.case_id,
        case_files=case_files,
        use_vlm=False,  # mock：不联网、不花钱，编排骨架与线上一致
        allow_fallback=True,
        source="baseline",
        refresh=True,  # 必须绕开整份报告缓存，否则测的是缓存不是编排
    )


# --------------------------------------------------------------------------
# 回放：完整 live 编排 + 录制的模型响应
# --------------------------------------------------------------------------


def replay_prompt_text() -> str:
    """回放 case 的提示词。

    键名是 ``prompt_zh`` / ``prompt_en``（``upload_case.build_case_files`` 写的就是这两个）。
    这里曾经读 ``zh`` / ``en``，于是一直取到空串——等价性测试因此长期拿**空提示词**在跑。
    两条路径仍然一致（录制响应按阶段回放，与提示词无关），所以测试照样通过，但它比对的
    编排跑在退化输入上：提示词编译、指令 checklist、约束路由全都拿不到东西。

    旧键留作兜底：万一还有按旧格式写的装置，不该因为改这里而整体失效。
    """
    data = json.loads((REPLAY_CASE / "prompt.json").read_text(encoding="utf-8"))
    for key in ("prompt_zh", "prompt_en", "zh", "en"):
        text = str(data.get(key) or "").strip()
        if text:
            return text
    raise SystemExit(f"回放 case 的提示词是空的：{REPLAY_CASE / 'prompt.json'}")


def load_recorded_stages() -> tuple[dict[str, Any], str]:
    """从 trace 里取出各模型阶段的响应，按 ``(stage, title)`` 建索引。

    挑步数最全的那条 trace 而不是最新的：中途失败的运行也会存 trace，那种缺阶段，
    回放到一半会撞上"没有录制"而中断。

    **键里必须带 title。** 一个阶段可以发多次模型调用：4c 的「音频独立复听」与
    「音频反证复核」都传 ``stage="audio"``。只按 stage 建索引，后录的那条会把先录的
    覆盖掉，回放时第一次调用拿到的是反证的响应——而反证按契约"只返回输入中要求复核的
    键"，据它往下跑会得出与录制时不同的结论，可测试照样"通过"，因为两条路径拿到的是
    同一份错响应。
    """
    best: tuple[int, dict[str, Any], str] | None = None
    for path in TRACE_DIR.glob("*.json"):
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
        except Exception:  # noqa: BLE001 - 坏文件跳过
            continue
        if data.get("case_id") != REPLAY_CASE.name:
            continue
        stages = {
            (step["stage"], str(step.get("title") or "")): step["output"]["parsed_json"]
            for step in data.get("steps") or []
            if step.get("source") != "tool"
            and isinstance(step.get("output"), dict)
            and step["output"].get("parsed_json")
        }
        if best is None or len(stages) > best[0]:
            best = (len(stages), stages, str(data.get("model") or "replay-model"))
    if best is None:
        raise SystemExit(f"没找到 {REPLAY_CASE.name} 的可回放 trace（{TRACE_DIR}）")
    return best[1], best[2]


def pick_recorded(stages: dict[Any, Any], stage: str, title: str) -> Any:
    """取这次调用该回放的响应；取不到返回 ``None`` 交给调用方报错。

    先按 ``(stage, title)`` 精确匹配，不中再退到只按 stage 匹配。退化那一档是为了容忍
    标题微调——标题是给人看的，改一个字不该让整条回放失效——但阶段本身没录过仍然会
    落到 ``None``，由调用方响亮地失败。
    """
    exact = stages.get((stage, title))
    if exact is not None:
        return exact
    for key, payload in stages.items():
        if isinstance(key, tuple) and key[0] == stage:
            return payload
    return None


def recorded_stage_names(stages: dict[Any, Any]) -> list[str]:
    """报错信息里列出已录的阶段名，去重后排序。"""
    return sorted({key[0] if isinstance(key, tuple) else str(key) for key in stages})


class ReplayConfig:
    """替掉真实 provider 配置，免得回放依赖线上环境变量。"""

    def __init__(self, model: str):
        self.model = model
        self.chat_model = model
        self.configured = True


class ReplayClient:
    """按 stage 回放录制响应；其余行为与真 provider 一致。

    抽帧、帧预算协商、JSON 后处理、聚合全都真实执行，所以这个替身验的是编排，
    而不是模型。

    ``supports_video`` / ``max_media`` 必须与录制时的通道一致（录制来自 agentk），
    否则 4b 会按另一个帧预算走另一条分支，跑出来的报告与基线不可比。

    **两套入口都要实现。** 单体路径（studio、批处理）走 ``generate_json``；worker 路径
    走 ``build_request`` / ``consume_response``——那条路上模型调用被抽到 workflow 里，
    client 只负责组装与解析。两套共用同一份录制响应，这样"两条路径给出同一份报告"
    才是真的在比对，而不是各自和自己比。
    """

    supports_video = True
    max_media = 10

    def __init__(self, config: Any, *, trace: Any = None, on_event: Any = None):
        self.config = config
        self.trace = trace
        self.on_event = on_event
        self.usage: list[dict[str, Any]] = []
        self.stages: dict[str, Any] = {}
        self.seen: list[str] = []

    def generate_json(
        self,
        parts: list[dict[str, Any]],
        *,
        system_instruction: str | None = None,
        temperature: float = 0.1,
        stage: str = "replay",
        title: str = "replay",
        max_attempts: int = 4,
        max_output_tokens: int | None = None,
    ) -> dict[str, Any]:
        self.seen.append(stage)
        recorded_payload = pick_recorded(self.stages, stage, title)
        if recorded_payload is None:
            # 不静默返回空：编排新增了一个模型阶段时必须显式暴露，
            # 否则基线会"通过"，而实际上少跑了一整段。
            raise SystemExit(
                f"回放缺少 stage={stage!r} 的录制"
                f"（已录：{recorded_stage_names(self.stages)}）"
            )
        self.usage.append(
            {
                "stage": stage,
                "title": title,
                "attempt": 1,
                "ok": True,
                "replay": True,
                "model": self.config.model,
            }
        )
        recorded = deepcopy(recorded_payload)
        if self.trace is not None:
            self.trace.record_step(
                stage=stage,
                title=title,
                system_instruction=system_instruction,
                parts=parts,
                raw_output=json.dumps(recorded, ensure_ascii=False)[:8000],
                parsed_output=recorded,
                started_at=0.0,
                temperature=temperature,
                source="replay",
            )
        return recorded

    # ---- worker 路径的三段式入口 ----------------------------------------

    def build_request(
        self,
        parts: list[dict[str, Any]],
        *,
        system_instruction: str | None = None,
        stage: str = "replay",
        title: str = "replay",
        digest: str = "",
        attempt: int = 1,
    ) -> Any:
        """组装段。回放不真的上传素材，但要如实反映派发到哪个能力——
        ``consume_response`` 按 capabilityId 决定怎么取原文，选错就解不出来。"""
        from aigc_eval.providers.model_call import (
            GENERATE_TEXT_CAPABILITY,
            UNDERSTAND_MEDIA_CAPABILITY,
            ModelRequest,
        )

        has_media = any(
            isinstance(part, dict)
            and any(
                key in part
                for key in ("video_path", "audio_path", "inline_data", "inlineData")
            )
            for part in parts
        )
        capability = (
            UNDERSTAND_MEDIA_CAPABILITY if has_media else GENERATE_TEXT_CAPABILITY
        )
        payload: dict[str, Any] = {"model": self.config.model, "prompt": "<replay>"}
        if has_media:
            payload["mediaUrls"] = ["replay://media"]
        return ModelRequest(
            capabilityId=capability,
            payload=payload,
            digest=digest,
            stage=stage,
            title=title,
            attempt=attempt,
        )

    def consume_response(
        self,
        request: Any,
        response: Any,
        *,
        parts: list[dict[str, Any]],
        system_instruction: str | None = None,
        temperature: float = 0.1,
        started: float | None = None,
    ) -> Any:
        from aigc_eval.providers.model_call import ModelOutcome

        if response.error is not None:
            return ModelOutcome(
                ok=False,
                error=response.error,
                errorCode=response.errorCode,
                retryable=response.errorRetryable,
            )
        self.seen.append(request.stage)
        self.usage.append(
            {
                "stage": request.stage,
                "title": request.title,
                "attempt": request.attempt,
                "ok": True,
                "replay": True,
                "model": self.config.model,
            }
        )
        try:
            parsed = json.loads(response.rawText)
        except Exception as error:  # noqa: BLE001 - 解析失败要重发，不是终态
            return ModelOutcome(ok=False, error=str(error), retryable=True)
        if self.trace is not None:
            self.trace.record_step(
                stage=request.stage,
                title=request.title,
                system_instruction=system_instruction,
                parts=parts,
                raw_output=response.rawText[:8000],
                parsed_output=parsed,
                started_at=0.0,
                temperature=temperature,
                source="replay",
            )
        return ModelOutcome(ok=True, parsed=parsed)

    def record_failure(self, request: Any, **_: Any) -> None:
        """终态失败的 trace。回放里不会走到，留个空实现让契约完整。"""


def recorded_responder(stages: dict[str, Any], model: str) -> Any:
    """建一个"假 invoker"：按请求的 stage 返回录制响应。

    改造后 fake 的位置从"假 client"下移到了"假 invoke"，这一层更薄也更贴近真实
    边界——真实路径上 workflow 拿到的就是一个 ``ModelResponse``。
    """

    def respond(request: Any) -> dict[str, Any]:
        from aigc_eval.providers.model_call import ModelResponse

        payload = pick_recorded(stages, request.stage, request.title)
        if payload is None:
            # 不静默返回空：编排新增了一个模型阶段时必须显式暴露，否则测试会
            # "通过"，而实际上少跑了一整段。
            raise SystemExit(
                f"回放缺少 stage={request.stage!r} 的录制"
                f"（已录：{recorded_stage_names(stages)}）"
            )
        return ModelResponse(
            rawText=json.dumps(payload, ensure_ascii=False),
            model=model,
        ).model_dump(mode="json")

    return respond


def run_replay() -> dict[str, Any]:
    """用录制响应跑完整 live 编排。"""
    import aigc_eval.judge.pipeline as jp
    from aigc_eval.dataset.upload_case import build_upload_case_files

    manifest_path = REPLAY_CASE / "manifest.json"
    if not manifest_path.exists():
        raise SystemExit(f"缺回放素材：{manifest_path}")
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    prompt_text = replay_prompt_text()

    stages, model = load_recorded_stages()
    case_files = build_upload_case_files(
        prompt=prompt_text,
        video_path=Path(manifest["video"]),
        references=[
            {"path": ref["path"], "kind": ref.get("kind", "image")}
            for ref in manifest.get("references") or []
        ],
    )

    def factory(config: Any, *, trace: Any = None, on_event: Any = None) -> ReplayClient:
        client = ReplayClient(config, trace=trace, on_event=on_event)
        client.stages = stages
        return client

    # 只替换 provider 层：其余编排一行不动。
    saved_clients = dict(jp._PROVIDER_CLIENTS)
    saved_configs = dict(jp._PROVIDER_CONFIGS)
    jp._PROVIDER_CLIENTS["agentk"] = factory  # type: ignore[assignment]
    jp._PROVIDER_CONFIGS["agentk"] = lambda: ReplayConfig(model)  # type: ignore[assignment]
    try:
        return jp.analyze_case(
            case_files.case_id,
            case_files=case_files,
            use_vlm=True,
            provider="agentk",
            allow_fallback=False,  # 回放不该退化成 mock，退化了要立刻报错
            source="replay",
            refresh=True,
        )
    finally:
        jp._PROVIDER_CLIENTS.clear()
        jp._PROVIDER_CLIENTS.update(saved_clients)
        jp._PROVIDER_CONFIGS.clear()
        jp._PROVIDER_CONFIGS.update(saved_configs)


# --------------------------------------------------------------------------
# 换实例回放：产物只经对象存储传递
# --------------------------------------------------------------------------


class MemoryBlob:
    """内存对象存储。只实现产物层用到的三个方法。"""

    def __init__(self) -> None:
        self.objects: dict[str, bytes] = {}
        self.fetched: list[str] = []

    def exists(self, key: str) -> bool:
        return key in self.objects

    def put_at(self, key: str, data: bytes, *, content_type: str) -> str:
        self.objects[key] = data
        return f"memory://{key}"

    def get_bytes(self, key: str) -> bytes | None:
        self.fetched.append(key)
        return self.objects.get(key)


def run_relocate() -> dict[str, Any]:
    """回放一遍，但每个产出阶段结束后立刻删掉它写在本地盘的文件。

    这模拟的是 Temporal 把各 activity 派到不同实例上：后一个阶段开工时，前一个阶段的
    帧图在本地根本不存在，只能从对象存储还原。跑出来的报告必须与 `replay` 基线逐字段
    一致——一致才说明产物层是透明的，拆 activity 不会悄悄改变判定。

    删文件是有风险的动作（输入素材也在删除范围内），所以结束时无论成败都会把 manifest
    里所有缺失的文件从内存对象存储写回去。
    """
    import aigc_eval.judge.pipeline as jp
    from aigc_eval.judge.artifact_store import ArtifactStore
    from aigc_eval.paths import RUNTIME_ROOT

    blob = MemoryBlob()
    store = ArtifactStore(blob)
    seen: list[dict[str, Any]] = []  # 所有 manifest 条目，用于兜底恢复

    evicted: list[str] = []

    def evicting(func: Any) -> Any:
        def wrapper(**kwargs: Any) -> Any:
            result = func(**kwargs)
            for entries in (result.get("artifacts") or {}).values():
                for entry in entries or []:
                    seen.append(entry)
                    target = RUNTIME_ROOT / str(entry["path"])
                    if target.exists():
                        target.unlink()
                        evicted.append(str(entry["path"]))
            return result

        return wrapper

    producers = ("_stage_prepare", "_stage_scan", "_stage_evidence")
    saved_stages = {name: getattr(jp, name) for name in producers}
    saved_shared = jp.shared_artifact_store
    jp.shared_artifact_store = lambda: store  # type: ignore[assignment]
    for name in producers:
        setattr(jp, name, evicting(saved_stages[name]))
    try:
        report = run_replay()
    finally:
        jp.shared_artifact_store = saved_shared  # type: ignore[assignment]
        for name, func in saved_stages.items():
            setattr(jp, name, func)
        # 兜底：把删掉但没被还原的文件写回，别让护栏破坏回放素材。
        for entry in seen:
            target = RUNTIME_ROOT / str(entry["path"])
            if not target.exists():
                data = blob.get_bytes(str(entry["key"]))
                if data:
                    target.parent.mkdir(parents=True, exist_ok=True)
                    target.write_bytes(data)

    stats = report.pop("artifacts", None)
    if not isinstance(stats, dict):
        raise SystemExit("报告里没有 artifacts 字段：产物层没被启用，这一轮什么都没验到")
    restored = {
        group: (counts or {}).get("restored", 0)
        for group, counts in (stats.get("restored") or {}).items()
    }
    uploaded = stats.get("counts") or {}
    print(f"      产物上传：{uploaded}")
    print(f"      本地删除：{len(evicted)} 个文件；对象存储拉取：{len(blob.fetched)} 次")
    print(f"      报告阶段还原：{restored}")
    for group in ("input", "sampling", "scan_frames", "evidence"):
        if not uploaded.get(group):
            raise SystemExit(f"产物组 {group} 一个文件都没上传，等于没验")
    if not evicted:
        raise SystemExit("没有删掉任何本地产物：这一轮退化成了普通回放")
    # 拉取次数要盖住删除数，否则说明有阶段其实在读别的路径、或者删除发生在没人再用之后，
    # 两种情况下"换实例"都没被真正验证到。
    if len(blob.fetched) < len(evicted):
        raise SystemExit(
            f"拉取次数（{len(blob.fetched)}）少于删除数（{len(evicted)}）："
            "有产物被删后没人还原，却仍跑出了同样的报告，说明这批产物其实没被用到"
        )
    print("PASS  产物只经对象存储传递，也跑完了全流程")
    return report


RUNNERS = {"mock": run_mock, "replay": run_replay, "relocate": run_relocate}

# 换实例回放的期望产出就是普通回放的基线：产物层若是透明的，两者必须逐字段一致。
BASELINE_FOR = {"relocate": "replay"}


def summarize(report: dict[str, Any]) -> dict[str, Any]:
    """报告之外单独钉几个关键计数，便于定位是哪一段变了。"""
    trace = report.get("llm_trace") or {}
    return {
        "schema_version": report.get("schema_version"),
        "analysis_status": report.get("analysis_status"),
        "issue_count": len(report.get("issues") or []),
        "rubric_dims": len(report.get("rubric") or []),
        "subcriteria": sum(
            len(dim.get("subcriteria") or [])
            for dim in (report.get("rubric") or [])
            if isinstance(dim, dict)
        ),
        "instruction_rows": len(report.get("instruction_checklist") or []),
        "evidence_cards": len(report.get("evidence") or []),
        "sampling_frames": len((report.get("sampling") or {}).get("frames") or []),
        "trace_steps": trace.get("step_count"),
        "top_level_keys": sorted(report.keys()),
    }


def run_one(case: str, *, save: bool) -> int:
    print(f"\n{'=' * 60}\n{case}\n{'=' * 60}")
    report = RUNNERS[case]()
    payload = {"summary": summarize(report), "report": strip_volatile(report)}
    path = baseline_path(case)

    if save and case in BASELINE_FOR:
        # 它比对的是别人的基线，自己没有可存的东西；存了反而会覆盖那份基线。
        print(f"（{case} 复用 {BASELINE_FOR[case]} 的基线，跳过保存）")
        return 0

    if save:
        path.write_text(
            json.dumps(payload, ensure_ascii=False, indent=1, sort_keys=True),
            encoding="utf-8",
        )
        print(f"基线已存：{path.name}")
        for key, value in payload["summary"].items():
            if key != "top_level_keys":
                print(f"  {key:20} {value}")
        return 0

    if not path.exists():
        raise SystemExit(f"没有基线 {path.name}，先跑 --save")
    old = json.loads(path.read_text(encoding="utf-8"))

    bad: list[str] = []
    for key, value in payload["summary"].items():
        was = old["summary"].get(key)
        same = was == value
        print(f"{'PASS' if same else 'FAIL'}  {key}" + (f" — {was} → {value}" if not same else ""))
        if not same:
            bad.append(key)

    if old["report"] != payload["report"]:
        # 只报第一处差异的路径：整份 diff 有几十 KB，读不动。
        def first_diff(a: Any, b: Any, path: str = "") -> str | None:
            if isinstance(a, dict) and isinstance(b, dict):
                for key in sorted(set(a) | set(b)):
                    if key not in a:
                        return f"{path}.{key}（新增）"
                    if key not in b:
                        return f"{path}.{key}（丢失）"
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
            return None if a == b else f"{path}: {str(a)[:60]} → {str(b)[:60]}"

        where = first_diff(old["report"], payload["report"]) or "（结构相同但序列化不同）"
        print(f"FAIL  报告内容有差异，首处：{where}")
        bad.append("report")
    else:
        print("PASS  报告逐字段一致")

    if bad:
        print(f"FAILED: {bad}")
        return 1
    return 0


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--save", action="store_true", help="跑一次并存为基线")
    parser.add_argument("--check", action="store_true", help="跑一次并与基线对比")
    parser.add_argument(
        "--case",
        choices=(*RUNNERS, "all"),
        default="all",
        help="mock=确定性骨架；replay=完整 live 编排（录制响应回放）",
    )
    opts = parser.parse_args()
    if not (opts.save or opts.check):
        parser.error("给 --save 或 --check")

    cases = list(RUNNERS) if opts.case == "all" else [opts.case]
    failed = [case for case in cases if run_one(case, save=opts.save) != 0]
    if failed:
        print(f"\nFAILED: {failed}")
        return 1
    print("\nALL PASSED" + ("" if opts.save else "：重构未改变行为"))
    return 0


if __name__ == "__main__":
    sys.exit(main())
