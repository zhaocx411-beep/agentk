from __future__ import annotations

import json
import os
from typing import Any

CORE_TASK_QUEUE = os.getenv("NARRATO_CORE_TASK_QUEUE", "agentk").strip() or "agentk"


class ActivityNames:
    PROBE_VIDEO = "agentk.capability.probe_video"
    UNDERSTAND_VIDEO_SEGMENT = "agentk.capability.understand_video_segment"
    GENERATE_TEXT = "agentk.capability.generate_text"
    GENERATE_SPEECH_QWEN = "agentk.capability.generate_speech_qwen"
    PROCESS_VIDEO = "agentk.capability.process_video"
    COMPOSE_VIDEO = "agentk.capability.compose_video"
    CONCAT_VIDEOS = "agentk.capability.concat_videos"


CORE_CAPABILITY_IDS = frozenset(
    {
        "agentk.probe_video",
        "agentk.understand_video_segment",
        "agentk.generate_text",
        "agentk.generate_speech_qwen",
        "agentk.process_video",
        "agentk.compose_video",
        "agentk.concat_videos",
    }
)

# concat_videos is registered as a Core Temporal Activity but is not currently
# published in AgentK's consumer Capability catalog.
DISCOVERABLE_CORE_CAPABILITY_IDS = CORE_CAPABILITY_IDS - {"agentk.concat_videos"}

CORE_SCHEMA_PROPERTIES = {
    "agentk.probe_video": ({"videoUrl"}, {"fileSizeBytes", "durationSeconds", "videoStreams"}),
    "agentk.understand_video_segment": (
        {"model", "videoUrl", "startSeconds", "endSeconds", "prompt"},
        {"text"},
    ),
    "agentk.generate_text": ({"model", "prompt", "outputSchema"}, {"result"}),
    "agentk.generate_speech_qwen": (
        {"model", "text", "voiceId"},
        {"artifacts", "metadata"},
    ),
    "agentk.process_video": ({"videoUrl", "operations"}, {"artifacts"}),
    "agentk.compose_video": (
        {"videoUrls", "audioTracks", "preserveOriginalAudio", "subtitleSegments"},
        {"artifacts"},
    ),
}


def first_artifact_uri(output: Any, operation: str) -> str:
    if not isinstance(output, dict):
        raise TypeError(f"{operation} returned a non-object result")
    artifacts = output.get("artifacts")
    if not isinstance(artifacts, list) or not artifacts or not isinstance(artifacts[0], dict):
        raise ValueError(f"{operation} returned no artifact")
    uri = str(artifacts[0].get("uri") or "").strip()
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
