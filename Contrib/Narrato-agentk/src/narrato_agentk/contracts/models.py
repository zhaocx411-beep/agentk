from __future__ import annotations

import math
from collections.abc import Mapping
from typing import Any
from urllib.parse import urlparse

MAX_VIDEO_BYTES = 512 << 20
DEFAULT_ANALYSIS_WINDOW_SECONDS = 30.0
VALID_ASPECTS = frozenset({"source", "16:9", "9:16", "1:1"})
AUTOMATIC_NARRATION_CHARS_PER_SECOND = 4.0


class ValidationError(ValueError):
    """A caller-visible contract error that must not be retried."""


def require_http_uri(value: Any, field: str = "videoUri") -> str:
    uri = str(value or "").strip()
    parsed = urlparse(uri)
    if parsed.scheme not in {"http", "https"} or not parsed.netloc:
        raise ValidationError(f"{field} must be an absolute HTTP or HTTPS URI")
    return uri


def normalize_generate_input(value: Mapping[str, Any]) -> dict[str, Any]:
    if not isinstance(value, Mapping):
        raise ValidationError("input must be an object")
    return {
        "videoUri": require_http_uri(value.get("videoUri")),
        "theme": str(value.get("theme") or "").strip(),
        "instructions": str(value.get("instructions") or "").strip(),
    }


def normalize_render_input(value: Mapping[str, Any]) -> dict[str, Any]:
    if not isinstance(value, Mapping):
        raise ValidationError("input must be an object")
    voice_id = str(value.get("voiceId") or "").strip()
    if not voice_id:
        raise ValidationError("voiceId is required")
    aspect = str(value.get("aspect") or "source")
    if aspect not in VALID_ASPECTS:
        raise ValidationError(f"aspect must be one of {sorted(VALID_ASPECTS)}")
    volume = _finite_float(value.get("narrationVolume", 1.0), "narrationVolume")
    if not 0 <= volume <= 2:
        raise ValidationError("narrationVolume must be between 0 and 2")
    script = validate_documentary_script(value.get("script"))
    return {
        "script": script,
        "voiceId": voice_id,
        "aspect": aspect,
        "narrationVolume": volume,
        "subtitles": _optional_bool(value, "subtitles", True),
    }


def normalize_create_input(value: Mapping[str, Any]) -> dict[str, Any]:
    generated = normalize_generate_input(value)
    voice_id = str(value.get("voiceId") or "").strip()
    if not voice_id:
        raise ValidationError("voiceId is required")
    aspect = str(value.get("aspect") or "source")
    if aspect not in VALID_ASPECTS:
        raise ValidationError(f"aspect must be one of {sorted(VALID_ASPECTS)}")
    volume = _finite_float(value.get("narrationVolume", 1.0), "narrationVolume")
    if not 0 <= volume <= 2:
        raise ValidationError("narrationVolume must be between 0 and 2")
    return {
        **generated,
        "voiceId": voice_id,
        "aspect": aspect,
        "narrationVolume": volume,
        "subtitles": _optional_bool(value, "subtitles", True),
    }


def validate_documentary_script(
    value: Any,
    *,
    automatic: bool = False,
    source_uri: str | None = None,
    source_duration: float | None = None,
    theme: str | None = None,
) -> dict[str, Any]:
    if not isinstance(value, Mapping):
        raise ValidationError("script must be an object")
    if value.get("version") != "1":
        raise ValidationError("script.version must be '1'")

    uri = require_http_uri(source_uri or value.get("sourceVideoUri"), "script.sourceVideoUri")
    duration = _finite_float(
        source_duration if source_duration is not None else value.get("sourceDurationSeconds"),
        "script.sourceDurationSeconds",
    )
    if duration <= 0:
        raise ValidationError("script.sourceDurationSeconds must be greater than 0")

    raw_segments = value.get("segments")
    if not isinstance(raw_segments, list) or not raw_segments:
        raise ValidationError("script.segments must contain at least one segment")
    if len(raw_segments) > 500:
        raise ValidationError("script.segments must contain at most 500 segments")

    normalized: list[dict[str, Any]] = []
    seen: set[str] = set()
    previous_start = -1.0
    for index, raw in enumerate(raw_segments, start=1):
        path = f"script.segments[{index - 1}]"
        if not isinstance(raw, Mapping):
            raise ValidationError(f"{path} must be an object")
        segment_id = f"segment-{index:03d}" if automatic else str(raw.get("id") or "").strip()
        if not segment_id:
            raise ValidationError(f"{path}.id is required")
        if segment_id in seen:
            raise ValidationError(f"{path}.id is duplicated: {segment_id}")
        seen.add(segment_id)

        start = _finite_float(raw.get("sourceStartSeconds"), f"{path}.sourceStartSeconds")
        end = _finite_float(raw.get("sourceEndSeconds"), f"{path}.sourceEndSeconds")
        if start < 0 or end <= start:
            raise ValidationError(f"{path} must have 0 <= sourceStartSeconds < sourceEndSeconds")
        if end > duration + 1e-6:
            raise ValidationError(f"{path}.sourceEndSeconds exceeds source duration")
        if start < previous_start:
            raise ValidationError("script.segments must be ordered by sourceStartSeconds")
        previous_start = start

        narration = str(raw.get("narration") or "").strip()
        ost = 2 if automatic else raw.get("ost")
        if type(ost) is not int or ost not in (0, 1, 2):
            raise ValidationError(f"{path}.ost must be 0, 1, or 2")
        if not narration:
            raise ValidationError(f"{path}.narration is required")
        if automatic:
            estimated_duration = max(
                1.0,
                len("".join(narration.split())) / AUTOMATIC_NARRATION_CHARS_PER_SECOND,
            )
            if start + estimated_duration > duration + 1e-6:
                raise ValidationError(
                    f"{path} does not leave enough source duration for narration; "
                    f"move sourceStartSeconds earlier than "
                    f"{max(0.0, duration - estimated_duration):.3f}"
                )

        normalized.append(
            {
                "id": segment_id,
                "sourceStartSeconds": _round_seconds(start),
                "sourceEndSeconds": _round_seconds(end),
                "narration": narration,
                "ost": int(ost),
            }
        )

    return {
        "version": "1",
        "sourceVideoUri": uri,
        "sourceDurationSeconds": _round_seconds(duration),
        "theme": str(theme if theme is not None else value.get("theme") or "").strip(),
        "segments": normalized,
    }


def build_analysis_windows(
    duration_seconds: float,
    window_seconds: float = DEFAULT_ANALYSIS_WINDOW_SECONDS,
) -> list[dict[str, float | int]]:
    duration = _finite_float(duration_seconds, "durationSeconds")
    window = _finite_float(window_seconds, "windowSeconds")
    if duration <= 0 or window <= 0:
        raise ValidationError("durationSeconds and windowSeconds must be greater than 0")
    count = math.ceil(duration / window)
    return [
        {
            "index": index,
            "startSeconds": _round_seconds(index * window),
            "endSeconds": _round_seconds(min(duration, (index + 1) * window)),
        }
        for index in range(count)
    ]


def _finite_float(value: Any, field: str) -> float:
    if isinstance(value, bool):
        raise ValidationError(f"{field} must be a finite number")
    try:
        parsed = float(value)
    except (TypeError, ValueError) as error:
        raise ValidationError(f"{field} must be a finite number") from error
    if not math.isfinite(parsed):
        raise ValidationError(f"{field} must be a finite number")
    return parsed


def _optional_bool(value: Mapping[str, Any], field: str, default: bool) -> bool:
    if field not in value:
        return default
    parsed = value[field]
    if type(parsed) is not bool:
        raise ValidationError(f"{field} must be a boolean")
    return parsed


def _round_seconds(value: float) -> float:
    return round(value, 3)
