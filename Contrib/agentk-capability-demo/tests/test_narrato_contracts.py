import math

import pytest
from jsonschema import ValidationError as JsonSchemaValidationError
from jsonschema import validate

from agentk_capability_demo.agent100.narrato.contracts import (
    CREATE_INPUT_SCHEMA,
    DOCUMENTARY_SCRIPT_SCHEMA,
    GENERATE_INPUT_SCHEMA,
    RENDER_INPUT_SCHEMA,
    RenderSegment,
    ValidationError,
    build_analysis_windows,
    build_compose_batches,
    build_process_operations,
    normalize_create_input,
    normalize_render_input,
    post_event_timing_issues,
    resolve_render_segments,
    resolve_render_timeline,
    validate_documentary_script,
)


def profile():
    return {
        "primaryGenre": "纪录片",
        "secondaryGenres": ["历史人文"],
        "thesis": "人在环境变化中的选择。",
        "hook": "为什么所有人都在天亮前离开？",
        "style": "准确、紧凑、有悬念",
        "pacing": "开场快，中段留白，转折加重",
        "globalTtsInstruction": "沉稳清晰，带轻微紧迫感。",
    }


def cue(index=1, start=1.0, end=5.0, **overrides):
    value = {
        "id": f"cue-{index:03d}",
        "startSeconds": start,
        "endSeconds": end,
        "narration": f"第 {index} 段解说",
        "evidenceWindowIndexes": [index - 1],
        "ttsInstruction": "自然叙述，关键词稍加重。",
        "importance": 3,
    }
    value.update(overrides)
    return value


def script(cues=None, duration=100.0):
    return {
        "version": "2",
        "sourceVideoUri": "https://cdn.test/source.mp4",
        "sourceDurationSeconds": duration,
        "narrativeProfile": profile(),
        "narrationCues": cues or [cue()],
    }


def qwen_tts():
    return {"provider": "qwen", "model": "qwen3-tts-flash", "voiceId": "Cherry"}


def tts_result(name: str, milliseconds: int):
    return {
        "artifacts": [{"uri": f"https://cdn.test/{name}.mp3"}],
        "metadata": {"audioLengthMs": milliseconds},
    }


def test_analysis_windows_include_short_tail():
    assert build_analysis_windows(65) == [
        {"index": 0, "startSeconds": 0.0, "endSeconds": 30.0},
        {"index": 1, "startSeconds": 30.0, "endSeconds": 60.0},
        {"index": 2, "startSeconds": 60.0, "endSeconds": 65.0},
    ]


@pytest.mark.parametrize("value", [0, -1, math.inf, math.nan])
def test_analysis_windows_reject_bad_duration(value):
    with pytest.raises(ValidationError):
        build_analysis_windows(value)


def test_pipeline_json_schemas_use_v2_and_provider_tts_contract():
    valid_script = script()
    validate({"videoUri": "https://cdn.test/a.mp4"}, GENERATE_INPUT_SCHEMA)
    validate({"videoUri": "https://cdn.test/a.mp4", "tts": qwen_tts()}, CREATE_INPUT_SCHEMA)
    validate({"script": valid_script, "tts": qwen_tts()}, RENDER_INPUT_SCHEMA)
    validate(valid_script, DOCUMENTARY_SCRIPT_SCHEMA)
    with pytest.raises(JsonSchemaValidationError):
        validate(
            {"videoUri": "https://cdn.test/a.mp4", "tts": qwen_tts(), "theme": "自然"},
            CREATE_INPUT_SCHEMA,
        )


def test_automatic_script_rewrites_ids_and_accepts_minimax_emotion():
    value = script([cue(id="model-id", emotion="surprised")])
    normalized = validate_documentary_script(value, automatic=True)
    assert normalized["narrationCues"][0]["id"] == "cue-001"
    assert normalized["narrationCues"][0]["emotion"] == "surprised"


def test_post_event_timing_issues_are_soft_and_do_not_reject_script():
    windows = [{"index": 0, "startSeconds": 0.0, "endSeconds": 30.0}]
    missing = script([cue(start=12, end=15)])
    normalized = validate_documentary_script(missing, automatic=True)
    assert post_event_timing_issues(normalized, windows) == [
        {
            "type": "missingEvidenceEnd",
            "cueId": "cue-001",
            "message": "缺少 evidenceEndSeconds，无法确认解说是否晚于事件。",
        }
    ]

    premature = script([cue(start=12, end=15, evidenceEndSeconds=13)])
    normalized = validate_documentary_script(premature, automatic=True)
    issues = post_event_timing_issues(normalized, windows)
    assert issues[0]["type"] == "startsBeforeEvidence"
    assert normalized["narrationCues"][0]["startSeconds"] == 12

    outside = validate_documentary_script(
        script([cue(start=12, end=15, evidenceEndSeconds=-1)]), automatic=True
    )
    assert post_event_timing_issues(outside, windows)[0]["type"] == ("evidenceTimeOutsideSource")


def test_script_rejects_v1_overlap_and_invalid_emotion():
    legacy = {**script(), "version": "1"}
    with pytest.raises(ValidationError, match="version"):
        validate_documentary_script(legacy)
    overlapping = script([cue(1, 0, 5), cue(2, 4, 7)])
    with pytest.raises(ValidationError, match="non-overlapping"):
        validate_documentary_script(overlapping)
    invalid_emotion = script([cue(emotion="excited")])
    with pytest.raises(ValidationError, match="emotion"):
        validate_documentary_script(invalid_emotion)


def test_create_and_render_input_defaults_and_reject_legacy_fields():
    normalized = normalize_create_input({"videoUri": "https://cdn.test/a.mp4", "tts": qwen_tts()})
    assert normalized["aspect"] == "source"
    assert normalized["subtitles"] is True
    assert normalized["tts"] == qwen_tts()
    with pytest.raises(ValidationError, match="HTTP"):
        normalize_create_input({"videoUri": "/tmp/a.mp4", "tts": qwen_tts()})
    with pytest.raises(ValidationError, match="theme"):
        normalize_create_input(
            {"videoUri": "https://cdn.test/a.mp4", "tts": qwen_tts(), "theme": "自然"}
        )
    with pytest.raises(ValidationError, match="voiceId"):
        normalize_render_input({"script": script(), "voiceId": "Cherry"})


def test_content_model_overrides_no_longer_accept_tts_slot():
    normalized = normalize_create_input(
        {
            "videoUri": "https://cdn.test/a.mp4",
            "tts": qwen_tts(),
            "models": {
                "vision": "gemini-3.1-pro-preview",
                "asr": "azure-whisper",
                "story": "gpt-5.4",
                "script": "deepseek-v4-pro",
            },
            "analysisOptions": {"asrEnabled": True, "asrLanguage": "zh"},
        }
    )
    assert normalized["models"]["story"] == "gpt-5.4"
    assert normalized["analysisOptions"]["asrLanguage"] == "zh"
    with pytest.raises(ValidationError, match="models.tts"):
        normalize_create_input(
            {
                "videoUri": "https://cdn.test/a.mp4",
                "tts": qwen_tts(),
                "models": {"tts": "qwen3-tts-flash"},
            }
        )


@pytest.mark.parametrize("value", ["false", 0, 1])
def test_create_input_requires_real_boolean_subtitles(value):
    with pytest.raises(ValidationError, match="subtitles"):
        normalize_create_input(
            {"videoUri": "https://cdn.test/a.mp4", "tts": qwen_tts(), "subtitles": value}
        )


@pytest.mark.parametrize("field", ["narrationVolume", "originalAudioVolume"])
@pytest.mark.parametrize("volume", [-0.1, 2.1, math.nan, math.inf, True])
def test_render_input_rejects_invalid_volumes(field, volume):
    with pytest.raises(ValidationError, match=field):
        normalize_render_input({"script": script(), "tts": qwen_tts(), field: volume})


def test_original_audio_volume_defaults_to_quiet_background_level():
    assert (
        normalize_render_input({"script": script(), "tts": qwen_tts()})["originalAudioVolume"]
        == 0.2
    )


def test_full_video_timeline_has_no_deleted_or_repeated_picture():
    normalized = validate_documentary_script(
        script(
            [
                cue(1, 2, 4),
                cue(2, 8, 10, evidenceWindowIndexes=[1]),
            ],
            duration=12,
        )
    )
    timeline = resolve_render_timeline(
        normalized,
        {"cue-001": tts_result("a", 3000), "cue-002": tts_result("b", 2000)},
    )
    assert timeline.segments[0].source_start_seconds == 0
    assert round(sum(item.duration_seconds for item in timeline.segments), 3) == 12
    assert all(
        round(left.source_start_seconds + left.duration_seconds, 3) == right.source_start_seconds
        for left, right in zip(timeline.segments, timeline.segments[1:])
    )
    assert timeline.segments[-1].source_start_seconds + timeline.segments[-1].duration_seconds == 12
    assert [item.ost for item in timeline.segments] == [1, 2, 1, 2, 1]


def test_actual_tts_is_moved_near_eof_without_losing_picture():
    normalized = validate_documentary_script(script([cue(1, 9, 10)], duration=10))
    timeline = resolve_render_timeline(normalized, {"cue-001": tts_result("a", 3500)})
    narration = next(item for item in timeline.segments if item.audio_uri)
    assert narration.source_start_seconds == 6.5
    assert narration.duration_seconds == 3.5
    assert round(sum(item.duration_seconds for item in timeline.segments), 3) == 10


def test_evidence_time_does_not_hard_limit_tts_placement():
    normalized = validate_documentary_script(
        script([cue(1, 9, 10, evidenceEndSeconds=8)], duration=10)
    )
    timeline = resolve_render_timeline(normalized, {"cue-001": tts_result("a", 3500)})
    narration = next(item for item in timeline.segments if item.audio_uri)
    assert narration.source_start_seconds == 6.5
    assert narration.duration_seconds == 3.5
    assert timeline.script["narrationCues"][0]["evidenceEndSeconds"] == 8


def test_impossible_tts_set_drops_lowest_importance_and_returns_warning():
    normalized = validate_documentary_script(
        script(
            [
                cue(1, 0, 2, importance=5),
                cue(2, 3, 5, importance=1, evidenceWindowIndexes=[1]),
            ],
            duration=5,
        )
    )
    timeline = resolve_render_timeline(
        normalized,
        {"cue-001": tts_result("important", 4000), "cue-002": tts_result("minor", 3000)},
    )
    assert len(timeline.script["narrationCues"]) == 1
    assert timeline.script["narrationCues"][0]["narration"] == "第 1 段解说"
    assert "cue-002" in timeline.warnings[0]
    assert round(sum(item.duration_seconds for item in timeline.segments), 3) == 5


def test_render_rejects_missing_or_malformed_tts_results():
    normalized = validate_documentary_script(script())
    with pytest.raises(ValidationError, match="missing TTS"):
        resolve_render_segments(normalized, {})
    with pytest.raises(ValidationError, match="audioLengthMs"):
        resolve_render_segments(
            normalized,
            {"cue-001": {"artifacts": [{"uri": "https://cdn.test/a.mp3"}], "metadata": {}}},
        )


def test_process_operations_mute_only_narration_windows():
    narrated = RenderSegment("cue-001", 1.0, 2.0, "解说", 2, "https://cdn/a.mp3", 2.0)
    original = RenderSegment("gap-001", 0.0, 1.0, "", 1, None)
    assert build_process_operations(narrated, "9:16") == [
        {"type": "trim", "startSeconds": 1.0, "durationSeconds": 2.0},
        {"type": "mute"},
        {"type": "reframe", "width": 1080, "height": 1920, "fit": "cover"},
    ]
    assert build_process_operations(original, "source") == [
        {"type": "trim", "startSeconds": 0.0, "durationSeconds": 1.0}
    ]


def test_compose_uses_audio_duration_and_wraps_vertical_subtitles():
    narration = "楼里只剩一扇窗亮着。林悦在十一楼的走廊停下脚步，听见门后传来低语。"
    segment = RenderSegment(
        "cue-001", 0.0, 6.2, narration, 2, "https://cdn.test/narration.mp3", 6.0
    )
    batch = build_compose_batches(
        [segment],
        {segment.id: "https://cdn.test/clip.mp4"},
        narration_volume=1.0,
        subtitles=True,
        video_width=1080,
        video_height=1920,
    )[0]
    assert batch["audioTracks"][0]["durationSeconds"] == 6.0
    cues = batch["subtitleSegments"]
    assert len(cues) >= 2
    assert cues[0]["startSeconds"] == 0.0
    assert cues[-1]["endSeconds"] == 6.0
    assert all(len(cue["text"].splitlines()) <= 2 for cue in cues)


@pytest.mark.parametrize(
    ("narration", "forbidden"),
    [
        # 竖屏每行只放得下约 12 个汉字，按宽度硬断会把词劈开。
        ("没想到一朝灭门，他披上黑衣，独行风雪，走向苍茫的山海。", "灭\n门"),
        ("短短十二秒，江湖的苍凉与执着瞬间拉满。", "十二\n秒"),
        ("A long English sentence without any punctuation to break", "se\nntence"),
    ],
)
def test_vertical_subtitles_break_on_punctuation_not_mid_word(narration, forbidden):
    segment = RenderSegment(
        "cue-001", 0.0, 12.0, narration, 2, "https://cdn.test/narration.mp3", 12.0
    )
    batch = build_compose_batches(
        [segment],
        {segment.id: "https://cdn.test/clip.mp4"},
        narration_volume=1.0,
        subtitles=True,
        video_width=1076,
        video_height=1920,
    )[0]
    rendered = "\n".join(cue["text"] for cue in batch["subtitleSegments"])
    assert forbidden not in rendered
    # 断行只能改变换行位置，不能吞字。
    assert "".join(rendered.split()) == "".join(narration.split())


def test_compose_mixes_original_audio_and_shares_eight_track_limit():
    segments = [
        RenderSegment(f"cue-{i:03d}", float(i), 1.0, f"n{i}", 2, f"https://cdn/n{i}.mp3")
        for i in range(1, 6)
    ]
    clips = {item.id: f"https://cdn/{item.id}.mp4" for item in segments}
    originals = {item.id: f"https://cdn/o{item.id}.mp3" for item in segments}
    batches = build_compose_batches(
        segments,
        clips,
        narration_volume=1.0,
        original_audio_uris=originals,
        original_audio_volume=0.2,
        subtitles=True,
    )
    assert [len(batch["audioTracks"]) for batch in batches] == [8, 2]


def test_nine_original_only_segments_split_by_clip_limit():
    segments = [RenderSegment(f"gap-{i:03d}", float(i), 1.0, "", 1, None) for i in range(9)]
    clips = {item.id: f"https://cdn.test/{item.id}.mp4" for item in segments}
    batches = build_compose_batches(segments, clips, narration_volume=1.0, subtitles=True)
    assert [len(batch["videoUrls"]) for batch in batches] == [8, 1]
