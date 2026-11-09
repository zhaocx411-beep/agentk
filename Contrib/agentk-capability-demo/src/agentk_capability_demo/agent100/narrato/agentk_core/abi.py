from __future__ import annotations

import json
import os
from typing import Any

CORE_TASK_QUEUE = os.getenv("NARRATO_CORE_TASK_QUEUE", "agentk").strip() or "agentk"


class CoreTemporalTypes:
    PROBE_VIDEO = "agentk.capability.probe_video"
    EXTRACT_AUDIO = "agentk.capability.extract_audio"
    TRANSCRIBE_AUDIO = "agentk.capability.transcribe_audio"
    WEB_SEARCH = "agentk.capability.web_search"
    UNDERSTAND_VIDEO_SEGMENT = "agentk.capability.understand_video_segment"
    GENERATE_TEXT = "agentk.capability.generate_text"
    GENERATE_SPEECH_QWEN = "agentk.capability.generate_speech_qwen"
    GENERATE_SPEECH_MINIMAX = "agentk.capability.generate_speech_minimax"
    PROCESS_VIDEO = "agentk.capability.process_video"
    COMPOSE_VIDEO = "agentk.capability.compose_video"
    CONCAT_VIDEOS = "agentk.capability.concat_videos"


CORE_CAPABILITY_IDS = frozenset(
    {
        "agentk.probe_video",
        "agentk.extract_audio",
        "agentk.transcribe_audio",
        "agentk.web_search",
        "agentk.understand_video_segment",
        "agentk.generate_text",
        "agentk.generate_speech_qwen",
        "agentk.generate_speech_minimax",
        "agentk.process_video",
        "agentk.compose_video",
        "agentk.concat_videos",
    }
)

# concat_videos is registered as a Core Temporal Activity but is not currently
# published in AgentK's consumer Capability catalog.
DISCOVERABLE_CORE_CAPABILITY_IDS = CORE_CAPABILITY_IDS - {"agentk.concat_videos"}
TTS_CORE_CAPABILITY_IDS = frozenset(
    {"agentk.generate_speech_qwen", "agentk.generate_speech_minimax"}
)
REQUIRED_CORE_CAPABILITY_IDS = DISCOVERABLE_CORE_CAPABILITY_IDS - TTS_CORE_CAPABILITY_IDS

CORE_SCHEMA_PROPERTIES = {
    "agentk.probe_video": ({"videoUrl"}, {"fileSizeBytes", "durationSeconds", "videoStreams"}),
    "agentk.extract_audio": (
        {"videoUrl", "startSeconds", "durationSeconds", "format", "sampleRate", "channels"},
        {"artifacts", "durationSeconds"},
    ),
    "agentk.transcribe_audio": (
        {"model", "audioUrl", "task", "language", "prompt"},
        {"text", "segments", "model", "task"},
    ),
    "agentk.web_search": (
        {"model", "query", "timeRange"},
        {"results", "model", "query", "timeRange"},
    ),
    "agentk.understand_video_segment": (
        {"model", "videoUrl", "startSeconds", "endSeconds", "prompt"},
        {"text"},
    ),
    "agentk.generate_text": ({"model", "prompt", "outputSchema"}, {"result"}),
    "agentk.generate_speech_qwen": (
        {"model", "text", "voiceId", "instructions"},
        {"artifacts", "metadata"},
    ),
    "agentk.generate_speech_minimax": (
        {"model", "text", "voiceId", "emotion"},
        {"artifacts", "metadata"},
    ),
    "agentk.process_video": ({"videoUrl", "operations"}, {"artifacts"}),
    "agentk.compose_video": (
        {"videoUrls", "audioTracks", "preserveOriginalAudio", "subtitleSegments"},
        {"artifacts"},
    ),
}


def first_artifact_uri(
    output: Any,
    operation: str,
    *,
    expected_type: str | None = None,
) -> str:
    if not isinstance(output, dict):
        raise TypeError(f"{operation} returned a non-object result")
    artifacts = output.get("artifacts")
    if not isinstance(artifacts, list) or not artifacts or not isinstance(artifacts[0], dict):
        raise ValueError(f"{operation} returned no artifact")
    artifact = artifacts[0]
    if expected_type is not None and artifact.get("type") != expected_type:
        raise ValueError(f"{operation} returned a non-{expected_type} artifact")
    uri = str(artifact.get("uri") or "").strip()
    if not uri:
        raise ValueError(f"{operation} returned an artifact without URI")
    return uri


def text_result(output: Any) -> Any:
    if not isinstance(output, dict) or "result" not in output:
        raise ValueError("generate_text returned no result")
    result = output["result"]
    if isinstance(result, str):
        try:
            return json.loads(result)
        except json.JSONDecodeError as error:
            raise ValueError("generate_text result is not valid JSON") from error
    return result
