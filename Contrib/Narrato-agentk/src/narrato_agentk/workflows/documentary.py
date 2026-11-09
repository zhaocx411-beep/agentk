from __future__ import annotations

import asyncio
from datetime import timedelta
from typing import Any

from agentk.worker import report_progress
from temporalio import workflow
from temporalio.common import RetryPolicy
from temporalio.exceptions import ApplicationError

from narrato_agentk.agentk_core import (
    CORE_TASK_QUEUE,
    ActivityNames,
    first_artifact_uri,
    text_result,
)
from narrato_agentk.contracts import (
    DOCUMENTARY_SCRIPT_SCHEMA,
    MAX_VIDEO_BYTES,
    ValidationError,
    build_analysis_windows,
    build_compose_batches,
    build_process_operations,
    normalize_create_input,
    normalize_render_input,
    resolve_render_segments,
    validate_documentary_script,
)
from narrato_agentk.contracts.models import normalize_generate_input
from narrato_agentk.prompts import analysis_prompt, script_prompt
from narrato_agentk.settings import Settings

GENERATE_CAPABILITY_ID = "contrib.narrato.generate_documentary_script"
RENDER_CAPABILITY_ID = "contrib.narrato.render_documentary_video"
CREATE_CAPABILITY_ID = "contrib.narrato.create_documentary_video"

GENERATE_WORKFLOW_TYPE = "narrato.documentary.generate_script.v1"
RENDER_WORKFLOW_TYPE = "narrato.documentary.render_video.v1"
CREATE_WORKFLOW_TYPE = "narrato.documentary.create_video.v1"

_SETTINGS = Settings()
_CORE_RETRY = RetryPolicy(
    initial_interval=timedelta(seconds=1),
    backoff_coefficient=2,
    maximum_interval=timedelta(seconds=30),
    maximum_attempts=2,
    non_retryable_error_types=["InvalidArgument", "Configuration"],
)


def configure(settings: Settings) -> None:
    global _SETTINGS, CORE_TASK_QUEUE
    _SETTINGS = settings
    CORE_TASK_QUEUE = settings.core_task_queue


async def _activity(name: str, payload: dict[str, Any], *, timeout: timedelta) -> Any:
    return await workflow.execute_activity(
        name,
        payload,
        task_queue=CORE_TASK_QUEUE,
        start_to_close_timeout=timeout,
        schedule_to_close_timeout=timeout + timedelta(minutes=2),
        retry_policy=_CORE_RETRY,
    )


def _invalid(error: Exception) -> ApplicationError:
    return ApplicationError(str(error), type="InvalidArgument", non_retryable=True)


def _validate_probe(output: Any) -> tuple[float, int]:
    if not isinstance(output, dict):
        raise ValidationError("probe_video returned a non-object result")
    size = int(output.get("fileSizeBytes") or 0)
    duration = float(output.get("durationSeconds") or 0)
    if size <= 0 or size > MAX_VIDEO_BYTES:
        raise ValidationError(f"video size must be between 1 and {MAX_VIDEO_BYTES} bytes")
    if duration <= 0:
        raise ValidationError("video duration must be greater than 0")
    if not output.get("videoStreams"):
        raise ValidationError("input contains no video stream")
    return duration, size


async def _in_chunks(items: list[Any], size: int, fn: Any) -> list[Any]:
    output: list[Any] = []
    for start in range(0, len(items), size):
        output.extend(await asyncio.gather(*(fn(item) for item in items[start : start + size])))
    return output


@workflow.defn(name=GENERATE_WORKFLOW_TYPE)
class GenerateDocumentaryScriptWorkflow:
    @workflow.run
    async def run(self, raw_input: dict[str, Any]) -> dict[str, Any]:
        try:
            args = normalize_generate_input(raw_input)
        except ValidationError as error:
            raise _invalid(error) from error

        await report_progress("validate_input", "正在验证输入视频", {"percent": 5})
        probe = await _activity(
            ActivityNames.PROBE_VIDEO,
            {"videoUrl": args["videoUri"]},
            timeout=timedelta(minutes=8),
        )
        try:
            duration, _ = _validate_probe(probe)
            windows = build_analysis_windows(duration, _SETTINGS.analysis_window_seconds)
        except (TypeError, ValueError, ValidationError) as error:
            raise _invalid(error) from error

        async def analyze(window: dict[str, Any]) -> dict[str, Any]:
            result = await _activity(
                ActivityNames.UNDERSTAND_VIDEO_SEGMENT,
                {
                    "model": _SETTINGS.vision_model,
                    "videoUrl": args["videoUri"],
                    "startSeconds": window["startSeconds"],
                    "endSeconds": window["endSeconds"],
                    "prompt": analysis_prompt(
                        start_seconds=float(window["startSeconds"]),
                        end_seconds=float(window["endSeconds"]),
                        theme=args["theme"],
                        instructions=args["instructions"],
                    ),
                },
                timeout=timedelta(minutes=8),
            )
            if not isinstance(result, dict) or not str(result.get("text") or "").strip():
                raise ApplicationError(
                    f"video analysis window {window['index']} returned no text",
                    type="InvalidModelOutput",
                    non_retryable=True,
                )
            return {**window, "analysis": str(result["text"]).strip()}

        await report_progress(
            "analyze_video",
            "正在分段理解视频",
            {"percent": 10, "completed": 0, "total": len(windows)},
        )
        analyses: list[dict[str, Any]] = []
        for start in range(0, len(windows), _SETTINGS.analysis_concurrency):
            chunk = await asyncio.gather(
                *(analyze(item) for item in windows[start : start + _SETTINGS.analysis_concurrency])
            )
            analyses.extend(chunk)
            completed = len(analyses)
            percent = 10 + round(25 * completed / len(windows))
            await report_progress(
                "analyze_video",
                "正在分段理解视频",
                {"percent": percent, "completed": completed, "total": len(windows)},
            )

        await report_progress("generate_script", "正在生成结构化解说脚本", {"percent": 40})
        generation_payload = {
            "model": _SETTINGS.text_model,
            "prompt": script_prompt(
                video_uri=args["videoUri"],
                duration_seconds=duration,
                theme=args["theme"],
                instructions=args["instructions"],
                analyses=analyses,
            ),
            "outputSchema": DOCUMENTARY_SCRIPT_SCHEMA,
        }
        invalid_output: Exception | None = None
        script: dict[str, Any] | None = None
        original_prompt = generation_payload["prompt"]
        for attempt in range(2):
            generated = await _activity(
                ActivityNames.GENERATE_TEXT,
                generation_payload,
                timeout=timedelta(minutes=12),
            )
            try:
                result = text_result(generated)
                script = validate_documentary_script(
                    result,
                    automatic=True,
                    source_uri=args["videoUri"],
                    source_duration=duration,
                    theme=args["theme"],
                )
                break
            except (TypeError, ValueError, ValidationError) as error:
                invalid_output = error
                if attempt == 0:
                    generation_payload = {
                        **generation_payload,
                        "prompt": (
                            original_prompt
                            + "\n上一次输出未通过本地校验："
                            + str(error)
                            + "。请只返回修正后的完整 JSON。"
                        ),
                    }
                    await report_progress(
                        "generate_script",
                        "结构化脚本校验失败，正在重试一次",
                        {"percent": 40, "attempt": 2},
                    )
        if script is None:
            raise ApplicationError(
                str(invalid_output), type="InvalidModelOutput", non_retryable=True
            ) from invalid_output
        await report_progress(
            "complete",
            "解说脚本生成完成",
            {"percent": 100, "total": len(script["segments"])},
        )
        return script


@workflow.defn(name=RENDER_WORKFLOW_TYPE)
class RenderDocumentaryVideoWorkflow:
    @workflow.run
    async def run(self, raw_input: dict[str, Any]) -> dict[str, Any]:
        try:
            args = normalize_render_input(raw_input)
        except ValidationError as error:
            raise _invalid(error) from error
        script = args["script"]

        await report_progress("validate_input", "正在验证脚本和源视频", {"percent": 5})
        probe = await _activity(
            ActivityNames.PROBE_VIDEO,
            {"videoUrl": script["sourceVideoUri"]},
            timeout=timedelta(minutes=8),
        )
        try:
            duration, _ = _validate_probe(probe)
            if abs(duration - float(script["sourceDurationSeconds"])) > 0.5:
                raise ValidationError(
                    "script source duration does not match the current source video"
                )
        except (TypeError, ValueError, ValidationError) as error:
            raise _invalid(error) from error

        narrated = [item for item in script["segments"] if item["ost"] in (0, 2)]

        async def synthesize(segment: dict[str, Any]) -> tuple[str, dict[str, Any]]:
            output = await _activity(
                ActivityNames.GENERATE_SPEECH_QWEN,
                {
                    "model": _SETTINGS.tts_model,
                    "text": segment["narration"],
                    "voiceId": args["voiceId"],
                    "languageType": "Chinese",
                },
                timeout=timedelta(minutes=7),
            )
            try:
                if not isinstance(output, dict):
                    raise ValidationError(f"TTS result for {segment['id']} must be an object")
                metadata = output.get("metadata") or {}
                if not isinstance(metadata, dict):
                    raise ValidationError(f"TTS result for {segment['id']} has invalid metadata")
                length_ms = metadata.get("audioLengthMs")
                if length_ms is None:
                    # Current Qwen/DashScope Core output contains the managed
                    # audio Artifact but may omit its duration. Probe that exact
                    # Artifact with Core ffprobe so the timeline still uses a
                    # measured duration rather than an estimate from the text.
                    audio_uri = first_artifact_uri(output, "generate_speech_qwen")
                    audio_probe = await _activity(
                        ActivityNames.PROBE_VIDEO,
                        {"videoUrl": audio_uri},
                        timeout=timedelta(minutes=8),
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
            return segment["id"], output

        await report_progress(
            "synthesize_speech",
            "正在生成解说配音",
            {"percent": 45, "completed": 0, "total": len(narrated)},
        )
        tts_results: dict[str, dict[str, Any]] = {}
        for start in range(0, len(narrated), _SETTINGS.tts_concurrency):
            chunk = await asyncio.gather(
                *(synthesize(item) for item in narrated[start : start + _SETTINGS.tts_concurrency])
            )
            tts_results.update(chunk)
            completed = len(tts_results)
            percent = 45 if not narrated else 45 + round(15 * completed / len(narrated))
            await report_progress(
                "synthesize_speech",
                "正在生成解说配音",
                {"percent": percent, "completed": completed, "total": len(narrated)},
            )

        try:
            render_segments = resolve_render_segments(script, tts_results)
        except ValidationError as error:
            raise _invalid(error) from error

        async def cut(segment: Any) -> tuple[str, str]:
            output = await _activity(
                ActivityNames.PROCESS_VIDEO,
                {
                    "videoUrl": script["sourceVideoUri"],
                    "operations": build_process_operations(segment, args["aspect"]),
                    "quality": "balanced",
                },
                timeout=timedelta(minutes=13),
            )
            try:
                return segment.id, first_artifact_uri(output, "process_video")
            except (TypeError, ValueError) as error:
                raise ApplicationError(
                    str(error), type="InvalidCoreOutput", non_retryable=True
                ) from error

        await report_progress(
            "cut_segments",
            "正在裁剪视频片段",
            {"percent": 60, "completed": 0, "total": len(render_segments)},
        )
        clip_uris: dict[str, str] = {}
        for start in range(0, len(render_segments), _SETTINGS.tts_concurrency):
            chunk = await asyncio.gather(
                *(cut(item) for item in render_segments[start : start + _SETTINGS.tts_concurrency])
            )
            clip_uris.update(chunk)
            completed = len(clip_uris)
            await report_progress(
                "cut_segments",
                "正在裁剪视频片段",
                {
                    "percent": 60 + round(15 * completed / len(render_segments)),
                    "completed": completed,
                    "total": len(render_segments),
                },
            )

        batches = build_compose_batches(
            render_segments,
            clip_uris,
            narration_volume=args["narrationVolume"],
            subtitles=args["subtitles"],
        )
        await report_progress(
            "compose_batches",
            "正在合成视频批次",
            {"percent": 75, "completed": 0, "total": len(batches)},
        )
        batch_outputs: list[dict[str, Any]] = []
        for index, batch in enumerate(batches, start=1):
            output = await _activity(
                ActivityNames.COMPOSE_VIDEO,
                batch,
                timeout=timedelta(minutes=18),
            )
            batch_outputs.append(output)
            await report_progress(
                "compose_batches",
                "正在合成视频批次",
                {
                    "percent": 75 + round(20 * index / len(batches)),
                    "completed": index,
                    "total": len(batches),
                },
            )

        try:
            if len(batch_outputs) == 1:
                final_output = batch_outputs[0]
            else:
                final_output = await _activity(
                    ActivityNames.CONCAT_VIDEOS,
                    {
                        "videoUrls": [
                            first_artifact_uri(item, "compose_video") for item in batch_outputs
                        ],
                        "size": "auto",
                    },
                    timeout=timedelta(minutes=18),
                )
            final_uri = first_artifact_uri(final_output, "finalize_video")
        except (TypeError, ValueError) as error:
            raise ApplicationError(
                str(error), type="InvalidCoreOutput", non_retryable=True
            ) from error

        await report_progress("finalize", "正在整理最终产物", {"percent": 98})
        total_duration = round(sum(item.duration_seconds for item in render_segments), 3)
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
            "segmentCount": len(render_segments),
        }
        await report_progress(
            "complete",
            "纪录片解说视频生成完成",
            {"percent": 100, "total": len(render_segments)},
        )
        return result


@workflow.defn(name=CREATE_WORKFLOW_TYPE)
class CreateDocumentaryVideoWorkflow:
    @workflow.run
    async def run(self, raw_input: dict[str, Any]) -> dict[str, Any]:
        try:
            args = normalize_create_input(raw_input)
        except ValidationError as error:
            raise _invalid(error) from error
        workflow_id = workflow.info().workflow_id
        script = await workflow.execute_child_workflow(
            GENERATE_WORKFLOW_TYPE,
            {
                "videoUri": args["videoUri"],
                "theme": args["theme"],
                "instructions": args["instructions"],
            },
            id=f"{workflow_id}/script",
            task_queue=_SETTINGS.worker_task_queue,
            execution_timeout=timedelta(hours=3),
            cancellation_type=workflow.ChildWorkflowCancellationType.WAIT_CANCELLATION_COMPLETED,
        )
        return await workflow.execute_child_workflow(
            RENDER_WORKFLOW_TYPE,
            {
                "script": script,
                "voiceId": args["voiceId"],
                "aspect": args["aspect"],
                "narrationVolume": args["narrationVolume"],
                "subtitles": args["subtitles"],
            },
            id=f"{workflow_id}/render",
            task_queue=_SETTINGS.worker_task_queue,
            execution_timeout=timedelta(hours=21),
            cancellation_type=workflow.ChildWorkflowCancellationType.WAIT_CANCELLATION_COMPLETED,
        )
