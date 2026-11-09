from __future__ import annotations

import unicodedata
from dataclasses import dataclass
from typing import Any

from .models import ValidationError

COMPOSE_AUDIO_TRACK_LIMIT = 8
ASPECT_DIMENSIONS = {
    "16:9": (1920, 1080),
    "9:16": (1080, 1920),
    "1:1": (1080, 1080),
}
SUBTITLE_MAX_LINES = 2
SUBTITLE_MAX_CJK_CHARS = 24
_SUBTITLE_CLOSING_PUNCTUATION = frozenset("，。！？；：、,.!?;:)]】》”’")


@dataclass(frozen=True, slots=True)
class RenderSegment:
    id: str
    source_start_seconds: float
    duration_seconds: float
    narration: str
    ost: int
    audio_uri: str | None
    audio_duration_seconds: float | None = None


@dataclass(frozen=True, slots=True)
class RenderTimeline:
    segments: list[RenderSegment]
    script: dict[str, Any]
    warnings: list[str]


def _tts_audio(result: Any, cue_id: str) -> tuple[str, float]:
    if not isinstance(result, dict):
        raise ValidationError(f"TTS result for {cue_id} must be an object")
    artifacts = result.get("artifacts")
    metadata = result.get("metadata") or {}
    if not isinstance(artifacts, list) or not artifacts or not isinstance(artifacts[0], dict):
        raise ValidationError(f"TTS result for {cue_id} has no artifact")
    if not isinstance(metadata, dict):
        raise ValidationError(f"TTS result for {cue_id} has invalid metadata")
    audio_uri = str(artifacts[0].get("uri") or "").strip()
    length_ms = metadata.get("audioLengthMs")
    if not audio_uri:
        raise ValidationError(f"TTS result for {cue_id} has no artifact URI")
    if (
        isinstance(length_ms, bool)
        or not isinstance(length_ms, (int, float))
        or length_ms <= 0
    ):
        raise ValidationError(f"TTS result for {cue_id} has no audioLengthMs")
    return audio_uri, round(float(length_ms) / 1000, 3)


def _place_cues(
    cues: list[dict[str, Any]],
    source_duration: float,
) -> list[tuple[dict[str, Any], float, float]] | None:
    """Place measured narration durations near their requested positions.

    A forward pass preserves every requested start where possible.  When the
    final cue crosses EOF, a backward pass borrows nearby empty picture while
    keeping cue order.  Failure therefore means that the measured narration
    durations themselves cannot fit in the source, not merely that an LLM
    suggested a late start.
    """

    placed: list[list[Any]] = []
    cursor = 0.0
    for item in cues:
        duration = float(item["audioDurationSeconds"])
        start = max(float(item["cue"]["startSeconds"]), cursor)
        end = start + duration
        placed.append([item, start, end])
        cursor = end
    if not placed:
        return []
    if placed[-1][2] > source_duration + 1e-6:
        next_start = source_duration
        for index in range(len(placed) - 1, -1, -1):
            item = placed[index][0]
            duration = float(item["audioDurationSeconds"])
            end = min(float(placed[index][2]), next_start)
            start = end - duration
            placed[index][1] = start
            placed[index][2] = end
            next_start = start
    if placed[0][1] < -1e-6:
        return None
    return [
        (item["cue"], round(max(0.0, start), 3), round(end, 3))
        for item, start, end in placed
    ]


def resolve_render_timeline(
    script: dict[str, Any],
    tts_results: dict[str, dict[str, Any]],
) -> RenderTimeline:
    source_duration = float(script["sourceDurationSeconds"])
    measured: list[dict[str, Any]] = []
    for cue in script["narrationCues"]:
        cue_id = str(cue["id"])
        if cue_id not in tts_results:
            raise ValidationError(f"missing TTS result for {cue_id}")
        audio_uri, audio_duration = _tts_audio(tts_results[cue_id], cue_id)
        measured.append(
            {
                "cue": cue,
                "audioUri": audio_uri,
                "audioDurationSeconds": audio_duration,
            }
        )

    active = list(measured)
    dropped: list[dict[str, Any]] = []
    placement = _place_cues(active, source_duration)
    truncated: list[dict[str, Any]] = []
    while placement is None and len(active) > 1:
        # Remove the least important item first. For equal importance, prefer
        # removing the later cue so the hook and established context survive.
        drop_index = min(
            range(len(active)),
            key=lambda index: (
                int(active[index]["cue"]["importance"]),
                -float(active[index]["cue"]["startSeconds"]),
            ),
        )
        dropped.append(active.pop(drop_index))
        placement = _place_cues(active, source_duration)
    if placement is None and active:
        # Keep the last (therefore highest-priority) cue and cap only its audio
        # overlay at EOF. This preserves the v2 contract's non-empty cue list
        # even when one provider returns speech longer than the entire source.
        active[0] = {
            **active[0],
            "audioDurationSeconds": round(source_duration, 3),
        }
        truncated.append(active[0])
        placement = _place_cues(active, source_duration)
    placement = placement or []

    measured_by_original_id = {str(item["cue"]["id"]): item for item in active}
    adjusted_cues: list[dict[str, Any]] = []
    resolved: list[RenderSegment] = []
    cursor = 0.0
    gap_index = 0
    for cue_index, (cue, start, end) in enumerate(placement, start=1):
        start = max(cursor, start)
        if start > cursor + 1e-6:
            gap_index += 1
            resolved.append(
                RenderSegment(
                    id=f"gap-{gap_index:03d}",
                    source_start_seconds=round(cursor, 3),
                    duration_seconds=round(start - cursor, 3),
                    narration="",
                    ost=1,
                    audio_uri=None,
                )
            )
        original_id = str(cue["id"])
        measured_item = measured_by_original_id[original_id]
        cue_id = f"cue-{cue_index:03d}"
        audio_duration = float(measured_item["audioDurationSeconds"])
        end = min(source_duration, round(start + audio_duration, 3))
        adjusted_cues.append(
            {
                **cue,
                "id": cue_id,
                "startSeconds": round(start, 3),
                "endSeconds": end,
            }
        )
        resolved.append(
            RenderSegment(
                id=cue_id,
                source_start_seconds=round(start, 3),
                duration_seconds=round(end - start, 3),
                narration=str(cue["narration"]),
                ost=2,
                audio_uri=str(measured_item["audioUri"]),
                audio_duration_seconds=round(end - start, 3),
            )
        )
        cursor = end
    if cursor < source_duration - 1e-6:
        gap_index += 1
        resolved.append(
            RenderSegment(
                id=f"gap-{gap_index:03d}",
                source_start_seconds=round(cursor, 3),
                duration_seconds=round(source_duration - cursor, 3),
                narration="",
                ost=1,
                audio_uri=None,
            )
        )

    warnings: list[str] = []
    if dropped:
        dropped_ids = ", ".join(str(item["cue"]["id"]) for item in dropped)
        warnings.append(
            f"TTS 实际总时长无法同时放入原片，已移除低优先级解说：{dropped_ids}"
        )
    if truncated:
        warnings.append("最高优先级解说仍长于原片，已在片尾截断其音频覆盖时长")
    adjusted_script = {**script, "narrationCues": adjusted_cues}
    return RenderTimeline(segments=resolved, script=adjusted_script, warnings=warnings)


def resolve_render_segments(
    script: dict[str, Any],
    tts_results: dict[str, dict[str, Any]],
) -> list[RenderSegment]:
    """Compatibility convenience returning the full-video segment partition."""

    return resolve_render_timeline(script, tts_results).segments


def build_process_operations(segment: RenderSegment, aspect: str) -> list[dict[str, Any]]:
    operations: list[dict[str, Any]] = [
        {
            "type": "trim",
            "startSeconds": segment.source_start_seconds,
            "durationSeconds": segment.duration_seconds,
        }
    ]
    # compose_video cannot lower embedded video audio. For ost=2, the workflow
    # extracts this exact source window and mixes it back as a quieter track.
    if segment.ost in (0, 2):
        operations.append({"type": "mute"})
    if aspect != "source":
        width, height = ASPECT_DIMENSIONS[aspect]
        operations.append({"type": "reframe", "width": width, "height": height, "fit": "cover"})
    return operations


def _subtitle_cell_width(character: str) -> int:
    return 2 if unicodedata.east_asian_width(character) in {"W", "F", "A"} else 1


def _subtitle_line_cells(video_width: int, video_height: int) -> int:
    # Keep subtitles inside a centered 78% safe area. The Core renderer's
    # default glyph height scales at roughly 3.4% of the output height.
    safe_width_pixels = video_width * 0.78
    estimated_font_pixels = max(32.0, video_height * 0.034)
    cjk_characters = max(
        8,
        min(SUBTITLE_MAX_CJK_CHARS, int(safe_width_pixels / estimated_font_pixels)),
    )
    return cjk_characters * 2


def _wrap_subtitle_lines(text: str, max_cells: int) -> list[str]:
    normalized = " ".join(text.split())
    if not normalized:
        return []
    total_cells = sum(_subtitle_cell_width(character) for character in normalized)
    target_line_count = max(1, (total_cells + max_cells - 1) // max_cells)
    if target_line_count > SUBTITLE_MAX_LINES and target_line_count % SUBTITLE_MAX_LINES:
        target_line_count += 1
    target_cells = min(
        max_cells,
        (total_cells + target_line_count - 1) // target_line_count + 1,
    )
    lines: list[str] = []
    current: list[str] = []
    current_cells = 0
    # 本行里最后一个句读或空格的位置。换行优先落在这里，否则窄画幅下每行只放得下
    # 五六个汉字，词语会被从中间劈开（"一朝灭/门"、"se/ntence"）。
    last_break_index = -1

    def flush() -> None:
        nonlocal current, current_cells, last_break_index
        line = "".join(current).strip()
        if line:
            lines.append(line)
        current = []
        current_cells = 0
        last_break_index = -1

    def carry_from(index: int) -> None:
        """在 index 之后断行，把剩余字符带到下一行。"""

        nonlocal current, current_cells, last_break_index
        carried = current[index + 1 :]
        del current[index + 1 :]
        current_cells = sum(_subtitle_cell_width(item) for item in current)
        flush()
        current = carried
        current_cells = sum(_subtitle_cell_width(item) for item in carried)
        last_break_index = -1

    for character in normalized:
        cells = _subtitle_cell_width(character)
        if current and current_cells + cells > target_cells:
            if character in _SUBTITLE_CLOSING_PUNCTUATION:
                # 句读不能落在行首。只要没顶到安全区宽度就让它悬挂在行尾，
                # 把前一个字符拽下来会在词中间断开，比稍微超出一格更糟。
                if current_cells + cells > max_cells:
                    carry_from(len(current) - 2)
            elif last_break_index >= 0 and last_break_index < len(current) - 1:
                # 只有断点不至于把整行掏空时才用它，否则宁可按宽度断。
                head_cells = sum(
                    _subtitle_cell_width(item) for item in current[: last_break_index + 1]
                )
                if head_cells * 2 >= target_cells:
                    carry_from(last_break_index)
                else:
                    flush()
            else:
                flush()
        if character == " " and not current:
            continue
        current.append(character)
        current_cells += cells
        if character in _SUBTITLE_CLOSING_PUNCTUATION or character == " ":
            last_break_index = len(current) - 1
    flush()
    return lines


def _subtitle_cues(
    text: str,
    *,
    start_seconds: float,
    end_seconds: float,
    video_width: int,
    video_height: int,
) -> list[dict[str, Any]]:
    lines = _wrap_subtitle_lines(text, _subtitle_line_cells(video_width, video_height))
    chunks = [
        "\n".join(lines[index : index + SUBTITLE_MAX_LINES])
        for index in range(0, len(lines), SUBTITLE_MAX_LINES)
    ]
    if not chunks:
        return []
    weights = [
        sum(_subtitle_cell_width(character) for character in chunk if character != "\n")
        for chunk in chunks
    ]
    total_weight = sum(weights)
    duration = end_seconds - start_seconds
    cues: list[dict[str, Any]] = []
    elapsed_weight = 0
    cue_start = start_seconds
    for index, (chunk, weight) in enumerate(zip(chunks, weights, strict=True)):
        elapsed_weight += weight
        cue_end = (
            end_seconds
            if index == len(chunks) - 1
            else round(start_seconds + duration * elapsed_weight / total_weight, 3)
        )
        cues.append(
            {
                "startSeconds": round(cue_start, 3),
                "endSeconds": round(cue_end, 3),
                "text": chunk,
            }
        )
        cue_start = cue_end
    return cues


def build_compose_batches(
    segments: list[RenderSegment],
    clip_uris: dict[str, str],
    *,
    narration_volume: float,
    original_audio_uris: dict[str, str] | None = None,
    original_audio_volume: float = 0.2,
    subtitles: bool,
    video_width: int = 1080,
    video_height: int = 1080,
) -> list[dict[str, Any]]:
    original_audio_uris = original_audio_uris or {}
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
            original_audio_uri = original_audio_uris.get(segment.id)
            if original_audio_uri:
                audio_tracks.append(
                    {
                        "url": original_audio_uri,
                        "startSeconds": round(offset, 3),
                        "durationSeconds": segment.duration_seconds,
                        "volume": original_audio_volume,
                    }
                )
            if segment.audio_uri:
                audio_duration = (
                    segment.audio_duration_seconds
                    if segment.audio_duration_seconds is not None
                    else segment.duration_seconds
                )
                audio_tracks.append(
                    {
                        "url": segment.audio_uri,
                        "startSeconds": round(offset, 3),
                        "durationSeconds": audio_duration,
                        "volume": narration_volume,
                    }
                )
                if subtitles and segment.narration:
                    subtitle_segments.extend(
                        _subtitle_cues(
                            segment.narration,
                            start_seconds=round(offset, 3),
                            end_seconds=round(offset + audio_duration, 3),
                            video_width=video_width,
                            video_height=video_height,
                        )
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
        adds_audio = int(segment.id in original_audio_uris) + int(bool(segment.audio_uri))
        if current and (
            len(current) >= COMPOSE_AUDIO_TRACK_LIMIT
            or current_audio_count + adds_audio > COMPOSE_AUDIO_TRACK_LIMIT
        ):
            flush()
        current.append(segment)
        current_audio_count += adds_audio
    flush()
    return batches
