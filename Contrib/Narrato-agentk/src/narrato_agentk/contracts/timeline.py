from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from .models import ValidationError

COMPOSE_AUDIO_TRACK_LIMIT = 8
ASPECT_DIMENSIONS = {
    "16:9": (1920, 1080),
    "9:16": (1080, 1920),
    "1:1": (1080, 1080),
}


@dataclass(frozen=True, slots=True)
class RenderSegment:
    id: str
    source_start_seconds: float
    duration_seconds: float
    narration: str
    ost: int
    audio_uri: str | None


def resolve_render_segments(
    script: dict[str, Any],
    tts_results: dict[str, dict[str, Any]],
) -> list[RenderSegment]:
    source_duration = float(script["sourceDurationSeconds"])
    resolved: list[RenderSegment] = []
    for segment in script["segments"]:
        segment_id = segment["id"]
        ost = int(segment["ost"])
        audio_uri: str | None = None
        if ost in (0, 2):
            result = tts_results.get(segment_id)
            if not result:
                raise ValidationError(f"missing TTS result for {segment_id}")
            if not isinstance(result, dict):
                raise ValidationError(f"TTS result for {segment_id} must be an object")
            artifacts = result.get("artifacts")
            metadata = result.get("metadata") or {}
            if (
                not isinstance(artifacts, list)
                or not artifacts
                or not isinstance(artifacts[0], dict)
            ):
                raise ValidationError(f"TTS result for {segment_id} has no artifact")
            if not isinstance(metadata, dict):
                raise ValidationError(f"TTS result for {segment_id} has invalid metadata")
            audio_uri = str(artifacts[0].get("uri") or "").strip()
            length_ms = metadata.get("audioLengthMs")
            if not audio_uri:
                raise ValidationError(f"TTS result for {segment_id} has no artifact URI")
            if (
                isinstance(length_ms, bool)
                or not isinstance(length_ms, (int, float))
                or length_ms <= 0
            ):
                raise ValidationError(f"TTS result for {segment_id} has no audioLengthMs")
            duration = round(float(length_ms) / 1000, 3)
        else:
            duration = round(
                float(segment["sourceEndSeconds"]) - float(segment["sourceStartSeconds"]),
                3,
            )
        start = float(segment["sourceStartSeconds"])
        if start + duration > source_duration + 1e-6:
            raise ValidationError(f"render duration for {segment_id} exceeds source video duration")
        resolved.append(
            RenderSegment(
                id=segment_id,
                source_start_seconds=start,
                duration_seconds=duration,
                narration=str(segment["narration"]),
                ost=ost,
                audio_uri=audio_uri,
            )
        )
    return resolved


def build_process_operations(segment: RenderSegment, aspect: str) -> list[dict[str, Any]]:
    operations: list[dict[str, Any]] = [
        {
            "type": "trim",
            "startSeconds": segment.source_start_seconds,
            "durationSeconds": segment.duration_seconds,
        }
    ]
    if segment.ost == 0:
        operations.append({"type": "mute"})
    if aspect != "source":
        width, height = ASPECT_DIMENSIONS[aspect]
        operations.append({"type": "reframe", "width": width, "height": height, "fit": "cover"})
    return operations


def build_compose_batches(
    segments: list[RenderSegment],
    clip_uris: dict[str, str],
    *,
    narration_volume: float,
    subtitles: bool,
) -> list[dict[str, Any]]:
    batches: list[dict[str, Any]] = []
    current: list[RenderSegment] = []
    current_audio_count = 0

    def flush() -> None:
        nonlocal current, current_audio_count
        if not current:
            return
        offset = 0.0
        video_urls: list[str] = []
        audio_tracks: list[dict[str, Any]] = []
        subtitle_segments: list[dict[str, Any]] = []
        for segment in current:
            try:
                video_urls.append(clip_uris[segment.id])
            except KeyError as error:
                raise ValidationError(f"missing processed clip for {segment.id}") from error
            if segment.audio_uri:
                audio_tracks.append(
                    {
                        "url": segment.audio_uri,
                        "startSeconds": round(offset, 3),
                        "durationSeconds": segment.duration_seconds,
                        "volume": narration_volume,
                    }
                )
                if subtitles and segment.narration:
                    subtitle_segments.append(
                        {
                            "startSeconds": round(offset, 3),
                            "endSeconds": round(offset + segment.duration_seconds, 3),
                            "text": segment.narration,
                        }
                    )
            offset += segment.duration_seconds
        batches.append(
            {
                "videoUrls": video_urls,
                "size": "auto",
                "audioTracks": audio_tracks,
                "preserveOriginalAudio": True,
                "subtitleSegments": subtitle_segments,
            }
        )
        current = []
        current_audio_count = 0

    for segment in segments:
        adds_audio = 1 if segment.audio_uri else 0
        if current and (
            len(current) >= COMPOSE_AUDIO_TRACK_LIMIT
            or current_audio_count + adds_audio > COMPOSE_AUDIO_TRACK_LIMIT
        ):
            flush()
        current.append(segment)
        current_audio_count += adds_audio
    flush()
    return batches
