from __future__ import annotations

import math
import re
from collections.abc import Mapping
from typing import Any

from .models import ValidationError

ASR_TIMESTAMP_TOLERANCE_SECONDS = 0.5
MAX_RESEARCH_QUERIES = 5


def validate_narrative_plan(
    value: Any,
    *,
    duration_seconds: float,
    window_count: int,
) -> dict[str, Any]:
    if not isinstance(value, Mapping):
        raise ValidationError("narrative plan must be an object")
    duration = round(_finite(duration_seconds, "narrative plan durationSeconds"), 3)
    if duration <= 0 or window_count <= 0:
        raise ValidationError("narrative plan requires a positive timeline and evidence windows")

    raw_secondary = value.get("secondaryGenres")
    if not isinstance(raw_secondary, list) or len(raw_secondary) > 3:
        raise ValidationError("narrative plan secondaryGenres must contain at most 3 items")
    secondary = [
        _required_text(item, f"secondaryGenres[{index}]")
        for index, item in enumerate(raw_secondary)
    ]
    profile = {
        "primaryGenre": _required_text(value.get("primaryGenre"), "primaryGenre"),
        "secondaryGenres": secondary,
        "thesis": _required_text(value.get("thesis"), "thesis"),
        "hook": _required_text(value.get("hook"), "hook"),
        "style": _required_text(value.get("style"), "style"),
        "pacing": _required_text(value.get("pacing"), "pacing"),
        "globalTtsInstruction": _required_text(
            value.get("globalTtsInstruction"), "globalTtsInstruction"
        ),
    }

    research_needed = value.get("researchNeeded")
    if type(research_needed) is not bool:
        raise ValidationError("narrative plan researchNeeded must be a boolean")
    raw_queries = value.get("researchQueries")
    if not isinstance(raw_queries, list):
        raise ValidationError("narrative plan researchQueries must be an array")
    queries: list[str] = []
    for index, raw in enumerate(raw_queries):
        query = _required_text(raw, f"researchQueries[{index}]")
        if len(query) > 100:
            raise ValidationError("narrative plan research query must not exceed 100 characters")
        if query not in queries:
            queries.append(query)
    if len(queries) > MAX_RESEARCH_QUERIES:
        raise ValidationError(f"narrative plan supports at most {MAX_RESEARCH_QUERIES} queries")
    if research_needed and not queries:
        raise ValidationError("narrative plan needs queries when researchNeeded is true")
    if not research_needed and queries:
        raise ValidationError("narrative plan must omit queries when researchNeeded is false")

    raw_beats = value.get("storyBeats")
    if not isinstance(raw_beats, list) or not raw_beats:
        raise ValidationError("narrative plan storyBeats must contain at least one beat")
    if len(raw_beats) > 500:
        raise ValidationError("narrative plan supports at most 500 story beats")
    beats: list[dict[str, Any]] = []
    previous_start = -1.0
    for index, raw in enumerate(raw_beats, start=1):
        path = f"storyBeats[{index - 1}]"
        if not isinstance(raw, Mapping):
            raise ValidationError(f"{path} must be an object")
        beat_id = str(raw.get("id") or "").strip()
        expected_id = f"beat-{index:03d}"
        if beat_id != expected_id:
            raise ValidationError(f"{path}.id must be {expected_id}")
        start = _finite(raw.get("startSeconds"), f"{path}.startSeconds")
        end = _finite(raw.get("endSeconds"), f"{path}.endSeconds")
        if start < 0 or end <= start or end > duration + 1e-6:
            raise ValidationError(f"{path} must be inside the source timeline")
        if start < previous_start:
            raise ValidationError("narrative plan storyBeats must be ordered")
        previous_start = start
        evidence = raw.get("evidenceWindowIndexes")
        if not isinstance(evidence, list) or not evidence:
            raise ValidationError(f"{path}.evidenceWindowIndexes must not be empty")
        if any(type(item) is not int or not 0 <= item < window_count for item in evidence):
            raise ValidationError(f"{path}.evidenceWindowIndexes contains an unknown window")
        if evidence != sorted(set(evidence)):
            raise ValidationError(f"{path}.evidenceWindowIndexes must be unique and ordered")
        importance = raw.get("importance")
        if type(importance) is not int or not 1 <= importance <= 5:
            raise ValidationError(f"{path}.importance must be an integer from 1 to 5")
        beats.append(
            {
                "id": beat_id,
                "startSeconds": round(start, 3),
                "endSeconds": round(end, 3),
                "summary": _required_text(raw.get("summary"), f"{path}.summary"),
                "narrationPurpose": _required_text(
                    raw.get("narrationPurpose"), f"{path}.narrationPurpose"
                ),
                "importance": importance,
                "evidenceWindowIndexes": list(evidence),
            }
        )
    return {
        **profile,
        "researchNeeded": research_needed,
        "researchReason": str(value.get("researchReason") or "").strip(),
        "researchQueries": queries,
        "storyBeats": beats,
    }


def narrative_profile_from_plan(plan: Mapping[str, Any]) -> dict[str, Any]:
    return {
        key: plan[key]
        for key in (
            "primaryGenre",
            "secondaryGenres",
            "thesis",
            "hook",
            "style",
            "pacing",
            "globalTtsInstruction",
        )
    }


def validate_research_plan(value: Any) -> dict[str, Any]:
    if not isinstance(value, Mapping):
        raise ValidationError("research plan must be an object")
    needed = value.get("needed")
    if type(needed) is not bool:
        raise ValidationError("research plan needed must be a boolean")
    reason = str(value.get("reason") or "").strip()
    raw_queries = value.get("queries")
    if not isinstance(raw_queries, list):
        raise ValidationError("research plan queries must be an array")
    queries: list[str] = []
    seen: set[str] = set()
    for raw in raw_queries:
        query = str(raw or "").strip()
        if not query or len(query) > 100:
            raise ValidationError("research query must contain 1 to 100 characters")
        if query not in seen:
            seen.add(query)
            queries.append(query)
    if len(queries) > MAX_RESEARCH_QUERIES:
        raise ValidationError(f"research plan supports at most {MAX_RESEARCH_QUERIES} queries")
    if needed and not queries:
        raise ValidationError("research plan must include a query when research is needed")
    if not needed and queries:
        raise ValidationError("research plan must not include queries when research is not needed")
    return {"needed": needed, "reason": reason, "queries": queries}


def normalize_asr_output(
    value: Any,
    *,
    window_index: int,
    start_seconds: float,
    end_seconds: float,
    audio_duration_seconds: float | None = None,
) -> dict[str, Any]:
    if not isinstance(value, Mapping):
        raise ValidationError("transcribe_audio returned a non-object result")
    window_duration = end_seconds - start_seconds
    if window_duration <= 0:
        raise ValidationError("ASR window must have positive duration")
    measured_duration = window_duration
    if audio_duration_seconds is not None:
        measured_duration = _finite(audio_duration_seconds, "ASR audio durationSeconds")
        if measured_duration <= 0 or measured_duration > window_duration + 1.0:
            raise ValidationError("ASR audio durationSeconds is outside its source window")
    effective_duration = min(window_duration, measured_duration)
    raw_segments = value.get("segments") or []
    if not isinstance(raw_segments, list):
        raise ValidationError("transcribe_audio segments must be an array")
    segments: list[dict[str, Any]] = []
    previous_end = 0.0
    for index, raw in enumerate(raw_segments):
        if not isinstance(raw, Mapping):
            raise ValidationError(f"transcribe_audio segments[{index}] must be an object")
        raw_text = raw.get("text")
        # Azure Whisper can append a zero-length sentinel segment with no text.
        # It carries no evidence and must not invalidate the useful segments.
        if raw_text is None or (isinstance(raw_text, str) and not raw_text.strip()):
            continue
        local_start = _finite(raw.get("start"), f"segments[{index}].start")
        local_end = _finite(raw.get("end"), f"segments[{index}].end")
        text = _required_text(raw_text, f"transcribe_audio segments[{index}].text")
        if (
            local_start < -ASR_TIMESTAMP_TOLERANCE_SECONDS
            or local_start > effective_duration + ASR_TIMESTAMP_TOLERANCE_SECONDS
            or local_end <= local_start
        ):
            raise ValidationError(f"transcribe_audio segments[{index}] is outside its audio window")
        # Encoders and Whisper can report a final timestamp in the short padding
        # after the source window. A segment wholly inside that padding has no
        # source-timeline evidence, so omit it instead of failing the workflow.
        if local_start >= effective_duration:
            continue
        normalized_start = max(0.0, local_start)
        normalized_end = min(effective_duration, local_end)
        if normalized_end <= normalized_start:
            raise ValidationError(f"transcribe_audio segments[{index}] is outside its audio window")
        if normalized_start + 1e-6 < previous_end:
            raise ValidationError("transcribe_audio segments must be ordered and non-overlapping")
        previous_end = normalized_end
        segments.append(
            {
                "id": f"asr-{window_index:03d}-{index:03d}",
                "startSeconds": round(start_seconds + normalized_start, 3),
                "endSeconds": round(start_seconds + normalized_end, 3),
                "text": text,
            }
        )
    raw_full_text = value.get("text")
    if raw_full_text is not None and not isinstance(raw_full_text, str):
        raise ValidationError("transcribe_audio text must be a string")
    full_text = (raw_full_text or "").strip()
    if full_text and not segments:
        raise ValidationError("transcribe_audio returned text without timestamped segments")
    return {
        "windowIndex": window_index,
        "startSeconds": round(start_seconds, 3),
        "endSeconds": round(end_seconds, 3),
        "language": str(value.get("language") or "").strip(),
        "text": full_text,
        "segments": segments,
    }


def validate_corrected_transcript(value: Any, source: list[dict[str, Any]]) -> list[dict[str, Any]]:
    if not isinstance(value, Mapping) or not isinstance(value.get("segments"), list):
        raise ValidationError("corrected transcript must contain a segments array")
    corrected = value["segments"]
    if len(corrected) != len(source):
        raise ValidationError("corrected transcript must preserve every ASR segment")
    output: list[dict[str, Any]] = []
    for index, (original, candidate) in enumerate(zip(source, corrected, strict=True)):
        if not isinstance(candidate, Mapping):
            raise ValidationError(f"corrected transcript segments[{index}] must be an object")
        if str(candidate.get("id") or "") != original["id"]:
            raise ValidationError("corrected transcript must preserve ASR segment IDs")
        start = _finite(candidate.get("startSeconds"), "corrected startSeconds")
        end = _finite(candidate.get("endSeconds"), "corrected endSeconds")
        if (
            abs(start - float(original["startSeconds"])) > 0.001
            or abs(end - float(original["endSeconds"])) > 0.001
        ):
            raise ValidationError("corrected transcript must preserve ASR timestamps")
        text = _required_text(candidate.get("text"), "corrected transcript text")
        output.append({**original, "text": text})
    return output


def validate_story_analysis(value: Any, *, duration_seconds: float) -> dict[str, Any]:
    """Validate the semantic parts that JSON Schema alone cannot constrain."""

    if not isinstance(value, Mapping):
        raise ValidationError("story analysis must be an object")
    duration = round(_finite(duration_seconds, "story durationSeconds"), 3)
    if duration <= 0:
        raise ValidationError("story durationSeconds must be greater than zero")

    summary = _required_text(value.get("summary"), "story analysis summary")
    characters = _string_object_list(value.get("characters"), "characters")
    facts = _string_list(value.get("facts"), "facts")
    uncertainties = _string_list(value.get("uncertainties"), "uncertainties")

    raw_scenes = value.get("scenes")
    if not isinstance(raw_scenes, list) or not raw_scenes:
        raise ValidationError("story analysis scenes must contain at least one scene")
    if len(raw_scenes) > 500:
        raise ValidationError("story analysis scenes must contain at most 500 scenes")
    scenes: list[dict[str, Any]] = []
    previous_start = -1.0
    for index, raw in enumerate(raw_scenes):
        path = f"scenes[{index}]"
        if not isinstance(raw, Mapping):
            raise ValidationError(f"{path} must be an object")
        start = _finite(raw.get("startSeconds"), f"{path}.startSeconds")
        end = _finite(raw.get("endSeconds"), f"{path}.endSeconds")
        if start < 0 or end <= start or end > duration + 1e-6:
            raise ValidationError(f"{path} must be inside the source timeline")
        if start < previous_start:
            raise ValidationError("story analysis scenes must be ordered")
        previous_start = start
        scene_summary = _required_text(raw.get("summary"), f"{path}.summary")
        scenes.append(
            {
                "startSeconds": round(start, 3),
                "endSeconds": round(end, 3),
                "summary": scene_summary,
                "visualFacts": _string_list(raw.get("visualFacts"), f"{path}.visualFacts"),
                "dialogueFacts": _string_list(raw.get("dialogueFacts"), f"{path}.dialogueFacts"),
                "uncertainties": _string_list(raw.get("uncertainties"), f"{path}.uncertainties"),
            }
        )
    return {
        "summary": summary,
        "characters": characters,
        "scenes": scenes,
        "facts": facts,
        "uncertainties": uncertainties,
    }


def validate_narration_draft(value: Any, *, window_count: int) -> dict[str, Any]:
    if not isinstance(value, Mapping):
        raise ValidationError("narration draft must be an object")
    raw_segments = value.get("segments")
    if not isinstance(raw_segments, list) or not raw_segments:
        raise ValidationError("narration draft segments must contain at least one segment")
    if len(raw_segments) > 500:
        raise ValidationError("narration draft segments must contain at most 500 segments")
    if window_count <= 0:
        raise ValidationError("narration draft requires at least one evidence window")

    segments: list[dict[str, Any]] = []
    for index, raw in enumerate(raw_segments, start=1):
        path = f"draft.segments[{index - 1}]"
        if not isinstance(raw, Mapping):
            raise ValidationError(f"{path} must be an object")
        segment_id = str(raw.get("id") or "").strip()
        if not re.fullmatch(r"draft-[0-9]{3,}", segment_id):
            raise ValidationError(f"{path}.id must match draft-NNN")
        expected_id = f"draft-{index:03d}"
        if segment_id != expected_id:
            raise ValidationError(f"{path}.id must be {expected_id}")
        narration = _required_text(raw.get("narration"), f"{path}.narration")
        evidence = raw.get("evidenceWindowIndexes")
        if not isinstance(evidence, list) or not evidence:
            raise ValidationError(f"{path}.evidenceWindowIndexes must not be empty")
        if any(type(item) is not int or not 0 <= item < window_count for item in evidence):
            raise ValidationError(f"{path}.evidenceWindowIndexes contains an unknown window")
        if evidence != sorted(set(evidence)):
            raise ValidationError(f"{path}.evidenceWindowIndexes must be unique and ordered")
        segments.append(
            {
                "id": segment_id,
                "narration": narration,
                "evidenceWindowIndexes": evidence,
            }
        )
    return {"segments": segments}


def _string_list(value: Any, field: str) -> list[str]:
    if not isinstance(value, list):
        raise ValidationError(f"{field} must be an array")
    output: list[str] = []
    for index, item in enumerate(value):
        if not isinstance(item, str):
            raise ValidationError(f"{field}[{index}] must be a string")
        output.append(item.strip())
    return output


def _string_object_list(value: Any, field: str) -> list[dict[str, Any]]:
    if not isinstance(value, list):
        raise ValidationError(f"{field} must be an array")
    output: list[dict[str, Any]] = []
    for index, item in enumerate(value):
        path = f"{field}[{index}]"
        if not isinstance(item, Mapping):
            raise ValidationError(f"{path} must be an object")
        name = _required_text(item.get("name"), f"{path}.name")
        if "role" not in item or not isinstance(item["role"], str):
            raise ValidationError(f"{path}.role must be a string")
        output.append(
            {
                "name": name,
                "role": item["role"].strip(),
                "evidence": _string_list(item.get("evidence"), f"{path}.evidence"),
            }
        )
    return output


def _required_text(value: Any, field: str) -> str:
    if not isinstance(value, str) or not value.strip():
        raise ValidationError(f"{field} is required and must be a string")
    return value.strip()


def _finite(value: Any, field: str) -> float:
    if isinstance(value, bool):
        raise ValidationError(f"{field} must be a finite number")
    try:
        parsed = float(value)
    except (TypeError, ValueError) as error:
        raise ValidationError(f"{field} must be a finite number") from error
    if not math.isfinite(parsed):
        raise ValidationError(f"{field} must be a finite number")
    return parsed
