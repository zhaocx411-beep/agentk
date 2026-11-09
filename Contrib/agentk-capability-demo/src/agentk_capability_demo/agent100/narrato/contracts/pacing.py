from __future__ import annotations

from collections.abc import Iterable, Mapping
from typing import Any

PACING_OPENING_HOOK_MAX_START_SECONDS = 3.0
PACING_RECOMMENDED_MAX_GAP_SECONDS = 5.0
PACING_RETRY_THRESHOLD_SECONDS = PACING_RECOMMENDED_MAX_GAP_SECONDS
PACING_LEGACY_RETRY_THRESHOLD_SECONDS = 18.0


def post_event_timing_issues(
    script: Mapping[str, Any],
    evidence_windows: Iterable[Mapping[str, Any]],
) -> list[dict[str, Any]]:
    """Describe post-event timing concerns without rejecting the script."""

    windows = {int(item["index"]): item for item in evidence_windows}
    issues: list[dict[str, Any]] = []
    for cue in script.get("narrationCues") or []:
        cue_id = str(cue.get("id") or "")
        evidence_end = cue.get("evidenceEndSeconds")
        if evidence_end is None:
            issues.append(
                {
                    "type": "missingEvidenceEnd",
                    "cueId": cue_id,
                    "message": "缺少 evidenceEndSeconds，无法确认解说是否晚于事件。",
                }
            )
            continue
        evidence_end = float(evidence_end)
        start = float(cue["startSeconds"])
        duration = float(script["sourceDurationSeconds"])
        if evidence_end < 0 or evidence_end > duration + 1e-6:
            issues.append(
                {
                    "type": "evidenceTimeOutsideSource",
                    "cueId": cue_id,
                    "evidenceEndSeconds": evidence_end,
                    "sourceDurationSeconds": duration,
                    "message": "evidenceEndSeconds 不在原片时间轴内。",
                }
            )
        if start + 1e-6 < evidence_end:
            issues.append(
                {
                    "type": "startsBeforeEvidence",
                    "cueId": cue_id,
                    "startSeconds": start,
                    "evidenceEndSeconds": evidence_end,
                    "message": "startSeconds 早于 evidenceEndSeconds，可能提前讲述剧情。",
                }
            )
        referenced = [
            windows[index]
            for index in cue.get("evidenceWindowIndexes") or []
            if index in windows
        ]
        if not referenced:
            continue
        latest_start = max(float(item["startSeconds"]) for item in referenced)
        latest_end = max(float(item["endSeconds"]) for item in referenced)
        if evidence_end + 1e-6 < latest_start or evidence_end > latest_end + 1e-6:
            issues.append(
                {
                    "type": "evidenceTimeOutsideWindows",
                    "cueId": cue_id,
                    "evidenceEndSeconds": evidence_end,
                    "referencedRange": [latest_start, latest_end],
                    "message": "evidenceEndSeconds 与引用的证据窗口不一致。",
                }
            )
    return issues


def opening_hook_gap(script: Mapping[str, Any]) -> dict[str, float] | None:
    """Return the opening interval when the first narration starts too late."""

    cues = script.get("narrationCues") or []
    if not cues:
        return None
    start = float(cues[0]["startSeconds"])
    if start <= PACING_OPENING_HOOK_MAX_START_SECONDS:
        return None
    return {
        "startSeconds": 0.0,
        "endSeconds": round(start, 3),
        "durationSeconds": round(start, 3),
    }


def excessive_narration_gaps(
    script: Mapping[str, Any],
    transcript_segments: Iterable[Mapping[str, Any]],
    *,
    threshold_seconds: float = PACING_RETRY_THRESHOLD_SECONDS,
    dialogue_counts_as_coverage: bool = True,
) -> list[dict[str, float]]:
    """Return cadence gaps not covered by narration or informative dialogue.

    ``dialogue_counts_as_coverage`` reflects the narration style.  A companion
    documentary voice-over defers to informative dialogue, so speech in the
    source counts as covered.  A recap voice talks over the source instead, so
    dialogue must not suppress narration -- otherwise any video with continuous
    speech would legitimately produce almost no cues at all.
    """

    duration = float(script["sourceDurationSeconds"])
    covered: list[tuple[float, float]] = [
        (float(cue["startSeconds"]), float(cue["endSeconds"]))
        for cue in script["narrationCues"]
    ]
    if dialogue_counts_as_coverage:
        covered.extend(
            (float(item["startSeconds"]), float(item["endSeconds"]))
            for item in transcript_segments
            if str(item.get("text") or "").strip()
        )
    merged = _merge_ranges(covered, duration)
    gaps: list[dict[str, float]] = []
    cursor = 0.0
    for start, end in merged:
        if start - cursor > threshold_seconds:
            gaps.append(
                {
                    "startSeconds": round(cursor, 3),
                    "endSeconds": round(start, 3),
                    "durationSeconds": round(start - cursor, 3),
                }
            )
        cursor = max(cursor, end)
    if duration - cursor > threshold_seconds:
        gaps.append(
            {
                "startSeconds": round(cursor, 3),
                "endSeconds": round(duration, 3),
                "durationSeconds": round(duration - cursor, 3),
            }
        )
    return gaps


def pacing_warning(
    gaps: list[dict[str, float]],
    *,
    opening_cadence: bool = True,
    max_gap_seconds: float = PACING_RECOMMENDED_MAX_GAP_SECONDS,
) -> str:
    if not gaps:
        return ""
    ranges = "、".join(
        f"{item['startSeconds']:.3f}-{item['endSeconds']:.3f}秒"
        for item in gaps
    )
    if not opening_cadence:
        return f"非对白画面存在超过18秒的连续无解说区间：{ranges}"
    return (
        f"解说节奏未达到开场{PACING_OPENING_HOOK_MAX_START_SECONDS:g}秒内建立hook、"
        f"空档通常不超过{max_gap_seconds:g}秒的建议：{ranges}"
    )


def _merge_ranges(
    ranges: Iterable[tuple[float, float]], duration: float
) -> list[tuple[float, float]]:
    normalized = sorted(
        (max(0.0, start), min(duration, end))
        for start, end in ranges
        if end > start and end > 0 and start < duration
    )
    merged: list[tuple[float, float]] = []
    for start, end in normalized:
        if not merged or start > merged[-1][1]:
            merged.append((start, end))
        else:
            merged[-1] = (merged[-1][0], max(merged[-1][1], end))
    return merged
