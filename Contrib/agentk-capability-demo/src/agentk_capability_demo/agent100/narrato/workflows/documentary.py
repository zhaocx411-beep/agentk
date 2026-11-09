from __future__ import annotations

import asyncio
import json
import math
from datetime import timedelta
from typing import Any
from uuid import uuid4 as local_uuid4

from agentk.worker import report_progress
from temporalio import workflow
from temporalio.common import RetryPolicy
from temporalio.exceptions import ActivityError, ApplicationError

from agentk_capability_demo.agent100.narrato import workflow_config
from agentk_capability_demo.agent100.narrato.agentk_core import (
    CoreTemporalTypes,
    first_artifact_uri,
    text_result,
)
from agentk_capability_demo.agent100.narrato.contracts import (
    ASPECT_DIMENSIONS,
    MAX_VIDEO_BYTES,
    MAX_VIDEO_DURATION_SECONDS,
    NARRATIVE_PLAN_SCHEMA,
    PACING_LEGACY_RETRY_THRESHOLD_SECONDS,
    PACING_RETRY_THRESHOLD_SECONDS,
    SCRIPT_GENERATION_SCHEMA,
    ValidationError,
    build_analysis_windows,
    build_compose_batches,
    build_process_operations,
    excessive_narration_gaps,
    narrative_profile_from_plan,
    normalize_asr_output,
    normalize_create_input,
    normalize_render_input,
    opening_hook_gap,
    pacing_warning,
    post_event_timing_issues,
    require_http_uri,
    resolve_render_timeline,
    validate_documentary_script,
    validate_narrative_plan,
)
from agentk_capability_demo.agent100.narrato.contracts.models import normalize_generate_input
from agentk_capability_demo.agent100.narrato.contracts.styles import (
    DOCUMENTARY_STYLE,
    NarrationStyle,
    resolve_style,
)
from agentk_capability_demo.agent100.narrato.prompts import (
    analysis_prompt,
    documentary_script_prompt,
    narrative_plan_prompt,
    pacing_revision_prompt,
)
from agentk_capability_demo.agent100.narrato.settings import QWEN_TTS_INSTRUCT_MODEL, Settings
from agentk_capability_demo.agent100.narrato.workflows.asr_rate_limiter import (
    ASR_PERMIT_GRANTED_SIGNAL,
    ASR_RATE_LIMIT_RELEASE_SIGNAL,
    ASR_RATE_LIMIT_REQUEST_SIGNAL,
)

GENERATE_CAPABILITY_ID = "contrib.narrato.generate_documentary_script"
RENDER_CAPABILITY_ID = "contrib.narrato.render_documentary_video"
CREATE_CAPABILITY_ID = "contrib.narrato.create_documentary_video"

GENERATE_WORKFLOW_TYPE = "narrato.documentary.generate_script.v2"
RENDER_WORKFLOW_TYPE = "narrato.documentary.render_video.v2"
CREATE_WORKFLOW_TYPE = "narrato.documentary.create_video.v2"
OPENING_CADENCE_PATCH_ID = "narrato-opening-cadence-2026-08-27"
POSITIONING_HOOK_PATCH_ID = "narrato-positioning-hook-2026-08-27"
VIDEO_PRECLIP_PATCH_ID = "narrato-video-preclip-2026-08-29"
POST_EVENT_NARRATION_PATCH_ID = "narrato-post-event-narration-2026-08-29"
MODEL_OUTPUT_ATTEMPTS = 3
_MAX_NARRATION_CUES = 500


def _opening_cadence_enabled() -> bool:
    """Return the replay-safe cadence version selected by Temporal history."""

    return workflow.patched(OPENING_CADENCE_PATCH_ID)


def _positioning_hook_enabled() -> bool:
    """Return the replay-safe opening-hook copy version selected by history."""

    return workflow.patched(POSITIONING_HOOK_PATCH_ID)


def _video_preclip_enabled() -> bool:
    """Keep existing histories on their original activity sequence."""

    return workflow.patched(VIDEO_PRECLIP_PATCH_ID)


def _post_event_narration_enabled() -> bool:
    """Keep generated and rendered narration behind its source evidence."""

    return workflow.patched(POST_EVENT_NARRATION_PATCH_ID)


def _cadence_gaps(
    script: dict[str, Any],
    transcript_segments: list[dict[str, Any]],
    *,
    opening_cadence: bool,
    style: NarrationStyle = DOCUMENTARY_STYLE,
) -> list[dict[str, float]]:
    if style.retell_plot:
        threshold = style.max_gap_seconds
    elif opening_cadence:
        threshold = PACING_RETRY_THRESHOLD_SECONDS
    else:
        threshold = PACING_LEGACY_RETRY_THRESHOLD_SECONDS
    return excessive_narration_gaps(
        script,
        transcript_segments,
        threshold_seconds=threshold,
        dialogue_counts_as_coverage=style.dialogue_counts_as_coverage,
    )


def _cadence_warning(
    gaps: list[dict[str, float]],
    *,
    opening_cadence: bool,
    post_event_narration: bool = False,
    style: NarrationStyle = DOCUMENTARY_STYLE,
) -> str:
    if style.retell_plot and gaps:
        ranges = "、".join(
            f"{item['startSeconds']:.3f}-{item['endSeconds']:.3f}秒" for item in gaps
        )
        return f"解说密度未达到空档不超过{style.max_gap_seconds:g}秒的建议：{ranges}"
    if post_event_narration and gaps:
        ranges = "、".join(
            f"{item['startSeconds']:.3f}-{item['endSeconds']:.3f}秒" for item in gaps
        )
        return f"解说节奏未达到非对白空档通常不超过5秒的建议：{ranges}"
    return pacing_warning(
        gaps, opening_cadence=opening_cadence, max_gap_seconds=style.max_gap_seconds
    )


def _soft_timing_warning(issues: list[dict[str, Any]]) -> str:
    if not issues:
        return ""
    cue_ids = list(dict.fromkeys(str(item.get("cueId") or "") for item in issues))
    cue_ids = [item for item in cue_ids if item]
    suffix = f"（{', '.join(cue_ids[:8])}）" if cue_ids else ""
    return f"时间轴软检查仍有{len(issues)}项建议{suffix}，已采用最后一版脚本继续"


_CORE_RETRY = RetryPolicy(
    initial_interval=timedelta(seconds=1),
    backoff_coefficient=2,
    maximum_interval=timedelta(seconds=30),
    maximum_attempts=2,
    non_retryable_error_types=["InvalidArgument", "Configuration"],
)

_ASR_RETRY = RetryPolicy(
    initial_interval=timedelta(seconds=15),
    backoff_coefficient=2,
    maximum_interval=timedelta(seconds=60),
    maximum_attempts=4,
    non_retryable_error_types=["InvalidArgument", "Configuration"],
)
_ASR_ATTEMPT_TIMEOUT = timedelta(seconds=45)
_ASR_SCHEDULE_TO_CLOSE = timedelta(minutes=5)

_RENDER_RETRY = RetryPolicy(
    initial_interval=timedelta(seconds=5),
    backoff_coefficient=2,
    maximum_interval=timedelta(seconds=30),
    maximum_attempts=4,
    non_retryable_error_types=[
        "InvalidArgument",
        "Configuration",
        "InvalidCoreOutput",
    ],
)

_NON_DEGRADABLE_ACTIVITY_ERROR_TYPES = frozenset(
    {"InvalidArgument", "Configuration", "InvalidCoreOutput"}
)
_GEMINI_FILE_FETCH_LIMIT_BYTES = 15 << 20


def _is_degradable_activity_error(error: ActivityError) -> bool:
    """Return whether an exhausted Activity failure may use a workflow fallback."""

    current: BaseException | None = error
    seen: set[int] = set()
    while current is not None and id(current) not in seen:
        seen.add(id(current))
        if isinstance(current, ApplicationError):
            if current.type in _NON_DEGRADABLE_ACTIVITY_ERROR_TYPES:
                return False
            if current.non_retryable:
                return False
        current = current.__cause__ or current.__context__
    return True


def _upstream_rejection_reason(error: ActivityError) -> str:
    """上游供应商拒绝了这一次请求时返回其说明，否则返回空串。

    这类错误被标成不可重试，但它表达的是"这次调用没成功"，不是"入参或配置有问题"。
    对逐窗口的视频理解来说，一个窗口被拒不该让整条流水线失败——调用方本来就有
    退回 ASR 证据的降级路径。真正的入参与配置错误仍由 _is_degradable_activity_error 拦下。
    """

    current: BaseException | None = error
    seen: set[int] = set()
    while current is not None and id(current) not in seen:
        seen.add(id(current))
        if isinstance(current, ApplicationError) and current.type == "UpstreamRequestRejected":
            return str(current) or "上游拒绝了本次请求"
        current = current.__cause__ or current.__context__
    return ""


def _activity_error_contains(error: ActivityError, *needles: str) -> bool:
    current: BaseException | None = error
    seen: set[int] = set()
    lowered = tuple(item.lower() for item in needles)
    while current is not None and id(current) not in seen:
        seen.add(id(current))
        message = str(current).lower()
        if any(item in message for item in lowered):
            return True
        current = current.__cause__ or current.__context__
    return False


async def _understand_video_with_clip_fallback(
    payload: dict[str, Any], *, clip_first: bool = False
) -> Any | None:
    """Understand a segment, physically trimming when the provider rejects the source size."""

    if not clip_first:
        try:
            return await _activity(
                CoreTemporalTypes.UNDERSTAND_VIDEO_SEGMENT,
                payload,
                timeout=timedelta(minutes=8),
            )
        except ActivityError as error:
            if not _is_degradable_activity_error(error) and not _upstream_rejection_reason(error):
                raise
            if not _activity_error_contains(error, "http 413", "payload too large"):
                return None

    duration = round(float(payload["endSeconds"]) - float(payload["startSeconds"]), 3)
    try:
        clipped = await _activity(
            CoreTemporalTypes.PROCESS_VIDEO,
            {
                "videoUrl": payload["videoUrl"],
                "operations": [
                    {
                        "type": "trim",
                        "startSeconds": payload["startSeconds"],
                        "durationSeconds": duration,
                    }
                ],
                "quality": "small",
            },
            timeout=timedelta(minutes=13),
        )
        clip_uri = first_artifact_uri(clipped, "process_video", expected_type="video")
        return await _activity(
            CoreTemporalTypes.UNDERSTAND_VIDEO_SEGMENT,
            {
                **payload,
                "videoUrl": clip_uri,
                "startSeconds": 0,
                "endSeconds": duration,
            },
            timeout=timedelta(minutes=8),
        )
    except ActivityError as error:
        if not _is_degradable_activity_error(error) and not _upstream_rejection_reason(error):
            raise
        return None
    except (TypeError, ValueError):
        return None


def configure(settings: Settings) -> None:
    workflow_config.configure(settings)


def _next_call_id(name: str) -> str:
    try:
        workflow_id = workflow.info().workflow_id
        call_uuid = workflow.uuid4()
    except Exception:  # Unit-level workflow calls have no Temporal runtime.
        workflow_id = "workflow-test"
        call_uuid = local_uuid4()
    return f"{workflow_id}:{call_uuid}:{name}"


async def _activity(
    name: str,
    payload: dict[str, Any],
    *,
    timeout: timedelta,
    retry_policy: RetryPolicy = _CORE_RETRY,
    schedule_to_close_timeout: timedelta | None = None,
) -> Any:
    call_id = _next_call_id(name)
    await _report_request("activity", name, payload, call_id=call_id)
    output = await workflow.execute_activity(
        name,
        payload,
        task_queue=workflow_config.get_settings().core_task_queue,
        start_to_close_timeout=timeout,
        schedule_to_close_timeout=schedule_to_close_timeout or timeout + timedelta(minutes=2),
        retry_policy=retry_policy,
    )
    await _report_response("activity", name, output, call_id=call_id)
    return output


async def _report_request(
    kind: str,
    name: str,
    request: dict[str, Any],
    *,
    call_id: str | None = None,
) -> None:
    await report_progress(
        f"request:{call_id or name}",
        name,
        {
            "kind": "request",
            "requestKind": kind,
            "name": name,
            "request": request,
            **({"callId": call_id} if call_id else {}),
        },
    )


async def _report_response(kind: str, name: str, response: Any, *, call_id: str) -> None:
    await report_progress(
        f"response:{call_id}",
        name,
        {
            "kind": "response",
            "responseKind": kind,
            "name": name,
            "callId": call_id,
            "response": response,
        },
    )


def _invalid(error: Exception) -> ApplicationError:
    return ApplicationError(str(error), type="InvalidArgument", non_retryable=True)


def _validate_probe(output: Any) -> tuple[float, int, bool, int, int]:
    if not isinstance(output, dict):
        raise ValidationError("probe_video returned a non-object result")
    raw_size = output.get("fileSizeBytes")
    raw_duration = output.get("durationSeconds")
    if isinstance(raw_size, bool) or isinstance(raw_duration, bool):
        raise ValidationError("probe_video returned invalid numeric metadata")
    size = int(raw_size or 0)
    duration = float(raw_duration or 0)
    if not math.isfinite(duration):
        raise ValidationError("probe_video returned invalid durationSeconds")
    if size <= 0 or size > MAX_VIDEO_BYTES:
        raise ValidationError(f"video size must be between 1 and {MAX_VIDEO_BYTES} bytes")
    if duration <= 0:
        raise ValidationError("video duration must be greater than 0")
    if duration > MAX_VIDEO_DURATION_SECONDS:
        raise ValidationError(
            f"video duration must not exceed {MAX_VIDEO_DURATION_SECONDS} seconds (1 hour)"
        )
    video_streams = output.get("videoStreams")
    if not isinstance(video_streams, list) or not video_streams:
        raise ValidationError("input contains no video stream")
    video_stream = video_streams[0]
    if not isinstance(video_stream, dict):
        raise ValidationError("probe_video returned invalid video stream metadata")
    raw_width = video_stream.get("width")
    raw_height = video_stream.get("height")
    if isinstance(raw_width, bool) or isinstance(raw_height, bool):
        raise ValidationError("probe_video returned invalid video dimensions")
    width = int(raw_width or 0)
    height = int(raw_height or 0)
    if width <= 0 or height <= 0:
        raise ValidationError("probe_video returned invalid video dimensions")
    audio_streams = output.get("audioStreams") or []
    if not isinstance(audio_streams, list):
        raise ValidationError("probe_video returned invalid audioStreams")
    return round(duration, 3), size, bool(audio_streams), width, height


def _require_mapping(value: Any, operation: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValidationError(f"{operation} returned a non-object result")
    return value


def _normalize_search_output(value: Any, expected_query: str) -> dict[str, Any]:
    payload = _require_mapping(value, "web_search")
    raw_results = payload.get("results")
    if not isinstance(raw_results, list):
        raise ValidationError("web_search results must be an array")
    results: list[dict[str, str]] = []
    for raw in raw_results[:10]:
        if not isinstance(raw, dict):
            continue
        try:
            url = require_http_uri(raw.get("url"), "web_search result URL")
        except ValidationError:
            continue
        title = str(raw.get("title") or url).strip()[:300]
        summary = str(raw.get("summary") or "").strip()[:500]
        results.append({"title": title, "url": url, "summary": summary})
    return {"query": expected_query, "results": results}


def _repair_prompt(original_prompt: str, output: Any, error: Exception) -> str:
    serialized = json.dumps(output, ensure_ascii=False, separators=(",", ":"), default=str)
    return (
        original_prompt
        + "\n上一次输出未通过本地校验。请根据错误修正，不要重复错误。"
        + "\n校验错误："
        + str(error)
        + "\n上一次输出："
        + serialized[:12000]
        + "\n请只返回修正后的完整 JSON。"
    )


def _model_result(output: Any) -> Any:
    try:
        return text_result(output)
    except (TypeError, ValueError):
        return output


def _evidence_summary(window: dict[str, Any]) -> str:
    transcript = "".join(
        str(item.get("text") or "").strip()
        for item in window.get("transcriptSegments") or []
        if isinstance(item, dict)
    ).strip()
    observation = str(window.get("segmentObservation") or "").strip()
    text = transcript or observation or "画面在这一时段继续展开。"
    return text[:160]


def _fallback_narrative_plan(
    evidence_windows: list[dict[str, Any]],
    *,
    duration_seconds: float,
) -> dict[str, Any]:
    beats: list[dict[str, Any]] = []
    # Group neighboring evidence windows so even the deterministic fallback
    # represents an event arc rather than a shot-by-shot/ASR-by-ASR transcript.
    for offset in range(0, len(evidence_windows), 2):
        grouped = evidence_windows[offset : offset + 2]
        summaries = [_evidence_summary(window) for window in grouped]
        beats.append(
            {
                "id": f"beat-{len(beats) + 1:03d}",
                "startSeconds": grouped[0]["startSeconds"],
                "endSeconds": grouped[-1]["endSeconds"],
                "summary": "；".join(dict.fromkeys(summaries))[:300],
                "narrationPurpose": "概括这一阶段的新信息并推进整体发展",
                "importance": 5 if not beats else 3,
                "evidenceWindowIndexes": [int(window["index"]) for window in grouped],
            }
        )
    first_summary = beats[0]["summary"] if beats else "这段画面究竟发生了什么？"
    return validate_narrative_plan(
        {
            "primaryGenre": "通用剧情",
            "secondaryGenres": [],
            "thesis": "围绕原片中可确认的事件，提炼人物行动、变化与结果。",
            "hook": first_summary,
            "style": "简洁、有信息增量、尊重原片证据",
            "pacing": "按情节段落组织，重要对白处留白",
            "globalTtsInstruction": "自然、清晰、有叙事感，避免过度夸张。",
            "researchNeeded": False,
            "researchReason": "保底策划仅使用当前视频证据。",
            "researchQueries": [],
            "storyBeats": beats,
        },
        duration_seconds=duration_seconds,
        window_count=len(evidence_windows),
    )


def _fallback_script(
    plan: dict[str, Any],
    *,
    video_uri: str,
    duration_seconds: float,
    window_count: int,
    opening_cadence: bool = True,
    post_event_narration: bool = False,
) -> dict[str, Any]:
    cues: list[dict[str, Any]] = []
    cursor = 0.0
    for beat in plan["storyBeats"]:
        is_opening = opening_cadence and not cues
        evidence_end: float | None = None
        if post_event_narration:
            evidence_end = min(
                float(beat["endSeconds"]),
                max(0.0, duration_seconds - 0.5),
            )
            start = max(cursor, evidence_end)
        else:
            start = 0.0 if is_opening else max(cursor, float(beat["startSeconds"]))
        if start >= duration_seconds:
            break
        available = duration_seconds - start
        planned_duration = min(6.0, available)
        if not post_event_narration:
            planned_duration = min(planned_duration, float(beat["endSeconds"]) - start)
        end = min(duration_seconds, start + max(0.5, planned_duration))
        if end <= start:
            continue
        cues.append(
            {
                "id": f"cue-{len(cues) + 1:03d}",
                "startSeconds": round(start, 3),
                "endSeconds": round(end, 3),
                **(
                    {"evidenceEndSeconds": round(evidence_end, 3)}
                    if evidence_end is not None
                    else {}
                ),
                "narration": str(plan["hook"] if is_opening else beat["summary"])[:300],
                "evidenceWindowIndexes": list(beat["evidenceWindowIndexes"]),
                "ttsInstruction": (
                    "开场直接建立悬念，清晰、有抓力。"
                    if is_opening
                    else "自然清晰，按情节重点控制停连和力度。"
                ),
                "importance": 5 if is_opening else int(beat["importance"]),
                "emotion": "calm",
            }
        )
        cursor = end
    if not cues:
        start = max(0.0, duration_seconds - 1.0) if post_event_narration else 0.0
        cues.append(
            {
                "id": "cue-001",
                "startSeconds": start,
                "endSeconds": duration_seconds,
                **({"evidenceEndSeconds": start} if post_event_narration else {}),
                "narration": str(plan["hook"]),
                "evidenceWindowIndexes": [0],
                "ttsInstruction": "自然清晰地建立开场悬念。",
                "importance": 5,
                "emotion": "surprised",
            }
        )
    return validate_documentary_script(
        {
            "version": "2",
            "sourceVideoUri": video_uri,
            "sourceDurationSeconds": duration_seconds,
            "narrativeProfile": narrative_profile_from_plan(plan),
            "narrationCues": cues,
        },
        automatic=True,
        source_uri=video_uri,
        source_duration=duration_seconds,
        narrative_profile=narrative_profile_from_plan(plan),
        window_count=window_count,
    )


def _ensure_opening_hook(
    script: dict[str, Any],
    plan: dict[str, Any],
    *,
    window_count: int,
) -> dict[str, Any]:
    if opening_hook_gap(script) is None:
        return script
    first_beat = plan["storyBeats"][0]
    hook = {
        "id": "cue-001",
        "startSeconds": 0.0,
        "endSeconds": min(3.0, float(script["narrationCues"][0]["startSeconds"])),
        "narration": str(plan["hook"]),
        "evidenceWindowIndexes": list(first_beat["evidenceWindowIndexes"]),
        "ttsInstruction": "开场直接建立悬念，清晰、有抓力。",
        "importance": 5,
        "emotion": "surprised",
    }
    cues = [hook, *script["narrationCues"]][:_MAX_NARRATION_CUES]
    return validate_documentary_script(
        {**script, "narrationCues": cues},
        automatic=True,
        source_uri=script["sourceVideoUri"],
        source_duration=float(script["sourceDurationSeconds"]),
        narrative_profile=narrative_profile_from_plan(plan),
        window_count=window_count,
    )


@workflow.defn(name=GENERATE_WORKFLOW_TYPE)
class GenerateDocumentaryScriptWorkflow:
    def __init__(self) -> None:
        self._asr_permits: set[str] = set()

    @workflow.signal(name=ASR_PERMIT_GRANTED_SIGNAL)
    def grant_asr_permit(self, request_id: str) -> None:
        if request_id:
            self._asr_permits.add(str(request_id))

    async def _acquire_asr_permit(self, request_id: str, settings: Settings) -> bool:
        try:
            info = workflow.info()
        except Exception:
            # Unit-level workflow calls have no Temporal runtime.
            return False
        handle = workflow.get_external_workflow_handle(settings.asr_rate_limiter_workflow_id)
        await handle.signal(
            ASR_RATE_LIMIT_REQUEST_SIGNAL,
            {
                "requestId": request_id,
                "workflowId": info.workflow_id,
                "runId": info.run_id,
            },
        )
        await workflow.wait_condition(lambda: request_id in self._asr_permits)
        self._asr_permits.discard(request_id)
        return True

    async def _release_asr_permit(self, request_id: str, settings: Settings) -> None:
        handle = workflow.get_external_workflow_handle(settings.asr_rate_limiter_workflow_id)
        await handle.signal(ASR_RATE_LIMIT_RELEASE_SIGNAL, {"requestId": request_id})

    @workflow.run
    async def run(self, raw_input: dict[str, Any]) -> dict[str, Any]:
        # patched() 必须无条件调用，否则重放时的分支判定会和历史不一致。
        opening_cadence = _opening_cadence_enabled()
        positioning_hook = _positioning_hook_enabled()
        video_preclip = _video_preclip_enabled()
        post_event_patch = _post_event_narration_enabled()
        settings = workflow_config.get_settings()
        try:
            args = normalize_generate_input(raw_input)
            models = settings.resolve_models(args["models"])
        except (ValueError, ValidationError) as error:
            raise _invalid(error) from error
        style = resolve_style(args["style"])
        # 复述剧情的风格本就要提前交代情节，事件后置约束在这里必须让位于风格。
        post_event_narration = post_event_patch and not style.retell_plot

        await report_progress("validate_input", "正在验证输入视频", {"percent": 5})
        probe = await _activity(
            CoreTemporalTypes.PROBE_VIDEO,
            {"videoUrl": args["videoUri"]},
            timeout=timedelta(minutes=8),
        )
        try:
            duration, file_size, has_audio, _, _ = _validate_probe(probe)
        except (TypeError, ValueError, ValidationError) as error:
            raise _invalid(error) from error

        options = args["analysisOptions"]
        windows = build_analysis_windows(duration, settings.analysis_window_seconds)

        transcripts: dict[int, dict[str, Any]] = {
            int(window["index"]): {
                "windowIndex": int(window["index"]),
                "startSeconds": float(window["startSeconds"]),
                "endSeconds": float(window["endSeconds"]),
                "language": "",
                "text": "",
                "segments": [],
            }
            for window in windows
        }
        asr_failures: dict[int, str] = {}

        def degraded_transcript(window: dict[str, Any], error: Exception) -> tuple[int, dict[str, Any]]:
            index = int(window["index"])
            asr_failures[index] = str(error)
            return index, transcripts[index]

        async def transcribe(window: dict[str, Any]) -> tuple[int, dict[str, Any]]:
            start = float(window["startSeconds"])
            end = float(window["endSeconds"])
            try:
                extracted = await _activity(
                    CoreTemporalTypes.EXTRACT_AUDIO,
                    {
                        "videoUrl": args["videoUri"],
                        "startSeconds": start,
                        "durationSeconds": round(end - start, 3),
                        "format": "mp3",
                        "bitRate": "96k",
                        "sampleRate": 16000,
                        "channels": 1,
                        "normalizeLoudness": True,
                    },
                    timeout=timedelta(minutes=13),
                )
                extracted_payload = _require_mapping(extracted, "extract_audio")
                audio_uri = first_artifact_uri(extracted, "extract_audio", expected_type="audio")
                audio_duration = extracted_payload.get("durationSeconds", round(end - start, 3))
                if (
                    isinstance(audio_duration, bool)
                    or not isinstance(audio_duration, (int, float))
                    or audio_duration <= 0
                ):
                    raise ValidationError("extract_audio returned invalid durationSeconds")
            except ActivityError as error:
                if not _is_degradable_activity_error(error):
                    raise
                return degraded_transcript(window, error)
            except (TypeError, ValueError, ValidationError) as error:
                return degraded_transcript(window, error)
            permit_id = _next_call_id("asr-permit")
            permit_acquired = await self._acquire_asr_permit(permit_id, settings)
            try:
                try:
                    asr_request = {
                        "model": models["asr"],
                        "audioUrl": audio_uri,
                        "task": "transcribe",
                    }
                    if options["asrLanguage"]:
                        asr_request["language"] = options["asrLanguage"]
                    result = await _activity(
                        CoreTemporalTypes.TRANSCRIBE_AUDIO,
                        asr_request,
                        timeout=_ASR_ATTEMPT_TIMEOUT,
                        retry_policy=_ASR_RETRY,
                        schedule_to_close_timeout=_ASR_SCHEDULE_TO_CLOSE,
                    )
                except ActivityError as error:
                    if not _is_degradable_activity_error(error):
                        raise
                    return degraded_transcript(window, error)
            finally:
                if permit_acquired:
                    await self._release_asr_permit(permit_id, settings)
            try:
                normalized = normalize_asr_output(
                    result,
                    window_index=int(window["index"]),
                    start_seconds=start,
                    end_seconds=end,
                    audio_duration_seconds=float(audio_duration),
                )
            except ValidationError as error:
                # A malformed provider response is local to this analysis
                # window. Preserve the rest of the video's evidence instead of
                # turning an optional ASR enhancement into a global failure.
                return degraded_transcript(window, error)
            return int(window["index"]), normalized

        if options["asrEnabled"] and has_audio:
            await report_progress(
                "transcribe_audio",
                "正在提取音轨并识别对白",
                {"percent": 10, "completed": 0, "total": len(windows)},
            )
            completed = 0
            for start in range(0, len(windows), settings.asr_concurrency):
                chunk = await asyncio.gather(
                    *(
                        transcribe(item)
                        for item in windows[start : start + settings.asr_concurrency]
                    )
                )
                transcripts.update(chunk)
                completed += len(chunk)
                failed_windows = sorted(
                    int(item["index"])
                    for item in windows[start : start + settings.asr_concurrency]
                    if int(item["index"]) in asr_failures
                )
                await report_progress(
                    "transcribe_audio",
                    "部分音频窗口识别失败，已按无对白继续"
                    if failed_windows
                    else "正在提取音轨并识别对白",
                    {
                        "percent": 10 + round(12 * completed / len(windows)),
                        "completed": completed,
                        "total": len(windows),
                        **(
                            {"degraded": True, "failedWindows": failed_windows}
                            if failed_windows
                            else {}
                        ),
                    },
                )
        else:
            reason = "源视频没有音轨" if options["asrEnabled"] else "调用参数已关闭 ASR"
            await report_progress(
                "transcribe_audio",
                f"跳过语音识别：{reason}",
                {"percent": 22, "completed": 0, "total": len(windows), "skipped": True},
            )

        raw_transcript_segments = [
            segment for index in sorted(transcripts) for segment in transcripts[index]["segments"]
        ]
        corrected_by_window: dict[int, list[dict[str, Any]]] = {
            int(window["index"]): [] for window in windows
        }
        for segment in raw_transcript_segments:
            for window in windows:
                if (
                    float(window["startSeconds"])
                    <= segment["startSeconds"]
                    < float(window["endSeconds"])
                ):
                    corrected_by_window[int(window["index"])].append(segment)
                    break

        async def analyze(window: dict[str, Any]) -> dict[str, Any]:
            index = int(window["index"])
            transcript = corrected_by_window[index]
            segment_result = await _understand_video_with_clip_fallback(
                {
                    "model": models["vision"],
                    "videoUrl": args["videoUri"],
                    "startSeconds": window["startSeconds"],
                    "endSeconds": window["endSeconds"],
                    "fps": 1,
                    "prompt": analysis_prompt(
                        start_seconds=float(window["startSeconds"]),
                        end_seconds=float(window["endSeconds"]),
                        instructions=args["instructions"],
                        post_event_narration=post_event_narration,
                        style=style,
                    ),
                },
                clip_first=(
                    video_preclip and file_size > _GEMINI_FILE_FETCH_LIMIT_BYTES
                ),
            )
            if segment_result is None:
                return {
                    **window,
                    "transcriptSegments": transcript,
                    "segmentObservation": (
                        "当前环境无法使用视觉模型；本窗口仅允许使用 transcriptSegments "
                        "中的 ASR 对白作为证据，不得推断画面内容。"
                    ),
                    "analysisMode": "transcript_only",
                }
            try:
                segment_text = str(
                    _require_mapping(segment_result, "understand_video_segment").get("text") or ""
                ).strip()
            except (TypeError, ValueError, ValidationError):
                segment_text = ""
            if not segment_text:
                return {
                    **window,
                    "transcriptSegments": transcript,
                    "segmentObservation": "视觉模型未返回文本，已使用当前窗口 ASR 证据。",
                    "analysisMode": "transcript_only",
                }
            return {
                **window,
                "transcriptSegments": transcript,
                "segmentObservation": segment_text,
                "analysisMode": "multimodal",
            }

        await report_progress(
            "analyze_video",
            "正在融合连续视频与 ASR",
            {"percent": 26, "completed": 0, "total": len(windows)},
        )
        evidence_windows: list[dict[str, Any]] = []
        for start in range(0, len(windows), settings.analysis_concurrency):
            chunk = await asyncio.gather(
                *(analyze(item) for item in windows[start : start + settings.analysis_concurrency])
            )
            evidence_windows.extend(chunk)
            completed = len(evidence_windows)
            percent = 26 + round(24 * completed / len(windows))
            degraded = any(item["analysisMode"] == "transcript_only" for item in chunk)
            # 报出累计丢了几个窗口：单个窗口偶发被上游拒绝可以接受，但整片都降级
            # 意味着视觉模型根本没生效，脚本会变成纯 ASR 复述，必须让人看见。
            lost = sum(1 for item in evidence_windows if item["analysisMode"] == "transcript_only")
            await report_progress(
                "analyze_video",
                f"{lost}/{completed} 个窗口的视频理解不可用，正使用 ASR 证据继续"
                if degraded
                else "正在融合连续视频与 ASR",
                {
                    "percent": percent,
                    "completed": completed,
                    "total": len(windows),
                    **({"degraded": True, "visionUnavailableWindows": lost} if degraded else {}),
                },
            )

        if all(item["analysisMode"] == "transcript_only" for item in evidence_windows) and not any(
            item["transcriptSegments"] for item in evidence_windows
        ):
            await report_progress(
                "analyze_video",
                "视频理解与 ASR 均无有效文本，已使用保底描述继续",
                {"percent": 50, "degraded": True},
            )

        await report_progress("plan_narrative", "正在生成整片叙事策划", {"percent": 52})
        plan_payload = {
            "model": models["story"],
            "prompt": narrative_plan_prompt(
                evidence_windows=evidence_windows,
                duration_seconds=duration,
                instructions=args["instructions"],
                opening_cadence=opening_cadence,
                positioning_hook=positioning_hook,
                post_event_narration=post_event_narration,
                style=style,
            ),
            "outputSchema": NARRATIVE_PLAN_SCHEMA,
        }
        narrative_plan: dict[str, Any] | None = None
        plan_error: Exception | None = None
        original_plan_prompt = plan_payload["prompt"]
        for attempt in range(MODEL_OUTPUT_ATTEMPTS):
            plan_result: Any = None
            try:
                plan_output = await _activity(
                    CoreTemporalTypes.GENERATE_TEXT,
                    plan_payload,
                    timeout=timedelta(minutes=12),
                )
                plan_result = _model_result(plan_output)
                narrative_plan = validate_narrative_plan(
                    plan_result,
                    duration_seconds=duration,
                    window_count=len(evidence_windows),
                )
                break
            except ActivityError as error:
                if not _is_degradable_activity_error(error):
                    raise
                plan_error = error
                break
            except (TypeError, ValueError, ValidationError) as error:
                plan_error = error
                if attempt + 1 < MODEL_OUTPUT_ATTEMPTS:
                    plan_payload = {
                        **plan_payload,
                        "prompt": _repair_prompt(original_plan_prompt, plan_result, error),
                    }
                    await report_progress(
                        "plan_narrative",
                        "叙事策划结构校验失败，正在自动修复",
                        {"percent": 53, "attempt": attempt + 2},
                    )
        if narrative_plan is None:
            narrative_plan = _fallback_narrative_plan(
                evidence_windows,
                duration_seconds=duration,
            )
            await report_progress(
                "plan_narrative",
                "叙事策划修正未成功，已按情节窗口生成保底策划",
                {"percent": 55, "degraded": True, "reason": str(plan_error)},
            )

        research_results: list[dict[str, Any]] = []
        queries = narrative_plan["researchQueries"]
        if not narrative_plan["researchNeeded"]:
            await report_progress(
                "research_story",
                "当前策划无需联网补充背景",
                {"percent": 57, "skipped": True, "total": 0},
            )
        else:
            failed_queries: list[str] = []
            await report_progress(
                "research_story",
                "正在联网核实策划所需的公开背景",
                {"percent": 56, "completed": 0, "total": len(queries)},
            )
            for query in queries:
                try:
                    search_output = await _activity(
                        CoreTemporalTypes.WEB_SEARCH,
                        {"model": models["search"], "query": query},
                        timeout=timedelta(seconds=75),
                    )
                    research_results.append(_normalize_search_output(search_output, query))
                except ActivityError as error:
                    if not _is_degradable_activity_error(error):
                        raise
                    failed_queries.append(query)
                except (TypeError, ValueError, ValidationError):
                    failed_queries.append(query)
                completed_queries = len(research_results) + len(failed_queries)
                await report_progress(
                    "research_story",
                    "部分检索不可用，正在使用已有证据继续"
                    if failed_queries
                    else "正在联网核实策划所需的公开背景",
                    {
                        "percent": 56 + round(4 * completed_queries / len(queries)),
                        "completed": completed_queries,
                        "total": len(queries),
                        **(
                            {"degraded": True, "failedQueries": len(failed_queries)}
                            if failed_queries
                            else {}
                        ),
                    },
                )

        await report_progress("generate_script", "正在按策划生成具体解说脚本", {"percent": 62})
        original_script_prompt = documentary_script_prompt(
            video_uri=args["videoUri"],
            duration_seconds=duration,
            narrative_plan=narrative_plan,
            evidence_windows=evidence_windows,
            research_results=research_results,
            instructions=args["instructions"],
            opening_cadence=opening_cadence,
            positioning_hook=positioning_hook,
            post_event_narration=post_event_narration,
            style=style,
        )
        script_payload = {
            "model": models["script"],
            "prompt": original_script_prompt,
            "outputSchema": SCRIPT_GENERATION_SCHEMA,
        }
        invalid_output: Exception | None = None
        script: dict[str, Any] | None = None
        last_valid_script: dict[str, Any] | None = None
        last_gaps: list[dict[str, float]] = []
        last_timing_issues: list[dict[str, Any]] = []
        for attempt in range(MODEL_OUTPUT_ATTEMPTS):
            script_result: Any = None
            try:
                generated = await _activity(
                    CoreTemporalTypes.GENERATE_TEXT,
                    script_payload,
                    timeout=timedelta(minutes=12),
                )
                script_result = _model_result(generated)
                candidate = validate_documentary_script(
                    script_result,
                    automatic=True,
                    source_uri=args["videoUri"],
                    source_duration=duration,
                    narrative_profile=narrative_profile_from_plan(narrative_plan),
                    window_count=len(evidence_windows),
                )
                timing_issues = (
                    post_event_timing_issues(candidate, evidence_windows)
                    if post_event_narration
                    else []
                )
                gaps = _cadence_gaps(
                    candidate,
                    raw_transcript_segments,
                    opening_cadence=opening_cadence,
                    style=style,
                )
                opening_gap = (
                    opening_hook_gap(candidate)
                    if opening_cadence and not post_event_narration
                    else None
                )
                last_valid_script = candidate
                last_gaps = gaps
                last_timing_issues = timing_issues
                if (timing_issues or opening_gap or gaps) and attempt + 1 < MODEL_OUTPUT_ATTEMPTS:
                    script_payload = {
                        **script_payload,
                        "prompt": pacing_revision_prompt(
                            original_script_prompt,
                            candidate,
                            gaps,
                            opening_gap,
                            opening_cadence=opening_cadence,
                            post_event_narration=post_event_narration,
                            timing_issues=timing_issues,
                            style=style,
                        ),
                    }
                    await report_progress(
                        "generate_script",
                        (
                            "发现部分解说可能早于事件，正在携带上一版脚本修订"
                            if timing_issues
                            else "发现开场或解说间隔偏长，正在调整解说位置"
                            if opening_cadence
                            else "发现超长非对白空档，正在宽松调整解说位置"
                        ),
                        {
                            "percent": 66 + attempt * 3,
                            "attempt": attempt + 2,
                            **({"openingHookGap": opening_gap} if opening_gap else {}),
                            **({"timingIssues": timing_issues} if timing_issues else {}),
                            "problemGaps": gaps,
                        },
                    )
                    continue
                script = (
                    _ensure_opening_hook(
                        candidate,
                        narrative_plan,
                        window_count=len(evidence_windows),
                    )
                    if opening_cadence and not post_event_narration
                    else candidate
                )
                if opening_cadence and not post_event_narration:
                    gaps = _cadence_gaps(
                        script,
                        raw_transcript_segments,
                        opening_cadence=True,
                        style=style,
                    )
                warnings = [
                    item
                    for item in (
                        _soft_timing_warning(timing_issues),
                        _cadence_warning(
                            gaps,
                            opening_cadence=opening_cadence,
                            post_event_narration=post_event_narration,
                            style=style,
                        )
                        if gaps
                        else "",
                    )
                    if item
                ]
                if warnings:
                    script = {
                        **script,
                        "pacingWarning": "；".join(warnings),
                    }
                break
            except ActivityError as error:
                if not _is_degradable_activity_error(error):
                    raise
                invalid_output = error
                break
            except (TypeError, ValueError, ValidationError) as error:
                invalid_output = error
                if attempt + 1 < MODEL_OUTPUT_ATTEMPTS:
                    script_payload = {
                        **script_payload,
                        "prompt": _repair_prompt(original_script_prompt, script_result, error),
                    }
                    await report_progress(
                        "generate_script",
                        "脚本结构校验失败，正在自动修复",
                        {"percent": 66 + attempt * 3, "attempt": attempt + 2},
                    )
        if script is None and last_valid_script is not None:
            script = (
                _ensure_opening_hook(
                    last_valid_script,
                    narrative_plan,
                    window_count=len(evidence_windows),
                )
                if opening_cadence and not post_event_narration
                else last_valid_script
            )
            if opening_cadence and not post_event_narration:
                last_gaps = _cadence_gaps(
                    script,
                    raw_transcript_segments,
                    opening_cadence=True,
                    style=style,
                )
            warnings = [
                item
                for item in (
                    _soft_timing_warning(last_timing_issues),
                    _cadence_warning(
                        last_gaps,
                        opening_cadence=opening_cadence,
                        post_event_narration=post_event_narration,
                        style=style,
                    )
                    if last_gaps
                    else "",
                )
                if item
            ]
            if warnings:
                script = {
                    **script,
                    "pacingWarning": "；".join(warnings),
                }
            await report_progress(
                "generate_script",
                "节奏重试已用尽，继续使用最后一版结构合法脚本",
                {
                    "percent": 74,
                    "pacingWarning": script.get("pacingWarning", ""),
                    "degraded": True,
                },
            )
        if script is None:
            script = _fallback_script(
                narrative_plan,
                video_uri=args["videoUri"],
                duration_seconds=duration,
                window_count=len(evidence_windows),
                opening_cadence=opening_cadence,
                post_event_narration=post_event_narration,
            )
            fallback_gaps = _cadence_gaps(
                script,
                raw_transcript_segments,
                opening_cadence=opening_cadence,
                style=style,
            )
            if fallback_gaps:
                script = {
                    **script,
                    "pacingWarning": _cadence_warning(
                        fallback_gaps,
                        opening_cadence=opening_cadence,
                        post_event_narration=post_event_narration,
                        style=style,
                    ),
                }
            await report_progress(
                "generate_script",
                "脚本结构修正未成功，已按叙事策划生成保底脚本",
                {
                    "percent": 74,
                    "degraded": True,
                    "reason": str(invalid_output),
                    **(
                        {"pacingWarning": script["pacingWarning"]}
                        if script.get("pacingWarning")
                        else {}
                    ),
                },
            )

        cue_seconds = sum(
            float(cue["endSeconds"]) - float(cue["startSeconds"])
            for cue in script["narrationCues"]
        )
        narration_characters = sum(
            len("".join(str(cue["narration"]).split())) for cue in script["narrationCues"]
        )
        diagnostics = {
            "coverageRatio": round(cue_seconds / duration, 3),
            "narrationCharacters": narration_characters,
            "estimatedSpeechSeconds": round(narration_characters / 3.15, 3),
            "cueCount": len(script["narrationCues"]),
        }
        await report_progress(
            "complete",
            "两阶段解说脚本生成完成",
            {
                "percent": 100,
                "total": len(script["narrationCues"]),
                "pacingDiagnostics": diagnostics,
                **(
                    {"pacingWarning": script["pacingWarning"]}
                    if script.get("pacingWarning")
                    else {}
                ),
            },
        )
        return script


@workflow.defn(name=RENDER_WORKFLOW_TYPE)
class RenderDocumentaryVideoWorkflow:
    @workflow.run
    async def run(self, raw_input: dict[str, Any]) -> dict[str, Any]:
        settings = workflow_config.get_settings()
        try:
            args = normalize_render_input(raw_input)
            tts = settings.resolve_tts(args["tts"])
        except (ValueError, ValidationError) as error:
            raise _invalid(error) from error
        script = args["script"]

        await report_progress("validate_input", "正在验证脚本和源视频", {"percent": 5})
        probe = await _activity(
            CoreTemporalTypes.PROBE_VIDEO,
            {"videoUrl": script["sourceVideoUri"]},
            timeout=timedelta(minutes=8),
        )
        try:
            duration, _, has_audio, source_width, source_height = _validate_probe(probe)
            if abs(duration - float(script["sourceDurationSeconds"])) > 0.5:
                raise ValidationError(
                    "script source duration does not match the current source video"
                )
        except (TypeError, ValueError, ValidationError) as error:
            raise _invalid(error) from error

        narrated = script["narrationCues"]

        async def synthesize(
            segment: dict[str, Any],
        ) -> tuple[str, dict[str, Any], str | None]:
            provider = tts["provider"]
            request: dict[str, Any] = {
                "model": tts["model"],
                "text": segment["narration"],
                "voiceId": tts["voiceId"],
            }
            warning: str | None = None
            if provider == "qwen":
                request["languageType"] = "Chinese"
                if tts["model"] == QWEN_TTS_INSTRUCT_MODEL:
                    request["instructions"] = " ".join(
                        item
                        for item in (
                            str(script["narrativeProfile"]["globalTtsInstruction"]).strip(),
                            str(segment["ttsInstruction"]).strip(),
                        )
                        if item
                    )
                activity_name = CoreTemporalTypes.GENERATE_SPEECH_QWEN
                operation = "generate_speech_qwen"
            else:
                emotion = str(segment.get("emotion") or "").strip().lower()
                special_emotion_models = {
                    "speech-2.6-hd",
                    "speech-2.6-turbo",
                    "minimax-speech-2.6-hd",
                    "minimax-speech-2.6-turbo",
                }
                if emotion in {"fluent", "whisper"} and tts["model"] not in special_emotion_models:
                    warning = (
                        f"{segment['id']} 的 MiniMax emotion={emotion} 与模型 "
                        f"{tts['model']} 不兼容，已省略并使用自动情绪"
                    )
                elif emotion:
                    request["emotion"] = emotion
                activity_name = CoreTemporalTypes.GENERATE_SPEECH_MINIMAX
                operation = "generate_speech_minimax"
            output = await _activity(
                activity_name,
                request,
                timeout=timedelta(minutes=7),
                retry_policy=_RENDER_RETRY,
            )
            try:
                if not isinstance(output, dict):
                    raise ValidationError(f"TTS result for {segment['id']} must be an object")
                audio_uri = first_artifact_uri(
                    output,
                    operation,
                    expected_type="audio",
                )
                metadata = output.get("metadata") or {}
                if not isinstance(metadata, dict):
                    raise ValidationError(f"TTS result for {segment['id']} has invalid metadata")
                length_ms = metadata.get("audioLengthMs")
                if length_ms is None:
                    # Current Qwen/DashScope Core output contains the managed
                    # audio Artifact but may omit its duration. Probe that exact
                    # Artifact with Core ffprobe so the timeline still uses a
                    # measured duration rather than an estimate from the text.
                    audio_probe = await _activity(
                        CoreTemporalTypes.PROBE_VIDEO,
                        {"videoUrl": audio_uri},
                        timeout=timedelta(minutes=8),
                        retry_policy=_RENDER_RETRY,
                    )
                    duration = (
                        audio_probe.get("durationSeconds")
                        if isinstance(audio_probe, dict)
                        else None
                    )
                    if (
                        isinstance(duration, bool)
                        or not isinstance(duration, (int, float))
                        or duration <= 0
                    ):
                        raise ValidationError(
                            f"TTS result for {segment['id']} has no measurable duration"
                        )
                    output = {
                        **output,
                        "metadata": {
                            **metadata,
                            "audioLengthMs": round(float(duration) * 1000),
                        },
                    }
            except (TypeError, ValueError, ValidationError) as error:
                raise _invalid(error) from error
            return segment["id"], output, warning

        await report_progress(
            "synthesize_speech",
            "正在生成解说配音",
            {"percent": 45, "completed": 0, "total": len(narrated)},
        )
        tts_results: dict[str, dict[str, Any]] = {}
        render_warnings: list[str] = []
        for start in range(0, len(narrated), settings.tts_concurrency):
            chunk = await asyncio.gather(
                *(synthesize(item) for item in narrated[start : start + settings.tts_concurrency])
            )
            for cue_id, output, warning in chunk:
                tts_results[cue_id] = output
                if warning:
                    render_warnings.append(warning)
            completed = len(tts_results)
            percent = 45 if not narrated else 45 + round(15 * completed / len(narrated))
            await report_progress(
                "synthesize_speech",
                "正在生成解说配音",
                {"percent": percent, "completed": completed, "total": len(narrated)},
            )

        try:
            render_timeline = resolve_render_timeline(script, tts_results)
            render_segments = render_timeline.segments
            script = render_timeline.script
            render_warnings.extend(render_timeline.warnings)
        except ValidationError as error:
            raise _invalid(error) from error
        if render_warnings:
            await report_progress(
                "resolve_timeline",
                "已采用宽松策略调整实际配音时间轴",
                {"percent": 61, "warnings": render_warnings},
            )

        original_audio_segments = [
            item
            for item in render_segments
            if has_audio and args["originalAudioVolume"] > 0 and item.ost == 2
        ]

        async def extract_original_audio(segment: Any) -> tuple[str, str]:
            output = await _activity(
                CoreTemporalTypes.EXTRACT_AUDIO,
                {
                    "videoUrl": script["sourceVideoUri"],
                    "startSeconds": segment.source_start_seconds,
                    "durationSeconds": segment.duration_seconds,
                    "format": "mp3",
                    "bitRate": "128k",
                    "sampleRate": 48000,
                    "channels": 2,
                    "normalizeLoudness": False,
                },
                timeout=timedelta(minutes=13),
                retry_policy=_RENDER_RETRY,
            )
            try:
                return segment.id, first_artifact_uri(
                    output, "extract_audio", expected_type="audio"
                )
            except (TypeError, ValueError) as error:
                raise ApplicationError(
                    str(error), type="InvalidCoreOutput", non_retryable=True
                ) from error

        await report_progress(
            "lower_original_audio",
            "正在提取并压低原视频声音",
            {"percent": 60, "completed": 0, "total": len(original_audio_segments)},
        )
        original_audio_uris: dict[str, str] = {}
        for start in range(0, len(original_audio_segments), settings.tts_concurrency):
            chunk = await asyncio.gather(
                *(
                    extract_original_audio(item)
                    for item in original_audio_segments[start : start + settings.tts_concurrency]
                )
            )
            original_audio_uris.update(chunk)
            completed = len(original_audio_uris)
            await report_progress(
                "lower_original_audio",
                "正在提取并压低原视频声音",
                {
                    "percent": 60
                    if not original_audio_segments
                    else 60 + round(7 * completed / len(original_audio_segments)),
                    "completed": completed,
                    "total": len(original_audio_segments),
                },
            )

        async def cut(segment: Any) -> tuple[str, str]:
            output = await _activity(
                CoreTemporalTypes.PROCESS_VIDEO,
                {
                    "videoUrl": script["sourceVideoUri"],
                    "operations": build_process_operations(segment, args["aspect"]),
                    "quality": "balanced",
                },
                timeout=timedelta(minutes=13),
                retry_policy=_RENDER_RETRY,
            )
            try:
                return segment.id, first_artifact_uri(
                    output, "process_video", expected_type="video"
                )
            except (TypeError, ValueError) as error:
                raise ApplicationError(
                    str(error), type="InvalidCoreOutput", non_retryable=True
                ) from error

        await report_progress(
            "cut_segments",
            "正在裁剪视频片段",
            {"percent": 67, "completed": 0, "total": len(render_segments)},
        )
        clip_uris: dict[str, str] = {}
        for start in range(0, len(render_segments), settings.tts_concurrency):
            chunk = await asyncio.gather(
                *(cut(item) for item in render_segments[start : start + settings.tts_concurrency])
            )
            clip_uris.update(chunk)
            completed = len(clip_uris)
            await report_progress(
                "cut_segments",
                "正在裁剪视频片段",
                {
                    "percent": 67 + round(11 * completed / len(render_segments)),
                    "completed": completed,
                    "total": len(render_segments),
                },
            )

        output_width, output_height = (
            ASPECT_DIMENSIONS[args["aspect"]]
            if args["aspect"] != "source"
            else (source_width, source_height)
        )
        batches = build_compose_batches(
            render_segments,
            clip_uris,
            narration_volume=args["narrationVolume"],
            original_audio_uris=original_audio_uris,
            original_audio_volume=args["originalAudioVolume"],
            subtitles=args["subtitles"],
            video_width=output_width,
            video_height=output_height,
        )
        await report_progress(
            "compose_batches",
            "正在合成视频批次",
            {"percent": 78, "completed": 0, "total": len(batches)},
        )
        batch_outputs: list[dict[str, Any]] = []
        for index, batch in enumerate(batches, start=1):
            output = await _activity(
                CoreTemporalTypes.COMPOSE_VIDEO,
                batch,
                timeout=timedelta(minutes=18),
                retry_policy=_RENDER_RETRY,
            )
            batch_outputs.append(output)
            await report_progress(
                "compose_batches",
                "正在合成视频批次",
                {
                    "percent": 78 + round(18 * index / len(batches)),
                    "completed": index,
                    "total": len(batches),
                },
            )

        try:
            if len(batch_outputs) == 1:
                final_output = batch_outputs[0]
            else:
                final_output = await _activity(
                    CoreTemporalTypes.CONCAT_VIDEOS,
                    {
                        "videoUrls": [
                            first_artifact_uri(item, "compose_video", expected_type="video")
                            for item in batch_outputs
                        ],
                        "size": "auto",
                    },
                    timeout=timedelta(minutes=18),
                    retry_policy=_RENDER_RETRY,
                )
            final_uri = first_artifact_uri(final_output, "finalize_video", expected_type="video")
        except (TypeError, ValueError) as error:
            raise ApplicationError(
                str(error), type="InvalidCoreOutput", non_retryable=True
            ) from error

        await report_progress("finalize", "正在整理最终产物", {"percent": 98})
        total_duration = round(duration, 3)
        artifacts = list(final_output.get("artifacts") or [])
        artifacts[0] = {
            **artifacts[0],
            "uri": final_uri,
            "description": "纪录片解说成片",
        }
        result = {
            "script": script,
            "artifacts": artifacts,
            "durationSeconds": total_duration,
            "segmentCount": len(script["narrationCues"]),
            **({"warnings": render_warnings} if render_warnings else {}),
        }
        await report_progress(
            "complete",
            "纪录片解说视频生成完成",
            {
                "percent": 100,
                "total": len(script["narrationCues"]),
                **({"warnings": render_warnings} if render_warnings else {}),
                **(
                    {"pacingWarning": script["pacingWarning"]}
                    if script.get("pacingWarning")
                    else {}
                ),
            },
        )
        return result


@workflow.defn(name=CREATE_WORKFLOW_TYPE)
class CreateDocumentaryVideoWorkflow:
    @workflow.run
    async def run(self, raw_input: dict[str, Any]) -> dict[str, Any]:
        settings = workflow_config.get_settings()
        try:
            args = normalize_create_input(raw_input)
        except ValidationError as error:
            raise _invalid(error) from error
        workflow_id = workflow.info().workflow_id
        script_request = {
            "videoUri": args["videoUri"],
            "instructions": args["instructions"],
            "models": args["models"],
            "analysisOptions": args["analysisOptions"],
            "style": args["style"],
        }
        script_call_id = _next_call_id(GENERATE_WORKFLOW_TYPE)
        await _report_request(
            "workflow", GENERATE_WORKFLOW_TYPE, script_request, call_id=script_call_id
        )
        script = await workflow.execute_child_workflow(
            GENERATE_WORKFLOW_TYPE,
            script_request,
            id=f"{workflow_id}/script",
            task_queue=settings.worker_task_queue,
            execution_timeout=timedelta(hours=3),
            cancellation_type=workflow.ChildWorkflowCancellationType.WAIT_CANCELLATION_COMPLETED,
        )
        await _report_response(
            "workflow", GENERATE_WORKFLOW_TYPE, script, call_id=script_call_id
        )
        render_request = {
            "script": script,
            "tts": args["tts"],
            "aspect": args["aspect"],
            "narrationVolume": args["narrationVolume"],
            "originalAudioVolume": args["originalAudioVolume"],
            "subtitles": args["subtitles"],
        }
        render_call_id = _next_call_id(RENDER_WORKFLOW_TYPE)
        await _report_request(
            "workflow", RENDER_WORKFLOW_TYPE, render_request, call_id=render_call_id
        )
        rendered = await workflow.execute_child_workflow(
            RENDER_WORKFLOW_TYPE,
            render_request,
            id=f"{workflow_id}/render",
            task_queue=settings.worker_task_queue,
            execution_timeout=timedelta(hours=21),
            cancellation_type=workflow.ChildWorkflowCancellationType.WAIT_CANCELLATION_COMPLETED,
        )
        await _report_response(
            "workflow", RENDER_WORKFLOW_TYPE, rendered, call_id=render_call_id
        )
        return rendered
