from __future__ import annotations

import math
import re
from collections.abc import Mapping
from typing import Any
from urllib.parse import urlparse

MAX_VIDEO_BYTES = 512 << 20
MAX_VIDEO_DURATION_SECONDS = 60 * 60
DEFAULT_ANALYSIS_WINDOW_SECONDS = 30.0
VALID_ASPECTS = frozenset({"source", "16:9", "9:16", "1:1"})
MODEL_SLOTS = frozenset({"vision", "asr", "search", "story", "script"})
TTS_PROVIDERS = frozenset({"qwen", "minimax"})


class ValidationError(ValueError):
    """A caller-visible contract error that must not be retried."""


def require_http_uri(value: Any, field: str = "videoUri") -> str:
    uri = str(value or "").strip()
    parsed = urlparse(uri)
    if parsed.scheme in {"http", "https"} and parsed.netloc:
        return uri
    raise ValidationError(f"{field} must be an absolute HTTP(S) URI")


def normalize_generate_input(value: Mapping[str, Any]) -> dict[str, Any]:
    if not isinstance(value, Mapping):
        raise ValidationError("input must be an object")
    _reject_unknown(
        value,
        {"videoUri", "instructions", "models", "analysisOptions", "style"},
        "input",
    )
    return {
        "videoUri": require_http_uri(value.get("videoUri")),
        "instructions": str(value.get("instructions") or "").strip(),
        "models": _normalize_models(value.get("models")),
        "analysisOptions": _normalize_analysis_options(value.get("analysisOptions")),
        "style": _normalize_style(value.get("style")),
    }


def normalize_render_input(value: Mapping[str, Any]) -> dict[str, Any]:
    if not isinstance(value, Mapping):
        raise ValidationError("input must be an object")
    _reject_unknown(
        value,
        {
            "script",
            "tts",
            "aspect",
            "narrationVolume",
            "originalAudioVolume",
            "subtitles",
        },
        "input",
    )
    aspect = str(value.get("aspect") or "source")
    if aspect not in VALID_ASPECTS:
        raise ValidationError(f"aspect must be one of {sorted(VALID_ASPECTS)}")
    volume = _finite_float(value.get("narrationVolume", 1.0), "narrationVolume")
    if not 0 <= volume <= 2:
        raise ValidationError("narrationVolume must be between 0 and 2")
    original_audio_volume = _finite_float(
        value.get("originalAudioVolume", 0.2), "originalAudioVolume"
    )
    if not 0 <= original_audio_volume <= 2:
        raise ValidationError("originalAudioVolume must be between 0 and 2")
    script = validate_documentary_script(value.get("script"))
    return {
        "script": script,
        "tts": _normalize_tts(value.get("tts")),
        "aspect": aspect,
        "narrationVolume": volume,
        "originalAudioVolume": original_audio_volume,
        "subtitles": _optional_bool(value, "subtitles", True),
    }


def normalize_create_input(value: Mapping[str, Any]) -> dict[str, Any]:
    if not isinstance(value, Mapping):
        raise ValidationError("input must be an object")
    _reject_unknown(
        value,
        {
            "videoUri",
            "instructions",
            "models",
            "analysisOptions",
            "style",
            "tts",
            "aspect",
            "narrationVolume",
            "originalAudioVolume",
            "subtitles",
        },
        "input",
    )
    generated = normalize_generate_input(
        {
            key: value[key]
            for key in ("videoUri", "instructions", "models", "analysisOptions", "style")
            if key in value
        }
    )
    aspect = str(value.get("aspect") or "source")
    if aspect not in VALID_ASPECTS:
        raise ValidationError(f"aspect must be one of {sorted(VALID_ASPECTS)}")
    volume = _finite_float(value.get("narrationVolume", 1.0), "narrationVolume")
    if not 0 <= volume <= 2:
        raise ValidationError("narrationVolume must be between 0 and 2")
    # 原声音量未显式给出时跟随风格：影视解说要把原声压到只剩氛围。
    from .styles import resolve_style

    style_default = resolve_style(generated["style"]).default_original_audio_volume
    original_audio_volume = _finite_float(
        value.get("originalAudioVolume", style_default), "originalAudioVolume"
    )
    if not 0 <= original_audio_volume <= 2:
        raise ValidationError("originalAudioVolume must be between 0 and 2")
    return {
        **generated,
        "tts": _normalize_tts(value.get("tts")),
        "aspect": aspect,
        "narrationVolume": volume,
        "originalAudioVolume": original_audio_volume,
        "subtitles": _optional_bool(value, "subtitles", True),
    }


def validate_documentary_script(
    value: Any,
    *,
    automatic: bool = False,
    source_uri: str | None = None,
    source_duration: float | None = None,
    narrative_profile: Mapping[str, Any] | None = None,
    window_count: int | None = None,
) -> dict[str, Any]:
    if not isinstance(value, Mapping):
        raise ValidationError("script must be an object")
    if value.get("version") != "2":
        raise ValidationError("script.version must be '2'")
    _reject_unknown(
        value,
        {
            "version",
            "sourceVideoUri",
            "sourceDurationSeconds",
            "narrativeProfile",
            "narrationCues",
            "pacingWarning",
        },
        "script",
    )

    uri = require_http_uri(source_uri or value.get("sourceVideoUri"), "script.sourceVideoUri")
    duration = _finite_float(
        source_duration if source_duration is not None else value.get("sourceDurationSeconds"),
        "script.sourceDurationSeconds",
    )
    duration = _round_seconds(duration)
    if duration <= 0:
        raise ValidationError("script.sourceDurationSeconds must be greater than 0")

    profile = _normalize_narrative_profile(
        narrative_profile if narrative_profile is not None else value.get("narrativeProfile")
    )

    raw_cues = value.get("narrationCues")
    if not isinstance(raw_cues, list) or not raw_cues:
        raise ValidationError("script.narrationCues must contain at least one cue")
    if len(raw_cues) > 500:
        raise ValidationError("script.narrationCues must contain at most 500 cues")

    normalized: list[dict[str, Any]] = []
    seen: set[str] = set()
    previous_end = 0.0
    for index, raw in enumerate(raw_cues, start=1):
        path = f"script.narrationCues[{index - 1}]"
        if not isinstance(raw, Mapping):
            raise ValidationError(f"{path} must be an object")
        _reject_unknown(
            raw,
            {
                "id",
                "startSeconds",
                "endSeconds",
                "evidenceEndSeconds",
                "narration",
                "evidenceWindowIndexes",
                "ttsInstruction",
                "importance",
                "emotion",
            },
            path,
        )
        cue_id = f"cue-{index:03d}" if automatic else str(raw.get("id") or "").strip()
        if not re.fullmatch(r"cue-[0-9]{3,}", cue_id):
            raise ValidationError(f"{path}.id must match cue-NNN")
        if cue_id != f"cue-{index:03d}":
            raise ValidationError(f"{path}.id must be cue-{index:03d}")
        if not cue_id:
            raise ValidationError(f"{path}.id is required")
        if cue_id in seen:
            raise ValidationError(f"{path}.id is duplicated: {cue_id}")
        seen.add(cue_id)

        start = _finite_float(raw.get("startSeconds"), f"{path}.startSeconds")
        end = _finite_float(raw.get("endSeconds"), f"{path}.endSeconds")
        if start < 0 or end <= start:
            raise ValidationError(f"{path} must have 0 <= startSeconds < endSeconds")
        if end > duration + 1e-6:
            raise ValidationError(f"{path}.endSeconds exceeds source duration")
        if start + 1e-6 < previous_end:
            raise ValidationError("script.narrationCues must be ordered and non-overlapping")
        previous_end = end

        raw_evidence_end = raw.get("evidenceEndSeconds")
        evidence_end: float | None = None
        if raw_evidence_end is not None:
            evidence_end = _finite_float(raw_evidence_end, f"{path}.evidenceEndSeconds")

        raw_narration = raw.get("narration")
        if not isinstance(raw_narration, str):
            raise ValidationError(f"{path}.narration must be a string")
        narration = raw_narration.strip()
        if not narration:
            raise ValidationError(f"{path}.narration is required")
        evidence = raw.get("evidenceWindowIndexes")
        if not isinstance(evidence, list) or not evidence:
            raise ValidationError(f"{path}.evidenceWindowIndexes must not be empty")
        if any(type(item) is not int or item < 0 for item in evidence):
            raise ValidationError(f"{path}.evidenceWindowIndexes must contain integers")
        if window_count is not None and any(item >= window_count for item in evidence):
            raise ValidationError(f"{path}.evidenceWindowIndexes contains an unknown window")
        if evidence != sorted(set(evidence)):
            raise ValidationError(f"{path}.evidenceWindowIndexes must be unique and ordered")
        tts_instruction = _required_text(raw.get("ttsInstruction"), f"{path}.ttsInstruction")
        importance = raw.get("importance")
        if type(importance) is not int or not 1 <= importance <= 5:
            raise ValidationError(f"{path}.importance must be an integer from 1 to 5")
        normalized.append(
            {
                "id": cue_id,
                "startSeconds": _round_seconds(start),
                "endSeconds": _round_seconds(end),
                **(
                    {"evidenceEndSeconds": _round_seconds(evidence_end)}
                    if evidence_end is not None
                    else {}
                ),
                "narration": narration,
                "evidenceWindowIndexes": list(evidence),
                "ttsInstruction": tts_instruction,
                "importance": importance,
                **(
                    {"emotion": _normalize_emotion(raw.get("emotion"), path)}
                    if raw.get("emotion") not in (None, "")
                    else {}
                ),
            }
        )

    result = {
        "version": "2",
        "sourceVideoUri": uri,
        "sourceDurationSeconds": _round_seconds(duration),
        "narrativeProfile": profile,
        "narrationCues": normalized,
    }
    pacing_warning = str(value.get("pacingWarning") or "").strip()
    if pacing_warning:
        result["pacingWarning"] = pacing_warning
    return result


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


def _normalize_style(value: Any) -> str:
    # styles 依赖本模块的 ValidationError，这里延迟导入以避免循环。
    from .styles import resolve_style

    return resolve_style(value).id


def _normalize_models(
    value: Any,
    *,
    allowed: set[str] | frozenset[str] = MODEL_SLOTS,
) -> dict[str, str]:
    if value is None:
        return {}
    if not isinstance(value, Mapping):
        raise ValidationError("models must be an object")
    unknown = sorted(set(value) - set(allowed))
    if unknown:
        raise ValidationError(f"models.{unknown[0]} is not supported")
    normalized: dict[str, str] = {}
    for name, raw in value.items():
        model = str(raw or "").strip()
        if not model:
            raise ValidationError(f"models.{name} must be a non-empty string")
        normalized[name] = model
    return normalized


def _normalize_tts(value: Any) -> dict[str, str]:
    if not isinstance(value, Mapping):
        raise ValidationError("tts must be an object")
    _reject_unknown(value, {"provider", "model", "voiceId"}, "tts")
    provider = str(value.get("provider") or "").strip().lower()
    if provider not in TTS_PROVIDERS:
        raise ValidationError(f"tts.provider must be one of {sorted(TTS_PROVIDERS)}")
    return {
        "provider": provider,
        "model": _required_text(value.get("model"), "tts.model"),
        "voiceId": _required_text(value.get("voiceId"), "tts.voiceId"),
    }


def _normalize_emotion(value: Any, path: str) -> str:
    allowed = {
        "happy",
        "sad",
        "angry",
        "fearful",
        "disgusted",
        "surprised",
        "calm",
        "fluent",
        "whisper",
    }
    emotion = str(value or "").strip().lower()
    if emotion not in allowed:
        raise ValidationError(f"{path}.emotion must be one of {sorted(allowed)}")
    return emotion


def _normalize_narrative_profile(value: Any) -> dict[str, Any]:
    if not isinstance(value, Mapping):
        raise ValidationError("script.narrativeProfile must be an object")
    allowed = {
        "primaryGenre",
        "secondaryGenres",
        "thesis",
        "hook",
        "style",
        "pacing",
        "globalTtsInstruction",
    }
    _reject_unknown(value, allowed, "script.narrativeProfile")
    raw_secondary = value.get("secondaryGenres")
    if not isinstance(raw_secondary, list) or len(raw_secondary) > 3:
        raise ValidationError("script.narrativeProfile.secondaryGenres must be an array")
    secondary = [
        _required_text(item, f"script.narrativeProfile.secondaryGenres[{index}]")
        for index, item in enumerate(raw_secondary)
    ]
    return {
        "primaryGenre": _required_text(
            value.get("primaryGenre"), "script.narrativeProfile.primaryGenre"
        ),
        "secondaryGenres": secondary,
        "thesis": _required_text(value.get("thesis"), "script.narrativeProfile.thesis"),
        "hook": _required_text(value.get("hook"), "script.narrativeProfile.hook"),
        "style": _required_text(value.get("style"), "script.narrativeProfile.style"),
        "pacing": _required_text(value.get("pacing"), "script.narrativeProfile.pacing"),
        "globalTtsInstruction": _required_text(
            value.get("globalTtsInstruction"),
            "script.narrativeProfile.globalTtsInstruction",
        ),
    }


def _required_text(value: Any, field: str) -> str:
    if not isinstance(value, str) or not value.strip():
        raise ValidationError(f"{field} is required and must be a string")
    return value.strip()


def _reject_unknown(value: Mapping[str, Any], allowed: set[str], field: str) -> None:
    unknown = sorted(set(value) - allowed)
    if unknown:
        raise ValidationError(f"{field}.{unknown[0]} is not supported")


def _normalize_analysis_options(value: Any) -> dict[str, Any]:
    defaults: dict[str, Any] = {
        "asrEnabled": True,
        # Empty means that the ASR provider should detect the spoken language.
        # Forcing the narration language here corrupts recognition for foreign-
        # language source videos (for example English dialogue with Chinese UI).
        "asrLanguage": "",
    }
    if value is None:
        return defaults
    if not isinstance(value, Mapping):
        raise ValidationError("analysisOptions must be an object")
    unknown = sorted(set(value) - set(defaults))
    if unknown:
        raise ValidationError(f"analysisOptions.{unknown[0]} is not supported")
    result = dict(defaults)
    for field in ("asrEnabled",):
        if field in value:
            if type(value[field]) is not bool:
                raise ValidationError(f"analysisOptions.{field} must be a boolean")
            result[field] = value[field]
    if "asrLanguage" in value:
        result["asrLanguage"] = str(value["asrLanguage"] or "").strip()
    return result


def _round_seconds(value: float) -> float:
    return round(value, 3)
