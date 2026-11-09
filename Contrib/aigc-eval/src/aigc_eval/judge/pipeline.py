"""判定链的编排层：`analyze_case` 按顺序串起各阶段并汇总成报告。

阶段顺序遵循一条硬规则：**凡是程序能算准的，都不交给模型。** 模型只做「看画面、
给事实」，所有可重放的测量、聚合与门控都在代码里。

1. 确定性前置检查（ffprobe/ffmpeg）        -> integrity
1b. V2V 源视频对齐测量（ffmpeg SSIM）      -> v2v_align
2. 抽帧与素材装配                          -> media_parts
3. 拆解提示词要求                          -> prompt_compiler
4. 观察（只记事实与待复核时刻）            -> stages / mock
4b. 原生八维主判（唯一事实底稿）           -> zoom_inspector
4c. 疑点增量复核 / 音频独立复听            -> native8_recheck
6. 确定性三档聚合                          -> native8
7. 证据定位、成本核算、报告渲染            -> stages / evidence / cost / report

4b 是唯一的事实底稿，4c 只能按候选 issue 增量提交证据，无权整维覆盖；三档由代码
按结构化事实算出，模型给不了分数。native8 没有综合分，因此**任何一条被凭空写进
报告的结论都不会被平均掉**，这也是编排里到处宁可报错也不兜底的原因。

本模块只做编排，具体实现都在上面这些子模块里。此外它还转导一批名字：
server/app.py、dataset/upload_case.py 等调用点历史上都从 pipeline 取 `CaseFiles`、
`load_cached`、`RUNTIME_ROOT` 等，转导后那些调用点无需改动。
"""

from __future__ import annotations

import json
import re
from copy import deepcopy
from pathlib import Path
from typing import Any, Callable
from urllib.parse import quote

from aigc_eval.config import (
    DEFAULT_PROVIDER,
    load_agentk_config,
    normalize_provider,
)
from aigc_eval.judge.cases import (
    CaseFiles,
    _collection_focus,
    detect_mode,
    get_case_files,
    load_prompt,
    source_video_of,
)
from aigc_eval.judge.artifact_store import (
    ArtifactStore,
    materialize,
    put_files,
    shared_artifact_store,
)
from aigc_eval.judge.cost import _profile_for, build_cost
from aigc_eval.judge.evidence import build_native_evidence
from aigc_eval.judge.integrity import check_integrity, inspect_video
from aigc_eval.judge.layout import RESULTS_ROOT, work_dir
from aigc_eval.judge.media_parts import (
    audio_part,
    prepare_frame_bundle,
    reference_kind,
    reference_label,
    select_references,
    video_part,
)
from aigc_eval.judge.mock import MockVLMObserver, MockVLMVerifier
from aigc_eval.judge.model_checkpoint import checkpointed
from aigc_eval.judge.native8 import (
    SCHEMA_VERSION as NATIVE8_SCHEMA_VERSION,
    finalize_native8_result,
    validate_native8_report,
)
from aigc_eval.judge.native8_recheck import (
    apply_native_audio_increment,
    apply_native_incremental_recheck,
    apply_native_v2v_alignment,
    audio_signal_profile,
    build_native_recheck_candidates,
    AUDIO_NOT_HEARD_REASON,
    collapse_instruction_constraint_issues,
    enforce_native_instruction_checklist,
    merge_audio_confirmation,
    normalize_audio_recheck,
)
from aigc_eval.judge.prompt_compiler import compile_prompt_heuristic, compile_prompt_vlm
from aigc_eval.judge.result_cache import _cache_key
from aigc_eval.judge.rubric_prompts import (
    AUDIO_CONFIRM_SYSTEM,
    AUDIO_RECHECK_SYSTEM,
    AUDIO_REFERENCE_SYSTEM,
    NATIVE8_RECHECK_SYSTEM,
)
from aigc_eval.judge.settings import (
    AUDIO_RECHECK,
    CURATED_CASES,
    FOCUS_MIN_GLOBAL_FRAMES,
    GROUNDING_PASS,
    SEND_WHOLE_VIDEO,
    SKIP_EVIDENCE,
    TARGETED_RECHECK,
    UNIFIED_SCAN_FRAMES,
    UNIFIED_SCAN_MAX_SIDE,
    UNIFIED_SCAN_VIDEO,
    VIDEO_MAX_MB,
    plan_frame_budget,
)
from aigc_eval.judge.stages import GeminiGroundingLocator, GeminiObserver
from aigc_eval.judge.v2v_align import align_profile as v2v_align_profile
from aigc_eval.judge.zoom_inspector import GeminiZoomInspector
from aigc_eval.providers.agentk import AgentKClient
from aigc_eval.providers.media_io import encode_file
from aigc_eval.trace_store import TraceSession

# 以下纯为转导：server/app.py、dataset/upload_case.py 等调用点历史上都从 pipeline
# 取这些名字。拆分后它们的家搬到了子模块，这里保留入口，调用点一行都不用改。
# 删任何一个都会打断外部导入。
from aigc_eval.judge.cases import (  # noqa: F401
    case_info,
    discover_cases,
    list_collections,
    media_path,
)
from aigc_eval.judge.layout import PROJECT_ROOT, RUNTIME_ROOT  # noqa: F401
from aigc_eval.judge.result_cache import load_cached  # noqa: F401


# ---- provider 分发 ----------------------------------------------------------
# 各 provider 的 client 只需满足 ``model_call.JudgeClient``：一个 generate_json。
# 现在只剩 agentk 一条通道，这两张表仍保留——它们是"判定逻辑不认识传输层"这条边界的
# 落点，新增通道时只改这里。
_PROVIDER_CONFIGS = {
    "agentk": load_agentk_config,
}
_PROVIDER_CLIENTS = {
    "agentk": AgentKClient,
}


def _v2v_align_report(profile: dict[str, Any] | None) -> dict[str, Any] | None:
    """剖面进报告前去掉逐点 series。

    series 的点数是「时长 × 采样 fps」，进报告 JSON 会把体积顶起来，而报告页只用
    到聚合量。逐点数据仍完整留在 trace 里，需要复核时可以按 run_id 取回。
    """
    if not isinstance(profile, dict) or not profile.get("available"):
        return None
    return {key: value for key, value in profile.items() if key != "series"}


def make_emit(
    on_event: Callable[[dict[str, Any]], None] | None,
) -> Callable[..., None]:
    """构造阶段事件上报函数。

    提成工厂是为了让 worker 侧的每个 activity 用**同一个** emit 实现：阶段事件的形状
    （kind/id/status/input/output/detail）是前端与 AgentH 进度上报的共同契约，两处各写
    一份迟早漂移。
    """

    def emit(
        kind: str,
        step_id: str,
        title: str,
        status: str,
        *,
        input_text: str = "",
        output_text: str = "",
        detail: str = "",
    ) -> None:
        """Report one pipeline step to the UI as a call/result pair."""
        if on_event is None:
            return
        try:
            on_event(
                {
                    "type": "stage",
                    "kind": kind,  # "tool" (确定性程序) 或 "model" (大模型)
                    "id": step_id,
                    "title": title,
                    "status": status,  # running | done | error
                    "input": input_text,
                    "output": output_text,
                    "detail": detail,
                }
            )
        except Exception:  # noqa: BLE001 - UI updates must never break analysis
            pass

    return emit


def result_cache_path(
    slug: str,
    *,
    source: str,
    use_vlm: bool,
    provider: str,
    mode: str | None,
) -> Path:
    """整份报告的缓存落点。键含通道与模式，换通道不会互相污染。"""
    return RESULTS_ROOT / f"{_cache_key(slug, source, use_vlm, provider, mode)}.json"


def load_result_cache(
    cache_path: Path,
    *,
    refresh: bool,
    emit: Callable[..., None],
) -> dict[str, Any] | None:
    """命中同输入的历史报告就返回它，否则返回 None。

    worker 侧的 prepare 阶段也用这一份：命中缓存时它会跳过中间六个阶段直接出报告，
    判断口径必须与单机路径完全一致——两边各写一遍，迟早出现"页面命中缓存、平台却重跑"
    这种只在账单上才看得出来的分歧。
    """
    if refresh or not cache_path.exists():
        return None
    try:
        cached = json.loads(cache_path.read_text(encoding="utf-8"))
        if cached.get("schema_version") != NATIVE8_SCHEMA_VERSION:
            raise ValueError("stale cache schema")
    except Exception:  # noqa: BLE001 - corrupt cache falls through to a fresh run
        return None
    cached["from_cache"] = True
    emit(
        "tool", "cache", "复用历史结果", "done",
        output_text="该视频此前已分析过，直接使用历史结果，未重新调用模型。",
    )
    return cached


def build_live_client(
    *,
    provider: str,
    session: TraceSession,
    on_event: Callable[[dict[str, Any]], None] | None = None,
    refresh: bool = False,
) -> Any:
    """建一个带检查点的 provider client。

    检查点包在最外层：模型调用按请求哈希复用上次结果，让崩溃/容器重启后的重跑不必把
    模型钱再花一遍。worker 侧每个 activity 都会新建 client，所以这个包装必须只有一处
    实现——漏包一个阶段，那个阶段的重试就是全价重跑，而且不会有任何报错提示。
    """
    provider = normalize_provider(provider)
    config = _PROVIDER_CONFIGS.get(provider, load_agentk_config)()
    client = _PROVIDER_CLIENTS[provider](config, trace=session, on_event=on_event)
    session.engine = provider
    session.model = config.model
    return checkpointed(client, provider=provider, refresh=refresh, on_event=on_event)


def build_relay_client(
    *,
    provider: str,
    session: TraceSession,
    responses: dict[str, Any] | None = None,
    on_event: Callable[[dict[str, Any]], None] | None = None,
    refresh: bool = False,
    max_attempts: int = 4,
) -> Any:
    """建一个把模型调用交出去的 client，给 worker 路径用。

    与 ``build_live_client`` 的唯一区别是外层包的是 ``SuspendingClient`` 而不是
    ``CheckpointedClient``：前者未命中检查点时抛出 ``NeedsModelCall``，由 workflow 走
    ``invoke_capability`` 发起调用，再重放这一段；后者未命中就自己打 HTTP。

    两者都吸收了检查点，所以不会叠两层——叠了会算两次 digest、记两条复用 usage。
    """
    from aigc_eval.judge.model_relay import suspending

    provider = normalize_provider(provider)
    config = _PROVIDER_CONFIGS.get(provider, load_agentk_config)()
    client = _PROVIDER_CLIENTS[provider](config, trace=session, on_event=on_event)
    session.engine = provider
    session.model = config.model
    return suspending(
        client,
        provider=provider,
        responses=responses,
        max_attempts=max_attempts,
        refresh=refresh,
        on_event=on_event,
    )


def _frame_row_paths(rows: Any) -> list[Path]:
    """从 ``{"path": ..., "time": ...}`` 这类帧记录里取存在的本地文件。

    4b/证据定位返回的帧记录里 ``path`` 是字符串（要能进 JSON），这里统一转回 Path
    并过掉已经不在盘上的条目。
    """
    paths: list[Path] = []
    for row in rows or []:
        if not isinstance(row, dict):
            continue
        raw = str(row.get("path") or "")
        if raw and Path(raw).exists():
            paths.append(Path(raw))
    return paths


def _evidence_image_paths(cards: Any) -> list[Path]:
    """证据卡里所有图片：卡片级 image_path 与每帧的 image_path 都要收。"""
    paths: list[Path] = []
    for card in cards or []:
        if not isinstance(card, dict):
            continue
        candidates = [card.get("image_path")]
        for frame in card.get("frames") or []:
            if isinstance(frame, dict):
                candidates.append(frame.get("image_path"))
        for raw in candidates:
            if raw and Path(str(raw)).exists():
                paths.append(Path(str(raw)))
    return paths


def _input_asset_paths(
    case_files: CaseFiles, video_path: Path, source_video_path: Path | None
) -> list[Path]:
    """一次质检的输入素材：主视频、源视频、参考图/音频，以及合成 case 的两份小文件。

    每个阶段都要读它们，所以在流程一开始就整批入对象存储；换实例续跑时先还原这批，
    后面的阶段才谈得上照原样跑。

    `prompt.json` 与 `manifest.json` 看着不像"素材"，但少了它们后面照样跑不动：每个
    阶段都会重新 `load_prompt()`，报告里的媒体链接也要靠 manifest 才解得开。
    """
    paths = [video_path, Path(case_files.prompt_path)]
    manifest = Path(case_files.case_dir) / "manifest.json"
    if manifest.exists():
        paths.append(manifest)
    if source_video_path is not None:
        paths.append(source_video_path)
    if case_files.input_image_path:
        paths.append(Path(case_files.input_image_path))
    for ref in case_files.reference_assets or []:
        raw = str(ref.get("path") or "")
        if raw:
            paths.append(Path(raw))
    return [path for path in paths if path.exists()]


def _native_send_video(client: Any, video_path: Path) -> bool:
    """这次调用能否把整段视频交给模型。

    三个条件都得满足：全局开关没关、通道声明支持视频、体积在内联上限内。
    """
    return bool(
        SEND_WHOLE_VIDEO
        and getattr(client, "supports_video", True)
        and video_path.exists()
        and video_path.stat().st_size / 1e6 <= VIDEO_MAX_MB
    )


def _plan_scan_media(
    client: Any,
    *,
    references: list[dict[str, Any]],
    prompt_text: str,
    send_video: bool,
    has_source_video: bool,
    is_v2v: bool,
) -> tuple[int, list[dict[str, Any]], dict[str, Any] | None]:
    """按通道素材上限敲定 4b 实际送多少高清帧、带哪些参考素材。

    非帧素材（整段视频、V2V 源视频、参考图）信息密度最高，优先占额度，剩下的才
    给帧。参考图口径必须与 `scan_unified` 内部**逐字一致**——同样是
    `stage="consistency"` 且只取 image 类：两边差一件，协商出的帧数就会被通道再裁
    一次，重新制造出提示词与载荷不符的问题，而那正是这里要消除的。

    帧数跌破 `FOCUS_MIN_GLOBAL_FRAMES` 时，V2V 优先牺牲参考图而不是帧：源视频本身
    就是「改动前」的完整基线，换装图、风格图给的是同一信息的静态切片，而帧数一旦
    低于底线，全片时间覆盖就断了，模型会把没看到的时段报成 not_observable。
    实测 10 件上限下，V2V 带 2 张参考图只剩 6 帧，去掉参考图能回到 8 帧。

    返回 `(帧数, 送给 4b 的参考素材, 降载说明或 None)`。
    """
    max_media = getattr(client, "max_media", None)
    fixed = (1 if send_video else 0) + (1 if has_source_video else 0)
    image_refs = [
        ref for ref in select_references(references, stage="consistency", prompt=prompt_text)
        if reference_kind(ref) == "image"
    ]
    frames, note = plan_frame_budget(
        requested_frames=UNIFIED_SCAN_FRAMES,
        reserved_media=fixed + len(image_refs),
        max_media=max_media,
    )
    if not (is_v2v and image_refs and frames < FOCUS_MIN_GLOBAL_FRAMES):
        return frames, references, note

    lean_refs = [ref for ref in references if reference_kind(ref) != "image"]
    frames, note = plan_frame_budget(
        requested_frames=UNIFIED_SCAN_FRAMES,
        reserved_media=fixed,
        max_media=max_media,
    )
    note = dict(note or {})
    note["dropped_image_references"] = len(image_refs)
    return frames, lean_refs, note


def _stage_prepare(
    *,
    emit: Callable[..., None],
    session: TraceSession,
    case_files: CaseFiles,
    video_path: Path,
    source: str,
    source_label: str,
    resolved_mode: str,
    store: ArtifactStore | None = None,
) -> dict[str, Any]:
    """阶段 1 / 1b / 2：文件检查、V2V 测量、抽关键帧。全程无模型调用。

    这是七个阶段函数中的第一个。抽出来的目的是让 worker 侧能把每个阶段包成独立的
    Temporal activity——Temporal 只能在 activity 边界上续跑，边界在哪就决定了崩溃后
    能从哪儿接着跑。

    产物（输入素材、关键帧、联系表）在这一步整批入对象存储，manifest 随返回值上浮，
    后续阶段据此在任意实例上还原。没配对象存储时 manifest 为空、一切照旧走本地盘。
    """
    emit(
        "tool", "integrity", "1 · 视频文件检查", "running",
        input_text=f"{source_label} 视频文件：{video_path.name}",
        detail="检查能否正常解码、有无黑帧/卡顿、时长是否异常",
    )
    metadata = inspect_video(video_path)
    integrity = check_integrity(video_path, metadata)
    _issue_txt = "未发现异常" if not integrity["issues"] else f"{len(integrity['issues'])} 项异常"
    emit(
        "tool", "integrity", "1 · 视频文件检查", "done",
        output_text=f"{metadata['width']}×{metadata['height']}，{metadata['fps']} 帧/秒，"
        f"时长 {metadata['duration_seconds']} 秒；{_issue_txt}（检查得分 {integrity['score']}/100）",
    )
    session.record_tool_step(
        stage="integrity",
        title="1 · 视频文件检查",
        detail="ffprobe/ffmpeg 解码、黑帧、卡顿、时长检查结果",
        payload={"metadata": metadata, "integrity": integrity},
    )

    # ---------------------------------------------------------------------
    # 1b · V2V 源视频对齐测量（确定性）
    #
    # 放在这里而不是模型环节之后，是为了守住「哪里被改了」必须由程序先算出来这条
    # 底线，模型只在既有测量上解释「改得对不对」。位置紧跟文件检查，因为它和
    # integrity 同性质 —— 纯 ffmpeg、可重放、结论进 hard gate。
    # 非 V2V 完全不执行，因此 T2V/I2V 的 trace 与耗时不变。
    # ---------------------------------------------------------------------
    source_video_path = source_video_of(case_files) if resolved_mode == "V2V" else None
    v2v_align: dict[str, Any] | None = None
    if source_video_path is not None:
        emit(
            "tool", "v2v_align", "1b · 对照源视频测量改动", "running",
            input_text=f"源视频：{source_video_path.name} ↔ 生成视频：{video_path.name}",
            detail="逐时刻算两段视频的画面相似度，定位「哪里被改了」——这是程序算的客观测量，不判对错",
        )
        v2v_align = v2v_align_profile(
            source_video_path, video_path, work_dir(case_files.slug, source, "v2v_align")
        )
        if v2v_align:
            _verdict_txt = {
                "unchanged": "与源视频几乎逐帧相同（编辑疑未执行）",
                "global_edit": "全片范围的改动（整体重绘特征）",
                "local_edit": "局部时间段的改动",
                "minor_diff": "仅轻微差异",
            }.get(str(v2v_align.get("verdict")), "")
            emit(
                "tool", "v2v_align", "1b · 对照源视频测量改动", "done",
                output_text=f"画面相似度均值 {v2v_align.get('ssim_mean')}（1.0=完全相同），"
                f"改动时刻占比 {v2v_align.get('changed_ratio')}；{_verdict_txt}；"
                f"改动集中区域：{'、'.join(v2v_align.get('grid_changed_cells') or []) or '无'}",
            )
        else:
            # 测量失败不中断：回退成纯模型判断，只是少了定位先验。
            emit(
                "tool", "v2v_align", "1b · 对照源视频测量改动", "done",
                output_text="测量未成功（源视频无法解码或格式不兼容），本次改由模型直接对照两段视频判断。",
            )
    if v2v_align:
        # 逐点 series 一起入 trace：这是「可审计」那条底线的要求 —— 剖面结论必须能被
        # 重新核对，而不只是一个孤立的均值。点数 = 时长 × 2，体积可忽略。
        session.record_tool_step(
            stage="v2v_align",
            title="1b · 对照源视频测量改动",
            detail=(
                f"ffmpeg SSIM 逐时刻对照（{v2v_align.get('sample_fps')} fps，"
                f"{v2v_align.get('sample_count')} 个采样点）：均值 {v2v_align.get('ssim_mean')}，"
                f"改动时刻占比 {v2v_align.get('changed_ratio')}，范围判定 {v2v_align.get('verdict')}"
            ),
            payload={
                "source_video": source_video_path.name if source_video_path else None,
                "align": v2v_align,
            },
        )

    emit(
        "tool", "sampling", "2 · 抽取关键画面", "running",
        input_text="解码视频，计算相邻帧的运动量",
        detail="动作越快的片段取帧越密，便于看清旋转、穿插等细节",
    )
    media_bundle = prepare_frame_bundle(case_files, metadata, source=source, video_path=video_path)
    motion_summary = media_bundle.get("motion", {})
    emit(
        "tool", "sampling", "2 · 抽取关键画面", "done",
        output_text=f"共取 {len(media_bundle['frames'])} 张画面，其中 {motion_summary.get('dense_count', 0)} 张在"
        f"快动作处加密；识别到 {len(motion_summary.get('windows', []))} 段快动作区间",
    )
    dense_count = motion_summary.get("dense_count", 0)
    strategy_label = "运动加密" if motion_summary.get("strategy") == "motion_aware" else "均匀回退"
    session.record_tool_step(
        stage="sampling",
        title="2 · 抽取关键画面",
        detail=(
            f"抽取 {len(media_bundle['frames'])} 关键帧（{strategy_label}，其中 {dense_count} 高运动加密帧），"
            f"高运动窗口 {len(motion_summary.get('windows', []))} 个；生成 6fps 联系表"
        ),
        payload={
            "frame_count": len(media_bundle["frames"]),
            "sampling_strategy": motion_summary.get("strategy"),
            "dense_count": dense_count,
            "motion_windows": motion_summary.get("windows", []),
            "shot_segments": media_bundle.get("shot_segments", []),
            "contact_sheet": media_bundle["contact_sheet_url"],
            "frames": [
                {"file": str(item["path"].name), "time": item.get("time"), "dense": item.get("dense")}
                for item in media_bundle.get("frame_meta", [])
            ],
        },
    )
    sampling_paths = list(media_bundle["frames"])
    if media_bundle.get("contact_path"):
        sampling_paths.append(Path(media_bundle["contact_path"]))
    artifacts = {
        "input": put_files(
            store, _input_asset_paths(case_files, video_path, source_video_path)
        ),
        "sampling": put_files(store, sampling_paths),
    }
    return {
        "metadata": metadata,
        "integrity": integrity,
        "source_video_path": source_video_path,
        "v2v_align": v2v_align,
        "media_bundle": media_bundle,
        "motion_summary": motion_summary,
        "artifacts": artifacts,
    }


def _stage_compile(
    *,
    emit: Callable[..., None],
    client: Any,
    case_files: CaseFiles,
    prompt_text: str,
    resolved_mode: str,
    store: ArtifactStore | None = None,
    artifacts: dict[str, Any] | None = None,
) -> dict[str, Any]:
    """阶段 3：把提示词拆成一条条可核对的要求（一次模型调用）。

    要读参考图，所以开工前先还原输入素材；缺了就直接失败——拿不到参考图却继续拆解，
    产出的要求会缺掉"与参考图一致"这一整类，报告看起来正常但实际漏判。
    """
    materialize(store, (artifacts or {}).get("input"), required=True)
    has_ref = any(r["reference_id"] == "input_image" for r in case_files.reference_assets)
    emit(
        "model", "compile", "3 · 拆解提示词要求", "running",
        input_text=f"提示词全文（{len(prompt_text)} 字）" + ("，附参考图" if has_ref else ""),
        detail="把这句话要表达的画面、动作、关系拆成一条条可核对的要求",
    )
    prompt_graph = compile_prompt_vlm(client, prompt_text, resolved_mode, case_files.reference_assets)
    emit(
        "model", "compile", "3 · 拆解提示词要求", "done",
        output_text=f"拆出 {len(prompt_graph.get('constraints', []))} 条可核对要求，"
        f"识别 {len(prompt_graph.get('entities', []))} 个主体",
    )
    return {"prompt_graph": prompt_graph, "has_ref": has_ref}


def _stage_observe(
    *,
    emit: Callable[..., None],
    client: Any,
    case_files: CaseFiles,
    video_path: Path,
    source_video_path: Path | None,
    prompt_text: str,
    resolved_mode: str,
    prompt_graph: dict[str, Any],
    metadata: dict[str, Any],
    integrity: dict[str, Any],
    media_bundle: dict[str, Any],
    motion_summary: dict[str, Any],
    v2v_align: dict[str, Any] | None,
    has_ref: bool,
    store: ArtifactStore | None = None,
    artifacts: dict[str, Any] | None = None,
) -> dict[str, Any]:
    """阶段 4：看视频、只记录可核查事实（一次模型调用）。

    同时返回 `native`（这一通道是否直接送完整视频），因为 4b 主判要按同一判断决定
    送视频还是送帧，两处必须一致。

    注意还原必须在 `_native_send_video` 之前：那个判断要读视频体积，文件不在盘上会
    被判成"不能送视频"，于是静悄悄降级成看几张帧——这正是最需要避免的静默退化。
    """
    _refs = artifacts or {}
    materialize(store, _refs.get("input"), required=True)
    materialize(store, _refs.get("sampling"), required=True)
    native = _native_send_video(client, video_path)
    _size_mb = video_path.stat().st_size / 1e6
    _vin = (
        f"完整视频（{_size_mb:.1f} MB）" if native
        else f"{min(8, len(media_bundle['frames']))} 张关键画面（视频过大，改用抽帧）"
    )
    emit(
        "model", "observe", "4 · 看视频、记录画面", "running",
        input_text=_vin + ("（含音轨）" if metadata.get("has_audio") else "（无音轨）")
        + ("，附参考图" if has_ref else ""),
        detail="把视频交给大模型观看和收听，只记录可核查事实与待复核时刻，不在观察阶段输出维度结论",
    )
    observations_bundle = GeminiObserver(client).observe(
        prompt=prompt_text,
        mode=resolved_mode,
        prompt_graph=prompt_graph,
        metadata=metadata,
        integrity=integrity,
        frames=media_bundle["frames"],
        frame_meta=media_bundle.get("frame_meta"),
        motion=motion_summary,
        shot_segments=media_bundle.get("shot_segments"),
        contact_path=media_bundle["contact_path"],
        references=case_files.reference_assets,
        video_path=video_path,
        source_video_path=source_video_path,
        align_profile=v2v_align,
    )
    _vmode = "看完整视频" if observations_bundle.get("_video_mode") == "native" else "看关键帧"
    focus = observations_bundle.get("focus_intervals") or []
    emit(
        "model", "observe", "4 · 看视频、记录画面", "done",
        output_text=f"（{_vmode}）记录 {len(observations_bundle.get('observations', []))} 条画面描述；"
        f"指出 {len(focus)} 处待后续复核",
    )

    # 接在观察之后、4b 之前：参考音频的对照事实要赶在事实底稿成形前进去。
    _compare_reference_audio(
        emit=emit,
        client=client,
        case_files=case_files,
        video_path=video_path,
        metadata=metadata,
        observations_bundle=observations_bundle,
    )

    return {
        "observations_bundle": observations_bundle,
        "native": native,
        "focus": focus,
    }


def _reference_audio_paths(case_files: CaseFiles) -> list[Path]:
    """case 里的参考音频文件。没有就返回空表（多数 case 如此）。"""
    out: list[Path] = []
    for ref in case_files.reference_assets or []:
        if not isinstance(ref, dict) or reference_kind(ref) != "audio":
            continue
        path = Path(str(ref.get("path") or ""))
        if path.is_file():
            out.append(path)
    return out


# 参考音频对照结论落到哪些子判据。
#
# 靠的是 observer_facts 行里既有的 ``subcriteria`` 字段——4b 按它把事实归到对应判据下，
# 所以不需要往 4b 的系统提示词里加解释。往共享提示词里加话的代价是所有 case 都要读那几句，
# 而带参考音频的 case 是少数。
#
# 两个都给：台词/配乐这类"Prompt 要求的声音"归指令遵循，"和参考素材像不像"归参考一致性。
_AUDIO_REFERENCE_SUBCRITERIA = (
    "instruction_requested_text_audio",
    "reference_input_fidelity",
)


def _append_reference_audio_fact(
    observations_bundle: dict[str, Any], compared: dict[str, Any]
) -> None:
    """把对照结论写成一条观察器事实，形状与 ``normalize_observer_facts`` 产出的完全一致。

    ID 必须续着 ``OBS-*`` 这个命名空间往下发：4b 的提示词告诉模型"观察器事实由程序分配
    稳定的 OBS-* ID"，而 4b 回引事实时也用这套 ID。塞一个别的形状的 ID 进同一个列表，
    模型看到的是一条来源不明的记录，回引它时下游按 OBS-* 索引也对不上。

    ``kind`` 用 ``observation`` 而不是 ``focus``：focus 是"观察器看不清、要求 4b 逐条
    表态"的疑点，会触发强制处置与审计校验；这里是一份已经查清的描述，供引用即可。

    事实正文里点明"这是由能直接听到音频的模型给出的"。4b 自己听不到独立音频（它跑在主
    模型上），不说这句它没有依据判断该不该采信一条关于音色的描述。
    """
    facts = observations_bundle.get("observer_facts")
    if not isinstance(facts, list):
        return

    # 续号而不是从 1 开始：与观察器事实同一命名空间，撞号会让两条事实互相顶掉。
    used = 0
    for row in facts:
        if not isinstance(row, dict):
            continue
        raw_id = str(row.get("fact_id") or "")
        if raw_id.startswith("OBS-") and raw_id[4:].isdigit():
            used = max(used, int(raw_id[4:]))

    parts = [
        (
            "参考音频对照（由能直接听到音频的模型独立完成，主判模型本身听不到独立音频，"
            "此结论可直接采信为听觉事实）："
        ),
        f"是否同一人声：{compared.get('same_voice') or 'unsure'}",
    ]
    for label, key in (
        ("台词逐字对照", "speech_match"),
        ("语调语速差异", "delivery_diff"),
        ("配乐音效差异", "music_diff"),
        ("总结", "summary"),
    ):
        value = str(compared.get(key) or "").strip()
        if value:
            parts.append(f"{label}：{value[:600]}")

    facts.append(
        {
            "fact_id": f"OBS-{used + 1:03d}",
            "kind": "observation",
            "time": "",
            "fact": "\n".join(parts),
            "confidence": "",
            "tags": ["audio", "reference_audio"],
            "subcriteria": list(_AUDIO_REFERENCE_SUBCRITERIA),
            "constraint_ids": [],
            "contradicted_constraint_ids": [],
        }
    )


def _compare_reference_audio(
    *,
    emit: Callable[..., None],
    client: Any,
    case_files: CaseFiles,
    video_path: Path,
    metadata: dict[str, Any],
    observations_bundle: dict[str, Any],
) -> None:
    """参考音频 ↔ 生成视频音轨的听觉对照，结论作为**事实**交给 4b。

    ## 为什么要单独一个阶段

    参考音频进不了 4b：4b 跑在主模型上，而主模型约一半的调用会把音频在服务端转成文字
    （见 config.AgentKConfig.audio_model），拿不到音色与语调——而"配音是否符合参考"正是
    要听这些。也进不了 4c：4c 的职责是判生成视频自身的音频**质量**，明确不评价声音内容
    是否符合 Prompt，把参考塞进去会让那三个子判据的口径变形。

    所以只能新开一段，跑在 audio_model 上。

    ## 为什么只产出事实、不产出判定

    判定权归 4b —— 它是唯一的事实底稿作者（见 native8_recheck 的模块说明）。本阶段把
    结论写成一条 ``observer_facts`` 行，4b 读到之后自己决定 instruction_requested_text_audio
    与参考一致性怎么判。这样既不用改合并逻辑，也不会出现两个作者对同一维度各写一份。

    行的 ``kind`` 刻意不用 ``focus``：focus 行会触发 4b 的强制逐条表态与审计校验，
    那套机制是为"观察器看不清的时刻"设计的，而这里是一份已经查清的对照描述。

    ## 失败与降级

    对照失败不阻断整条质检：拿不到这份事实，4b 那边就只剩一句"本次听不到这段音频"，
    结论会是 not_observable，而不是错的结论。模型自报没听到时同样丢掉这份描述——
    一份凭空编出来的"音色一致"比没有更坏。
    """
    references = _reference_audio_paths(case_files)
    if not references:
        return
    if not _native_send_video(client, video_path):
        # 送不了视频就没有对照的另一半，只发参考音频毫无意义。
        return

    title = "4a · 参考音频对照"
    emit(
        "model", "audio_reference", title, "running",
        input_text=f"参考音频 {len(references)} 段 + 生成视频音轨",
        detail="听觉对照人声、台词、语调与配乐，只记录异同，不下判定——判定由 4b 做",
    )

    parts: list[dict[str, Any]] = [
        {"text": "以下是参考音频，请听它："},
    ]
    for path in references:
        parts.append({"text": f"参考音频【{path.name}】："})
        parts.append(audio_part(path))
    parts.append({"text": "以下是生成视频，请听它的音轨并与上面的参考音频对照："})
    parts.append(video_part(video_path, detail=False))

    try:
        compared = client.generate_json(
            parts,
            system_instruction=AUDIO_REFERENCE_SYSTEM,
            stage="audio_reference",
            title=title,
        )
    except Exception as error:  # noqa: BLE001 - 旁路证据，不该拖垮整条质检
        emit("model", "audio_reference", title, "error", output_text=str(error)[:140])
        return

    if not isinstance(compared, dict):
        emit("model", "audio_reference", title, "error", output_text="返回不是 JSON 对象")
        return

    # 两边只要有一边没真听到，这份对照就没有听觉依据。
    deaf = [
        name
        for key, name in (("can_hear_reference", "参考音频"), ("can_hear_video_audio", "视频音轨"))
        if compared.get(key) is False
    ]
    if deaf:
        observations_bundle["audio_reference_not_heard"] = deaf
        emit(
            "model", "audio_reference", title, "done",
            output_text=f"模型自报未听到{'、'.join(deaf)}，本次不产出对照事实",
            detail=AUDIO_NOT_HEARD_REASON,
        )
        return

    observations_bundle["audio_reference"] = compared
    _append_reference_audio_fact(observations_bundle, compared)

    emit(
        "model", "audio_reference", title, "done",
        output_text=f"同一人声：{compared.get('same_voice')}｜"
        + str(compared.get("summary") or "")[:120],
    )


def _stage_scan(
    *,
    emit: Callable[..., None],
    session: TraceSession,
    client: Any,
    provider: str,
    case_files: CaseFiles,
    source: str,
    video_path: Path,
    source_video_path: Path | None,
    prompt: dict[str, Any],
    prompt_text: str,
    prompt_graph: dict[str, Any],
    resolved_mode: str,
    metadata: dict[str, Any],
    media_bundle: dict[str, Any],
    v2v_align: dict[str, Any] | None,
    observations_bundle: dict[str, Any],
    focus: list[Any],
    native: bool,
    store: ArtifactStore | None = None,
    artifacts: dict[str, Any] | None = None,
) -> dict[str, Any]:
    """阶段 4b：原生八维主判。这是唯一的事实底稿，失败必须整次重试。

    只有这一套协议：不允许在失败后换一套旧判据继续，否则报告会在两种口径间漂移。

    自己抽出的高清帧要上传：4c 复核和证据环节都靠它们复用，换实例后没有这批帧就只能
    重抽，而重抽出来的帧未必与 4b 判定时看的是同一张。
    """
    _refs = artifacts or {}
    materialize(store, _refs.get("input"), required=True)
    materialize(store, _refs.get("sampling"), required=True)
    _shots = media_bundle.get("shot_segments") or []
    _send_scan_video = UNIFIED_SCAN_VIDEO and native
    _scan_frames_budget, _scan_refs, _budget_note = _plan_scan_media(
        client,
        references=case_files.reference_assets,
        prompt_text=prompt_text,
        send_video=_send_scan_video,
        has_source_video=source_video_path is not None,
        is_v2v=source_video_path is not None,
    )
    if _scan_frames_budget <= 0:
        raise RuntimeError(
            f"{provider} 通道的素材上限（{getattr(client, 'max_media', None)} 件）"
            "被非帧素材占满，4b 拿不到任何高清帧，无法执行原生八维主判"
        )
    emit(
        "model", "unified_scan", "4b · 原生八维主判", "running",
        input_text=f"整段视频 + 全片至多 {_scan_frames_budget} 张带镜头号高清帧"
        f"（长边 ≤{UNIFIED_SCAN_MAX_SIDE}px），按固定八维与全部子判据逐项检查"
        f"（检测到 {len(_shots) or 1} 个镜头）"
        + (
            f"；受 {provider} 通道素材上限约束，帧数由 {UNIFIED_SCAN_FRAMES} 降为 {_scan_frames_budget}"
            if _budget_note else ""
        ),
        detail="输出原子 issue、固定八维子判据和完整指令 checklist；不输出旧叶子、severity 或分数",
    )
    if _budget_note:
        # 降载写进 trace：帧数变化会实打实影响 not_observable 的数量，
        # 事后比对两次运行时必须能看出这次是按几帧判的。
        session.record_tool_step(
            stage="unified_scan_budget",
            title="4b · 素材预算协商",
            detail=(
                f"{provider} 通道素材上限 {_budget_note['cap']} 件，"
                f"非帧素材占 {_budget_note['reserved']} 件，"
                f"高清帧 {_budget_note['requested_frames']} → {_budget_note['granted_frames']} 张"
                + (
                    f"；为保住帧数底线，V2V 弃送 {_budget_note['dropped_image_references']} 张参考图"
                    "（源视频已含该信息）"
                    if _budget_note.get("dropped_image_references") else ""
                )
            ),
            payload=_budget_note,
        )
    try:
        _uni = GeminiZoomInspector(client).scan_unified(
            video_path=video_path,
            frame_dir=work_dir(case_files.slug, source, "unified_scan", session.run_id),
            metadata=metadata,
            max_frames=_scan_frames_budget,
            reference_assets=_scan_refs,
            shot_segments=_shots,
            prompt=prompt,
            prompt_text=prompt_text,
            prompt_graph=prompt_graph,
            send_video=_send_scan_video,
            mode=resolved_mode,
            source_video_path=source_video_path,
            align_profile=v2v_align,
            focus_intervals=[row for row in focus if isinstance(row, dict)],
            observer_facts=[
                row for row in (observations_bundle.get("observer_facts") or [])
                if isinstance(row, dict)
            ],
        )
    except Exception as _err:  # noqa: BLE001
        emit(
            "model", "unified_scan", "4b · 原生八维主判", "error",
            output_text=str(_err)[:160],
        )
        raise
    if not _uni:
        raise RuntimeError("4b 原生八维主判没有返回有效 JSON")
    _subcriteria = [
        sub
        for dim in (_uni.get("rubric") or []) if isinstance(dim, dict)
        for sub in (dim.get("subcriteria") or []) if isinstance(sub, dict)
    ]
    _violated_4b = sum(sub.get("verdict") == "violated" for sub in _subcriteria)
    _unknown_4b = sum(sub.get("verdict") == "not_observable" for sub in _subcriteria)
    _instruction_rows = _uni.get("instruction_checklist") or []
    _unresponded_4b = sum(item.get("responded") is False for item in _instruction_rows)
    emit(
        "model", "unified_scan", "4b · 原生八维主判", "done",
        output_text=f"固定八维/28 个子判据：{_violated_4b} 个违反、"
        f"{_unknown_4b} 个待复核；指令 {len(_instruction_rows)} 条中 "
        f"{_unresponded_4b} 条未响应",
    )
    return {
        "uni": _uni,
        "artifacts": {
            "scan_frames": put_files(store, _frame_row_paths(_uni.get("_frames")))
        },
    }


def _stage_recheck(
    *,
    emit: Callable[..., None],
    client: Any,
    video_path: Path,
    prompt_text: str,
    prompt_graph: dict[str, Any],
    metadata: dict[str, Any],
    observations_bundle: dict[str, Any],
    v2v_align: dict[str, Any] | None,
    uni: dict[str, Any],
    focus: list[Any],
    store: ArtifactStore | None = None,
    artifacts: dict[str, Any] | None = None,
) -> list[dict[str, Any]]:
    """阶段 4c：按 4b 留下的候选疑点做增量取证，外加一次音频独立复听。

    这一步只允许在 4b 的候选 issue 上确认或推翻，不能新开维度结论——4b 是唯一事实
    底稿。任何一次复核调用失败都不阻断整体，失败的候选降级为"保留 4b 主判"。

    结论直接写进 `observations_bundle`（native8_recheck / native8_review_funnel /
    audio_recheck 等键）；返回候选列表供后续聚合判断复核漏斗是否走满。

    还原 4b 的高清帧只按 best-effort 处理：候选帧本来就按"文件还在不在"过滤，缺了就
    少送几张证据，与本地盘被清理过的既有行为一致，不该因此让整次质检失败。
    """
    _refs = artifacts or {}
    materialize(store, _refs.get("input"), required=True)
    materialize(store, _refs.get("scan_frames"))
    _uni = uni
    _focus = focus
    # =============== 4c · 疑点增量复核 ===============
    _recheck_candidates = build_native_recheck_candidates(_uni, _focus)
    observations_bundle["native8_review_funnel"] = {
        "candidate_count": len(_recheck_candidates),
        "candidates": deepcopy(_recheck_candidates),
        "recheck_available": False,
        "confirmed_count": 0,
        "rejected_count": 0,
        "unresolved_count": 0,
    }
    _can_native_recheck = (
        bool(_recheck_candidates)
        and TARGETED_RECHECK
        and _native_send_video(client, video_path)
    )
    if _can_native_recheck:
        observations_bundle["native8_review_funnel"]["recheck_available"] = True
        emit(
            "model", "native8_recheck", "4c · 原生八维疑点复核", "running",
            input_text=f"完整视频独立复核 {len(_recheck_candidates)} 个候选子判据",
            detail="按各自维度规则确认或推翻，不跨维外推",
        )
        _candidate_ids = {
            str(issue_id)
            for item in _recheck_candidates for issue_id in (item.get("issue_ids") or [])
        }
        _candidate_issue_rows = [
            item for item in (_uni.get("issues") or [])
            if isinstance(item, dict) and str(item.get("issue_id")) in _candidate_ids
        ]
        _recheck_payload = {
            "prompt": prompt_text,
            "prompt_requirements": prompt_graph.get("constraints") or [],
            "candidates": _recheck_candidates,
            "candidate_issues": _candidate_issue_rows,
            "observer_facts": {
                "observations": (observations_bundle.get("observations") or [])[:24],
                "reference_facts": (observations_bundle.get("reference_facts") or [])[:16],
                "focus_intervals": (observations_bundle.get("focus_intervals") or [])[:16],
            },
            "story_event_chain": _uni.get("story_event_chain") or [],
            "continuity_checks": _uni.get("continuity_checks") or [],
            "focus_intervals": [
                {
                    "time": row.get("frame_time") or row.get("time"),
                    "fact_types": row.get("fact_types") or [],
                }
                for row in _focus if isinstance(row, dict)
            ],
        }
        _recheck_parts: list[dict[str, Any]] = [
            {"text": json.dumps(_recheck_payload, ensure_ascii=False, indent=2)},
            video_part(video_path, detail=False),
        ]
        # 视频输入用于核对连续运动；候选附近的原生帧用于核对手指、面部、
        # 文字等细节。两种证据同时提供，避免低细节视频采样制造结构误判。
        _candidate_times = [
            float(frame.get("time"))
            for issue in _candidate_issue_rows
            for frame in (issue.get("evidence_frames") or [])
            if isinstance(frame, dict) and isinstance(frame.get("time"), (int, float))
        ]
        _scan_frames = [
            row for row in (_uni.get("_frames") or [])
            if isinstance(row, dict) and Path(str(row.get("path") or "")).exists()
        ]
        _selected_paths: set[str] = set()
        for _time in _candidate_times:
            if len(_selected_paths) >= 12 or not _scan_frames:
                break
            _frame = min(_scan_frames, key=lambda row: abs(float(row.get("time") or 0) - _time))
            _path = str(_frame.get("path") or "")
            if _path in _selected_paths:
                continue
            _selected_paths.add(_path)
            _recheck_parts.append({"text": f"候选高清帧 t={float(_frame.get('time') or 0):.2f}s："})
            _recheck_parts.append({"inline_data": encode_file(Path(_path))})
        try:
            _native_recheck = client.generate_json(
                _recheck_parts,
                system_instruction=NATIVE8_RECHECK_SYSTEM,
                stage="verifier",
                title="4c · 原生八维疑点复核",
            )
        except Exception as _err:  # noqa: BLE001
            emit("model", "native8_recheck", "4c · 原生八维疑点复核", "error", output_text=str(_err)[:140])
            observations_bundle["native8_recheck"] = {
                "confirmed_issues": [],
                "rejected_candidate_issue_ids": [],
                "unresolved_candidates": [{
                    "dimension_id": item["dimension_id"],
                    "key": item["key"],
                    "issue_ids": item.get("issue_ids") or [],
                    "reason": "4c 调用失败，保留 4b 主判",
                } for item in _recheck_candidates],
            }
            observations_bundle["native8_review_funnel"]["unresolved_count"] = len(_recheck_candidates)
        else:
            if not isinstance(_native_recheck, dict):
                _native_recheck = {}
            _native_recheck = {
                "confirmed_issues": [
                    item for item in (_native_recheck.get("confirmed_issues") or [])
                    if isinstance(item, dict)
                ],
                "rejected_candidate_issue_ids": [
                    item for item in (_native_recheck.get("rejected_candidate_issue_ids") or [])
                    if isinstance(item, (str, dict))
                ],
                "unresolved_candidates": [
                    item for item in (_native_recheck.get("unresolved_candidates") or [])
                    if isinstance(item, dict)
                ],
            }
            observations_bundle["native8_recheck"] = _native_recheck
            observations_bundle["native8_review_funnel"].update({
                "confirmed_count": len(_native_recheck["confirmed_issues"]),
                "rejected_count": len(_native_recheck["rejected_candidate_issue_ids"]),
                "unresolved_count": len(_native_recheck["unresolved_candidates"]),
            })
            emit(
                "model", "native8_recheck", "4c · 原生八维疑点复核", "done",
                output_text=(
                    f"确认 {len(_native_recheck['confirmed_issues'])} 个原子问题，"
                    f"按ID拒绝 {len(_native_recheck['rejected_candidate_issue_ids'])} 个，"
                    f"保留 {len(_native_recheck['unresolved_candidates'])} 个待复核"
                ),
            )
    elif _recheck_candidates:
        observations_bundle["native8_recheck"] = {
            "confirmed_issues": [],
            "rejected_candidate_issue_ids": [],
            "unresolved_candidates": [{
                "dimension_id": item["dimension_id"],
                "key": item["key"],
                "issue_ids": item.get("issue_ids") or [],
                "reason": "当前输入条件无法执行4c增量取证，保留 4b 主判",
            } for item in _recheck_candidates],
        }
        observations_bundle["native8_review_funnel"]["unresolved_count"] = len(_recheck_candidates)

    # 4c · 音频质量独立复听：观察器容易漏掉底噪/短暂失真/口型偏移，
    # 因此有音轨时固定复听一次，不再用观察器自身置信度决定是否启动。
    if (
        AUDIO_RECHECK
        and bool(metadata.get("has_audio"))
        and _native_send_video(client, video_path)
    ):
        emit(
            "model", "audio", "4c · 音频独立复听", "running",
            input_text="独立复听整段音轨，判断音质、音画同步与音频一致性",
            detail="去掉其他维度的注意力稀释，补查底噪、失真、音量、音色漂移和口型偏移",
        )
        _audio_profile = audio_signal_profile(video_path)
        try:
            _ar = client.generate_json(
                [
                    {"text": "请判断音质、音画同步与音频一致性。程序侧信号测量仅用于定位，"
                             f"不能单独证明质量问题：{json.dumps(_audio_profile, ensure_ascii=False)}"},
                    video_part(video_path, detail=False),
                ],
                system_instruction=AUDIO_RECHECK_SYSTEM,
                stage="audio",
                title="4c · 音频独立复听",
            )
        except Exception as _err:  # noqa: BLE001 - 复听失败不阻断
            emit("model", "audio", "4c · 音频独立复听", "error", output_text=str(_err)[:140])
        else:
            # 平台把音频在服务端转成文字再交给模型时，这次复听就没有听觉依据了
            # （见 normalize_audio_recheck）。它是静默的，所以必须自己喊出来：
            # 不喊的话报告里只是三项都"未核实"，看着像视频本身没什么可判。
            if isinstance(_ar, dict) and _ar.get("can_hear_audio") is False:
                observations_bundle["audio_not_heard"] = True
                emit(
                    "model", "audio", "4c · 音频独立复听", "running",
                    output_text="模型自报未收到可听音频，三项音频判据记未核实",
                    detail=AUDIO_NOT_HEARD_REASON,
                )
            _ar = normalize_audio_recheck(_ar)
            _violated_audio = {
                key: value for key, value in _ar.items()
                if value.get("verdict") == "violated"
            }
            if _violated_audio:
                try:
                    _audio_confirm = client.generate_json(
                        [
                            {"text": json.dumps({
                                "first_findings": _violated_audio,
                                "signal_profile": _audio_profile,
                            }, ensure_ascii=False, indent=2)},
                            video_part(video_path, detail=False),
                        ],
                        system_instruction=AUDIO_CONFIRM_SYSTEM,
                        stage="audio",
                        title="4c · 音频反证复核",
                    )
                except Exception as _err:  # noqa: BLE001
                    emit("model", "audio_confirm", "4c · 音频反证复核", "error", output_text=str(_err)[:140])
                else:
                    _ar = merge_audio_confirmation(_ar, _audio_confirm)
            observations_bundle["audio_recheck"] = _ar or None
            observations_bundle["audio_signal_profile"] = _audio_profile
            emit(
                "model", "audio", "4c · 音频独立复听", "done",
                output_text=" · ".join(
                    f"{key}={item.get('verdict')} ({item.get('duration_seconds')}s)"
                    for key, item in _ar.items()
                ) or "未得到可用音频结论",
            )
    if v2v_align:
        observations_bundle["v2v_align"] = v2v_align
    return _recheck_candidates


def _stage_aggregate(
    *,
    case_files: CaseFiles,
    resolved_mode: str,
    metadata: dict[str, Any],
    prompt_graph: dict[str, Any],
    observations_bundle: dict[str, Any],
    verdict_bundle: dict[str, Any],
    recheck_candidates: list[dict[str, Any]],
    v2v_align: dict[str, Any] | None,
    use_vlm: bool,
) -> dict[str, Any]:
    """阶段 5：把 4b/4c 的事实聚合成八维三档。全程由代码执行，不调模型。

    这一步是"证据到结论"的唯一通道：checklist 执行、引用与事实校验、增量复核的
    落账、V2V 对齐落账、确定性三档，都在这里完成。放在模型之后单独成段，是为了让
    结论怎么来的可以被逐条重放核对。
    """
    _recheck_candidates = recheck_candidates
    observations = [
        item for item in (observations_bundle.get("observations") or [])
        if isinstance(item, dict)
    ]
    if verdict_bundle.get("schema_version") != NATIVE8_SCHEMA_VERSION:
        raise ValueError("4b must return schema_version=native8-v1")
    # 4b 是事实底稿。4c 只按候选 issue 增量确认或拒绝，不能整维覆盖；
    # 随后由程序执行 checklist、引用/事实校验与确定性三档。
    _native_scan = observations_bundle.get("native8_scan") or {}
    _recheck_audit = apply_native_incremental_recheck(
        verdict_bundle,
        observations_bundle.get("native8_recheck"),
        _recheck_candidates if use_vlm else [],
        [item for item in (prompt_graph.get("constraints") or []) if isinstance(item, dict)],
        resolved_mode,
    )
    _audio_audit = apply_native_audio_increment(
        verdict_bundle, observations_bundle.get("audio_recheck")
    )
    if resolved_mode == "V2V":
        apply_native_v2v_alignment(
            verdict_bundle, v2v_align, has_edit_ops=bool(prompt_graph.get("edit_ops"))
        )
    # checklist 是观看前固化的逐约束事实源；4c 无权修改它或 core/detail。
    enforce_native_instruction_checklist(
        verdict_bundle,
        _native_scan,
        [item for item in (prompt_graph.get("constraints") or []) if isinstance(item, dict)],
    )
    collapse_instruction_constraint_issues(verdict_bundle)
    native_draft = finalize_native8_result(
        verdict_bundle,
        mode=resolved_mode,
        has_reference=bool(case_files.reference_assets),
        has_audio=bool(metadata.get("has_audio")),
    )
    _review_funnel = deepcopy(observations_bundle.get("native8_review_funnel") or {})
    if _review_funnel:
        _review_funnel.update({
            "incremental_apply": _recheck_audit,
            "audio_incremental_apply": _audio_audit,
            "admitted_issue_count": len(native_draft.get("issues") or []),
            "gate_rejection_count": len(native_draft.get("review_required") or []),
            "gate_rejections": deepcopy(native_draft.get("review_required") or []),
        })
    rubric_violations = sum(
        sub.get("verdict") == "violated"
        for dimension in native_draft["rubric"]
        for sub in dimension["subcriteria"]
    )
    native_draft_errors = validate_native8_report(native_draft)
    if native_draft_errors:
        raise ValueError("native8 evidence draft failed: " + "; ".join(native_draft_errors))

    return {
        "observations": observations,
        "native_draft": native_draft,
        "review_funnel": _review_funnel,
        "rubric_violations": rubric_violations,
    }


def _stage_evidence(
    *,
    emit: Callable[..., None],
    session: TraceSession,
    client: Any | None,
    case_files: CaseFiles,
    source: str,
    video_path: Path,
    prompt: dict[str, Any],
    native_draft: dict[str, Any],
    observations_bundle: dict[str, Any],
    use_vlm: bool,
    store: ArtifactStore | None = None,
    artifacts: dict[str, Any] | None = None,
) -> dict[str, Any]:
    """阶段 6：给每条已成立的问题配定位与证据卡。

    这一步是 best-effort：定位或截图失败只降级为"没有框"，不能推翻已经成立的结论，
    因此内部所有异常都就地吞掉并 emit 出去，不向上抛。

    产出的证据图要上传：报告阶段要把它们内嵌进 HTML，那一步很可能在另一台实例上跑。
    """
    _refs = artifacts or {}
    materialize(store, _refs.get("input"), required=True)
    materialize(store, _refs.get("scan_frames"))
    # Grounding 只按最终 ISSUE-ID 定位，只补坐标，不参与事实或三档判定。
    grounding_bundle: dict[str, Any] = {"grounding": [], "frames": [], "note": None}
    if GROUNDING_PASS and not SKIP_EVIDENCE and use_vlm and client is not None:
        _targets = GeminiGroundingLocator.build_native_targets(native_draft)
        if _targets:
            emit(
                "tool", "grounding", "· 问题画面定位", "running",
                input_text=f"{len(_targets)} 条待定位问题",
                detail="在原生帧上给每条问题框出具体位置；整片级属性（运镜、画幅、全局偏色、音频）不给框",
            )
            try:
                grounding_bundle = GeminiGroundingLocator(client).locate(
                    video_path=video_path,
                    # 独立子目录：extract_native_frames 进目录会先清掉 zoom_*.jpg，
                    # 与证据帧共用目录会互相删帧。
                    frame_dir=work_dir(case_files.slug, source, "grounding", session.run_id),
                    targets=_targets,
                    prompt=prompt,
                )
            except Exception as error:  # noqa: BLE001 - grounding is best-effort
                emit("tool", "grounding", "· 问题画面定位", "error", output_text=str(error)[:160])
            else:
                _located = sum(
                    1 for g in grounding_bundle["grounding"] if g.get("locatable") and g.get("bbox")
                )
                emit(
                    "tool", "grounding", "· 问题画面定位", "done",
                    output_text=f"{len(_targets)} 条问题里 {_located} 条定位到了具体画面位置，"
                    f"其余为整片级属性不给框",
                )

    # 每个最终 issue 只生成一张证据卡；按 run_id 隔离，重复运行不会覆盖历史红框。
    if SKIP_EVIDENCE:
        evidence_cards = []
    else:
        emit(
            "tool", "evidence", "· 标注问题定位帧", "running",
            input_text=f"{len(native_draft.get('issues') or [])} 个原子问题",
            detail="按时间戳抽原生分辨率帧，能定位的画红框，便于确认是真实问题还是模型幻觉",
        )
        try:
            evidence_cards = build_native_evidence(
                video_path=video_path,
                slug=case_files.slug,
                source=source,
                run_id=session.run_id,
                issues=native_draft.get("issues") or [],
                observer_bundle=observations_bundle,
                grounding=grounding_bundle.get("grounding"),
            )
        except Exception as error:  # noqa: BLE001 - evidence is best-effort
            evidence_cards = []
            emit("tool", "evidence", "· 标注问题定位帧", "error", output_text=str(error)[:160])
        else:
            _boxed = sum(1 for c in evidence_cards if c.get("has_box"))
            emit(
                "tool", "evidence", "· 标注问题定位帧", "done",
                output_text=f"为 {len(evidence_cards)} 条问题配了定位截图，其中 {_boxed} 条画了红框",
            )

    return {
        "grounding_bundle": grounding_bundle,
        "evidence_cards": evidence_cards,
        "artifacts": {
            "evidence": put_files(store, _evidence_image_paths(evidence_cards))
        },
    }


def _stage_report(
    *,
    emit: Callable[..., None],
    session: TraceSession,
    client: Any | None,
    case_files: CaseFiles,
    source: str,
    source_label: str,
    resolved_mode: str,
    prompt: dict[str, Any],
    prompt_graph: dict[str, Any],
    metadata: dict[str, Any],
    integrity: dict[str, Any],
    media_bundle: dict[str, Any],
    motion_summary: dict[str, Any],
    source_video_path: Path | None,
    v2v_align: dict[str, Any] | None,
    observations: list[dict[str, Any]],
    observations_bundle: dict[str, Any],
    native_draft: dict[str, Any],
    review_funnel: dict[str, Any],
    rubric_violations: int,
    evidence_cards: list[dict[str, Any]],
    grounding_bundle: dict[str, Any],
    engine: str,
    vlm_error: str | None,
    cache_path: Path,
    store: ArtifactStore | None = None,
    artifacts: dict[str, Any] | None = None,
) -> dict[str, Any]:
    """阶段 7：组装报告、落 trace、生成 HTML、写结果缓存。

    这一步不再产生任何新结论，只把前面各阶段的事实拼成对外契约并做最后一轮合约校验
    （`validate_native8_report`、issue ID 未被证据环节改动）。HTML 与缓存都是
    best-effort，失败只在报告里留下错误信息，不影响已经成立的结论。

    HTML 要把帧图、联系表、证据图全部内嵌成 base64，所以先把这三批产物还原到本地。
    按 best-effort 处理：图缺了报告少几张图，不该让已经算完的结论发不出去。
    """
    _refs = artifacts or {}
    _restored = {
        group: materialize(store, _refs.get(group))
        for group in ("input", "sampling", "scan_frames", "evidence")
    }
    _review_funnel = review_funnel
    live = engine in _PROVIDER_CLIENTS
    engine_label = "大模型" if live else "离线"
    audit_trace = [
        {
            "stage": "1 · 视频文件检查",
            "status": "完成",
            "detail": f"检查得分 {integrity['score']}/100；{'未发现异常' if not integrity['issues'] else str(len(integrity['issues'])) + ' 项异常'}。",
        },
        {
            "stage": "2 · 抽取关键画面",
            "status": "完成",
            "detail": (
                f"取 {len(media_bundle['frames'])} 张画面，含 {motion_summary.get('dense_count', 0)} 张快动作加密，"
                f"{len(motion_summary.get('windows', []))} 段快动作区间。"
            ),
        },
        {
            "stage": "3 · 拆解提示词要求",
            "status": "完成",
            "detail": f"{len(prompt_graph.get('entities', []))} 个主体，{len(prompt_graph.get('constraints', []))} 条可核对要求。",
        },
        {
            "stage": "4 · 看视频、记录画面",
            "status": engine_label,
            "detail": f"记录 {len(observations)} 条画面描述，并按疑点触发专项复核。",
        },
        {
            "stage": "4b/4c · 原生八维事实形成",
            "status": engine_label,
            "detail": (
                f"4b 主判给出 {rubric_violations} 个有效问题子判据；"
                f"4c 仅按候选 issue 增量取证，未执行完整八维二次裁决。"
            ),
        },
        {
            "stage": "6 · 原生八维事实聚合",
            "status": "完成",
            "detail": "由代码按结构化事实生成八维三档，不计算综合分或整体质量状态。",
        },
    ]
    if vlm_error:
        audit_trace.insert(
            3,
            {
                "stage": "· 大模型调用失败",
                "status": "已改用离线",
                "detail": f"大模型不可用，已改用离线占位：{vlm_error[:240]}",
            },
        )

    session.record_tool_step(
        stage="aggregate",
        title="6 · 原生八维事实聚合",
        detail=f"rubric_candidates={rubric_violations}",
        payload={
            "schema_version": NATIVE8_SCHEMA_VERSION,
            "analysis_status": "complete",
            "rubric_candidates": rubric_violations,
        },
    )
    saved_path = session.save(
        extra={
            "schema_version": NATIVE8_SCHEMA_VERSION,
            "analysis_status": "complete",
            "vlm_error": vlm_error,
            "engine": engine,
        }
    )

    enc = quote(case_files.case_id, safe="")
    is_upload = case_files.collection == "uploads"
    if is_upload:
        # 上传流没有 case 目录，媒体按内容寻址的 slug + 素材标签取用。
        slug_enc = quote(case_files.slug, safe="")
        media = {
            "video_url": f"./api/upload_media?slug={slug_enc}&asset=video",
            "input_image_url": (
                f"./api/upload_media?slug={slug_enc}&asset=reference&ref_id="
                + quote(str(case_files.reference_assets[0]["reference_id"]), safe="")
            ) if case_files.input_image_path and case_files.reference_assets else None,
            "contact_sheet_url": media_bundle["contact_sheet_url"],
            "reference_urls": [
                {
                    "reference_id": ref["reference_id"],
                    "kind": reference_kind(ref),
                    "label": reference_label(ref),
                    "url": f"./api/upload_media?slug={slug_enc}&asset=reference"
                           f"&ref_id={quote(str(ref['reference_id']), safe='')}",
                    "expected_role": ref.get("expected_role"),
                }
                for ref in case_files.reference_assets
            ],
        }
        # 上传流的源视频走 asset=reference（它就在 reference_assets 里），
        # 不像身份寻址那样另有 asset=source_video 别名。
        if source_video_path is not None:
            _src_ref = next(
                (
                    r for r in case_files.reference_assets
                    if reference_kind(r) == "video" and Path(r.get("path", "")) == source_video_path
                ),
                None,
            )
            media["source_video_url"] = (
                f"./api/upload_media?slug={slug_enc}&asset=reference"
                f"&ref_id={quote(str(_src_ref['reference_id']), safe='')}"
                if _src_ref else None
            )
        else:
            media["source_video_url"] = None
    else:
        video_asset = "seedance" if source == "seedance" else "video"
        media = {
            "video_url": f"./api/case-media?case_id={enc}&asset={video_asset}",
            "input_image_url": (
                f"./api/case-media?case_id={enc}&asset=input_image" if case_files.input_image_path else None
            ),
            "source_video_url": (
                f"./api/case-media?case_id={enc}&asset=source_video"
                if source_video_path is not None else None
            ),
            "contact_sheet_url": media_bundle["contact_sheet_url"],
            "reference_urls": [
                {
                    "reference_id": ref["reference_id"],
                    "kind": reference_kind(ref),
                    "label": reference_label(ref),
                    "url": f"./api/case-media?case_id={enc}&asset=reference&ref_id={ref['reference_id']}",
                    "expected_role": ref.get("expected_role"),
                }
                for ref in case_files.reference_assets
            ],
        }

    report = {
        "case": {
            "case_id": case_files.case_id,
            "collection": case_files.collection,
            "case_name": case_files.case_name,
            "label": CURATED_CASES.get(case_files.case_name, {}).get("label", case_files.case_name),
            "focus": CURATED_CASES.get(case_files.case_name, {}).get(
                "focus", _collection_focus(case_files.collection)
            ),
            "mode": resolved_mode,
        },
        "source": source,
        "source_label": source_label,
        "from_cache": False,
        "mock_mode": not live,
        "engine": engine,
        "vlm_error": vlm_error,
        "prompt": prompt,
        "prompt_requirements": [
            {
                "requirement_id": str(item.get("id") or item.get("constraint_id") or ""),
                "category": str(item.get("category") or "其他"),
                "statement": str(item.get("statement") or item.get("requirement") or ""),
                "source": str(item.get("source") or "prompt"),
                "requirement_importance": (
                    "core" if str(item.get("requirement_importance") or item.get("importance") or "detail").lower()
                    in {"core", "hard"} else "detail"
                ),
            }
            for item in (prompt_graph.get("constraints") or []) if isinstance(item, dict)
        ],
        "metadata": metadata,
        "integrity": integrity,
        "media": media,
        "sampling": {
            "strategy": motion_summary.get("strategy"),
            "dense_count": motion_summary.get("dense_count", 0),
            "windows": motion_summary.get("windows", []),
            "shot_segments": media_bundle.get("shot_segments", []),
            "frames": [
                {"time": item.get("time"), "dense": item.get("dense")}
                for item in media_bundle.get("frame_meta", [])
            ],
        },
        "observations": observations,
        "zoom_findings": [z for z in (observations_bundle.get("zoom_findings") or []) if isinstance(z, dict)],
        "zoom_frames": observations_bundle.get("_zoom_frames") or [],
        "observation_extras": {
            "audio_recheck": observations_bundle.get("audio_recheck"),
            "audio_reference": observations_bundle.get("audio_reference"),
            # 两个"没听到"标记也带进报告。它们是排查依据：音频判据为什么全是未核实，
            # 答案只在这里，落在 bundle 里没人看得到。
            "audio_not_heard": observations_bundle.get("audio_not_heard"),
            "audio_reference_not_heard": observations_bundle.get("audio_reference_not_heard"),
            "focus_intervals": [f for f in (observations_bundle.get("focus_intervals") or []) if isinstance(f, dict)],
        },
        "native8_scan": observations_bundle.get("native8_scan"),
        "native8_recheck": observations_bundle.get("native8_recheck"),
        "v2v_align": _v2v_align_report(v2v_align),
        "schema_version": native_draft["schema_version"],
        "analysis_status": native_draft["analysis_status"],
        "issues": native_draft["issues"],
        "rubric": native_draft["rubric"],
        "review_required": native_draft["review_required"],
        "review_candidates": native_draft["review_candidates"],
        "review_funnel": _review_funnel,
        "evidence": evidence_cards,
        "grounding": grounding_bundle.get("grounding") or [],
        "cost": build_cost(client.usage if client is not None else []),
        "audit_trace": audit_trace,
        "llm_trace": session.public_view(),
        "trace_saved_path": str(saved_path),
    }

    native_errors = validate_native8_report(report)
    if native_errors:
        raise ValueError("native8 contract failed: " + "; ".join(native_errors))
    if [row.get("issue_id") for row in report.get("issues") or []] != [
        row.get("issue_id") for row in native_draft.get("issues") or []
    ]:
        raise ValueError("native8 issue IDs changed after evidence grounding")

    _cost = report["cost"]
    if _cost["calls"]:
        emit(
            "tool", "cost", "· 本次分析成本", "done",
            output_text=f"{_cost['calls']} 次模型调用"
            f"（视频输入 {_cost['calls_by_input'].get('video', 0)} 次 / 图片输入 {_cost['calls_by_input'].get('image', 0)} 次"
            f" / 纯文本 {_cost['calls_by_input'].get('text', 0)} 次）；"
            f"输入 {_cost['prompt_tokens']:,} tokens、输出 {_cost['output_tokens']:,}、"
            f"思考 {_cost['thoughts_tokens']:,}，合计 {_cost['total_tokens']:,} tokens"
            f"；预估成本 ¥{_cost['price']['total_cost']:.4f}",
        )

    # 检查点命中情况进报告：这是判断"重跑到底省下了什么"的唯一依据。命中数为 0 的
    # 重跑说明键没对上（素材或提示词变了、或换了模型），需要能一眼看出来。
    # 产物清单进报告：跨实例续跑时，"这次哪些图是拉回来的、哪些没拉到"是排查
    # 报告缺图的唯一线索。只在启用了对象存储时出现，单机跑法的报告结构不变。
    if store is not None:
        report["artifacts"] = {
            "store": store.kind,
            "restored": _restored,
            "counts": {group: len(items or []) for group, items in _refs.items()},
        }

    _stats = getattr(client, "checkpoint_stats", None)
    if callable(_stats):
        report["checkpoint"] = _stats()
        if report["checkpoint"]["hits"]:
            emit(
                "tool", "checkpoint", "· 复用检查点", "done",
                output_text=f"{report['checkpoint']['hits']} 次模型调用命中检查点"
                f"（存储：{report['checkpoint']['store']}），未重复计费",
            )

    # Every completed run emits a standalone, self-contained HTML report.
    emit("tool", "report", "7 · 生成报告", "running", input_text="汇总画面、要求、判定与调用记录")
    try:
        from aigc_eval.report.html import write_report_html

        html_path, view_url = write_report_html(
            report,
            run_id=session.run_id,
            frame_meta=media_bundle.get("frame_meta", []),
            contact_path=media_bundle.get("contact_path"),
            input_image_path=case_files.input_image_path,
        )
        report["report_html_path"] = str(html_path)
        report["report_view_url"] = view_url
        emit("tool", "report", "7 · 生成报告", "done", output_text="报告已生成，可在结论区打开")
    except Exception as error:  # noqa: BLE001 - report generation is best-effort
        report["report_html_path"] = None
        report["report_view_url"] = None
        report["report_error"] = str(error)
        emit("tool", "report", "7 · 生成报告", "error", output_text=str(error)[:160])

    # Persist the full report so a later request for the same (case, source,
    # engine, mode) reuses it instead of re-running the model.
    try:
        RESULTS_ROOT.mkdir(parents=True, exist_ok=True)
        cache_path.write_text(json.dumps(report, ensure_ascii=False), encoding="utf-8")
    except Exception:  # noqa: BLE001 - caching is best-effort
        pass
    return report


def _stage_offline(
    *,
    emit: Callable[..., None],
    session: TraceSession,
    profile: dict[str, Any],
    metadata: dict[str, Any],
    resolved_mode: str,
    prompt_text: str,
    case_files: CaseFiles,
) -> dict[str, Any]:
    """阶段 3+4 的离线版本：不调模型，用规则与 Mock 占位跑通同一条编排。

    emit 的顺序与 live 路径保持一一对应（compile → observe），前端不必区分两条路径。
    """
    emit("tool", "compile", "3 · 拆解提示词要求（离线）", "running", input_text="按关键词规则拆解")
    prompt_graph = compile_prompt_heuristic(prompt_text, resolved_mode, case_files.reference_assets)
    emit("tool", "compile", "3 · 拆解提示词要求（离线）", "done", output_text=f"拆出 {len(prompt_graph.get('constraints', []))} 条要求")
    emit("tool", "observe", "4 · 看视频、记录画面（离线）", "running", input_text="离线占位")
    observations_bundle = MockVLMObserver().observe(profile, metadata, resolved_mode)
    emit("tool", "observe", "4 · 看视频、记录画面（离线）", "done", output_text=f"{len(observations_bundle.get('observations', []))} 条画面描述")
    verdict_bundle = MockVLMVerifier().verify(profile)
    observations_bundle["native8_scan"] = deepcopy(verdict_bundle)
    session.record_step(
        stage="prompt_compiler",
        title="3 · 拆解提示词要求（离线规则）",
        system_instruction="heuristic_compiler",
        parts=[{"text": prompt_text}],
        raw_output=json.dumps(prompt_graph, ensure_ascii=False, indent=2),
        parsed_output=prompt_graph,
        source="heuristic",
    )
    session.record_step(
        stage="observer",
        title="4 · 看视频、记录画面（离线占位）",
        system_instruction="mock_observer",
        parts=[{"text": json.dumps({"profile": profile, "mode": resolved_mode}, ensure_ascii=False)}],
        raw_output=json.dumps(observations_bundle, ensure_ascii=False, indent=2),
        parsed_output=observations_bundle,
        source="mock",
    )
    return {
        "prompt_graph": prompt_graph,
        "observations_bundle": observations_bundle,
        "verdict_bundle": verdict_bundle,
    }


def _offline_bundles(
    *,
    profile: dict[str, Any],
    metadata: dict[str, Any],
    resolved_mode: str,
    prompt_text: str,
    case_files: CaseFiles,
) -> dict[str, Any]:
    """离线占位：用规则拆提示词 + Mock 观察/裁决，产出与 live 同形状的两个 bundle。

    离线路径存在的意义是让不接模型也能把编排、聚合、报告全跑通（联调与回归都靠它），
    因此这里必须产出与 live 完全同构的 bundle，只是内容是占位。
    """
    prompt_graph = compile_prompt_heuristic(prompt_text, resolved_mode, case_files.reference_assets)
    observations_bundle = MockVLMObserver().observe(profile, metadata, resolved_mode)
    verdict_bundle = MockVLMVerifier().verify(profile)
    observations_bundle["native8_scan"] = deepcopy(verdict_bundle)
    return {
        "prompt_graph": prompt_graph,
        "observations_bundle": observations_bundle,
        "verdict_bundle": verdict_bundle,
    }


def analyze_case(
    case_id: str,
    *,
    use_vlm: bool = True,
    provider: str = DEFAULT_PROVIDER,
    mode: str | None = None,
    allow_fallback: bool = False,
    on_event: Callable[[dict[str, Any]], None] | None = None,
    source: str = "kling",
    refresh: bool = False,
    case_files: CaseFiles | None = None,
    source_label_override: str | None = None,
) -> dict[str, Any]:
    emit = make_emit(on_event)

    provider = normalize_provider(provider)
    if case_files is None:
        # 老流：身份寻址，source 限定 kling/seedance。
        source = "seedance" if source == "seedance" else "kling"
        source_label = "Kling" if source == "kling" else "Seedance"
        case_files = get_case_files(case_id)
        if source == "seedance":
            if not case_files.seedance_path:
                raise FileNotFoundError("该案例无 seedance 视频")
            video_path = case_files.seedance_path
        else:
            video_path = case_files.video_path
    else:
        # 上传流：调用方已经把素材组装好，source 是自由标签。
        source = re.sub(r"[^0-9A-Za-z_.-]", "_", source or "upload") or "upload"
        source_label = source_label_override or "上传视频"
        video_path = case_files.video_path

    # Reuse a prior run for the same (case, source, engine, mode) unless forced.
    cache_path = result_cache_path(
        case_files.slug, source=source, use_vlm=use_vlm, provider=provider, mode=mode
    )
    cached = load_result_cache(cache_path, refresh=refresh, emit=emit)
    if cached is not None:
        return cached

    prompt = load_prompt(case_files)
    prompt_text = prompt["zh"] or prompt["en"]
    resolved_mode = detect_mode(case_files, mode)

    profile = _profile_for(case_files.case_name)

    vlm_error: str | None = None
    engine = "mock"
    client: Any | None = None
    live_cfg: Any = _PROVIDER_CONFIGS.get(provider, load_agentk_config)()

    session = TraceSession(
        case_id=case_id,
        mode=resolved_mode,
        engine=provider if use_vlm else "mock",
        model=live_cfg.model if use_vlm and live_cfg.configured else None,
    )

    # 产物层：拆成多个 activity 后，各阶段可能落在不同实例上，帧图/证据图要经对象
    # 存储传递。没配 blob 时是 None，全流程退化成原来的本地盘用法。
    art_store = shared_artifact_store()
    artifacts: dict[str, Any] = {}

    _prepared = _stage_prepare(
        emit=emit,
        session=session,
        case_files=case_files,
        video_path=video_path,
        source=source,
        source_label=source_label,
        resolved_mode=resolved_mode,
        store=art_store,
    )
    artifacts.update(_prepared["artifacts"])
    metadata = _prepared["metadata"]
    integrity = _prepared["integrity"]
    source_video_path = _prepared["source_video_path"]
    v2v_align = _prepared["v2v_align"]
    media_bundle = _prepared["media_bundle"]
    motion_summary = _prepared["motion_summary"]

    if use_vlm:
        try:
            client = build_live_client(
                provider=provider, session=session, on_event=on_event, refresh=refresh
            )
            engine = provider
        except Exception as error:  # noqa: BLE001 - surface cleanly in UI
            vlm_error = str(error)
            if not allow_fallback:
                raise RuntimeError(f"Live {provider} 不可用（未配置或初始化失败）：{error}") from error
            use_vlm = False
            engine = "mock_fallback"
            session.engine = engine

    _recheck_candidates: list[dict[str, Any]] = []
    if use_vlm and client is not None:
        try:
            _compiled = _stage_compile(
                emit=emit,
                client=client,
                case_files=case_files,
                prompt_text=prompt_text,
                resolved_mode=resolved_mode,
                store=art_store,
                artifacts=artifacts,
            )
            prompt_graph = _compiled["prompt_graph"]
            _observed = _stage_observe(
                emit=emit,
                client=client,
                case_files=case_files,
                video_path=video_path,
                source_video_path=source_video_path,
                prompt_text=prompt_text,
                resolved_mode=resolved_mode,
                prompt_graph=prompt_graph,
                metadata=metadata,
                integrity=integrity,
                media_bundle=media_bundle,
                motion_summary=motion_summary,
                v2v_align=v2v_align,
                has_ref=_compiled["has_ref"],
                store=art_store,
                artifacts=artifacts,
            )
            observations_bundle = _observed["observations_bundle"]
            _native = _observed["native"]
            _focus = _observed["focus"]

            # =============== 4b · 原生八维主判 ===============
            _scanned = _stage_scan(
                emit=emit,
                session=session,
                client=client,
                provider=provider,
                case_files=case_files,
                source=source,
                video_path=video_path,
                source_video_path=source_video_path,
                prompt=prompt,
                prompt_text=prompt_text,
                prompt_graph=prompt_graph,
                resolved_mode=resolved_mode,
                metadata=metadata,
                media_bundle=media_bundle,
                v2v_align=v2v_align,
                observations_bundle=observations_bundle,
                focus=_focus,
                native=_native,
                store=art_store,
                artifacts=artifacts,
            )
            _uni = _scanned["uni"]
            artifacts.update(_scanned["artifacts"])
            observations_bundle["native8_scan"] = _uni
            observations_bundle["_zoom_frames"] = _uni.get("_frames") or []

            _recheck_candidates = _stage_recheck(
                emit=emit,
                client=client,
                video_path=video_path,
                prompt_text=prompt_text,
                prompt_graph=prompt_graph,
                metadata=metadata,
                observations_bundle=observations_bundle,
                v2v_align=v2v_align,
                uni=_uni,
                focus=_focus,
                store=art_store,
                artifacts=artifacts,
            )
            # 4b 是唯一事实底稿；4c 只能在后续按候选 issue 增量提交证据。
            verdict_bundle = deepcopy(_uni)
        except Exception as error:  # noqa: BLE001
            vlm_error = str(error)
            emit("model", "native8", "原生八维分析失败", "error", output_text=str(error)[:180])
            if not allow_fallback:
                # VLM-first: never fabricate mock verdicts; surface the failure
                # (retries/backoff already exhausted inside the client).
                session.record_step(
                    stage="vlm_error",
                    title="Live VLM 失败",
                    system_instruction=None,
                    parts=[{"text": f"vlm_error={vlm_error}"}],
                    raw_output=vlm_error,
                    parsed_output=None,
                    error=vlm_error,
                    source="gemini",
                )
                session.save(extra={"analysis_status": "failed", "vlm_error": vlm_error, "engine": "error"})
                raise RuntimeError(f"Live VLM 分析失败（已重试）：{vlm_error}") from error
            engine = "mock_fallback"
            session.engine = engine
            use_vlm = False
            _recheck_candidates = []
            _offline = _offline_bundles(
                profile=profile,
                metadata=metadata,
                resolved_mode=resolved_mode,
                prompt_text=prompt_text,
                case_files=case_files,
            )
            prompt_graph = _offline["prompt_graph"]
            observations_bundle = _offline["observations_bundle"]
            verdict_bundle = _offline["verdict_bundle"]
            session.record_step(
                stage="mock_fallback",
                title="Live 失败后的 Mock 回退",
                system_instruction=None,
                parts=[{"text": f"vlm_error={vlm_error}"}],
                raw_output=json.dumps(
                    {"observations": observations_bundle, "verdict": verdict_bundle},
                    ensure_ascii=False,
                    indent=2,
                ),
                parsed_output={"observations": observations_bundle, "verdict": verdict_bundle},
                source="mock",
            )
    else:
        _offline = _stage_offline(
            emit=emit,
            session=session,
            profile=profile,
            metadata=metadata,
            resolved_mode=resolved_mode,
            prompt_text=prompt_text,
            case_files=case_files,
        )
        prompt_graph = _offline["prompt_graph"]
        observations_bundle = _offline["observations_bundle"]
        verdict_bundle = _offline["verdict_bundle"]

    _aggregated = _stage_aggregate(
        case_files=case_files,
        resolved_mode=resolved_mode,
        metadata=metadata,
        prompt_graph=prompt_graph,
        observations_bundle=observations_bundle,
        verdict_bundle=verdict_bundle,
        recheck_candidates=_recheck_candidates,
        v2v_align=v2v_align,
        use_vlm=use_vlm,
    )
    observations = _aggregated["observations"]
    native_draft = _aggregated["native_draft"]
    _review_funnel = _aggregated["review_funnel"]
    rubric_violations = _aggregated["rubric_violations"]

    _evidence = _stage_evidence(
        emit=emit,
        session=session,
        client=client,
        case_files=case_files,
        source=source,
        video_path=video_path,
        prompt=prompt,
        native_draft=native_draft,
        observations_bundle=observations_bundle,
        use_vlm=use_vlm,
        store=art_store,
        artifacts=artifacts,
    )
    grounding_bundle = _evidence["grounding_bundle"]
    evidence_cards = _evidence["evidence_cards"]
    artifacts.update(_evidence["artifacts"])

    return _stage_report(
        emit=emit,
        session=session,
        client=client,
        case_files=case_files,
        source=source,
        source_label=source_label,
        resolved_mode=resolved_mode,
        prompt=prompt,
        prompt_graph=prompt_graph,
        metadata=metadata,
        integrity=integrity,
        media_bundle=media_bundle,
        motion_summary=motion_summary,
        source_video_path=source_video_path,
        v2v_align=v2v_align,
        observations=observations,
        observations_bundle=observations_bundle,
        native_draft=native_draft,
        review_funnel=_review_funnel,
        rubric_violations=rubric_violations,
        evidence_cards=evidence_cards,
        grounding_bundle=grounding_bundle,
        engine=engine,
        vlm_error=vlm_error,
        cache_path=cache_path,
        store=art_store,
        artifacts=artifacts,
    )
