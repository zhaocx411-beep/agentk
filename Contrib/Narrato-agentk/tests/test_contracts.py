import math

import pytest
from jsonschema import ValidationError as JsonSchemaValidationError
from jsonschema import validate

from narrato_agentk.contracts import (
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
    resolve_render_segments,
    validate_documentary_script,
)


def script(segments=None, duration=100.0):
    return {
        "version": "1",
        "sourceVideoUri": "https://cdn.test/source.mp4",
        "sourceDurationSeconds": duration,
        "theme": "nature",
        "segments": segments
        or [
            {
                "id": "segment-001",
                "sourceStartSeconds": 0,
                "sourceEndSeconds": 10,
                "narration": "第一段",
                "ost": 2,
            }
        ],
    }


def test_analysis_windows_include_short_tail():
    assert build_analysis_windows(65) == [
        {"index": 0, "startSeconds": 0.0, "endSeconds": 30.0},
        {"index": 1, "startSeconds": 30.0, "endSeconds": 60.0},
        {"index": 2, "startSeconds": 60.0, "endSeconds": 65.0},
    ]


def test_pipeline_json_schemas_accept_and_reject_contract_examples():
    valid_script = script()
    validate({"videoUri": "https://cdn.test/a.mp4"}, GENERATE_INPUT_SCHEMA)
    validate(
        {"videoUri": "https://cdn.test/a.mp4", "voiceId": "Cherry"},
        CREATE_INPUT_SCHEMA,
    )
    validate({"script": valid_script, "voiceId": "Cherry"}, RENDER_INPUT_SCHEMA)
    validate(valid_script, DOCUMENTARY_SCRIPT_SCHEMA)
    with pytest.raises(JsonSchemaValidationError):
        validate(
            {"videoUri": "https://cdn.test/a.mp4", "voiceId": "Cherry", "extra": True},
            CREATE_INPUT_SCHEMA,
        )


@pytest.mark.parametrize("value", [0, -1, math.inf, math.nan])
def test_analysis_windows_reject_bad_duration(value):
    with pytest.raises(ValidationError):
        build_analysis_windows(value)


def test_automatic_script_rewrites_ids_and_ost():
    value = script()
    value["segments"][0]["id"] = "model-id"
    value["segments"][0]["ost"] = 0
    normalized = validate_documentary_script(value, automatic=True)
    assert normalized["segments"][0]["id"] == "segment-001"
    assert normalized["segments"][0]["ost"] == 2


def test_automatic_script_rejects_narration_without_enough_source_tail():
    value = script(
        [
            {
                "id": "model-id",
                "sourceStartSeconds": 9,
                "sourceEndSeconds": 10,
                "narration": "这是一段明显无法在最后一秒正常读完的旁白",
                "ost": 0,
            }
        ],
        duration=10,
    )
    with pytest.raises(ValidationError, match="does not leave enough source duration"):
        validate_documentary_script(value, automatic=True)

    # 人工脚本不使用字符估算；渲染阶段会以真实 TTS/ffprobe 时长严格校验。
    assert validate_documentary_script(value, automatic=False)["segments"][0]["ost"] == 0


def test_script_rejects_duplicate_ids_and_out_of_order_segments():
    segments = [
        {"id": "same", "sourceStartSeconds": 5, "sourceEndSeconds": 7, "narration": "a", "ost": 2},
        {"id": "same", "sourceStartSeconds": 1, "sourceEndSeconds": 2, "narration": "b", "ost": 2},
    ]
    with pytest.raises(ValidationError):
        validate_documentary_script(script(segments))


def test_create_input_defaults_and_rejects_local_paths():
    normalized = normalize_create_input({"videoUri": "https://cdn.test/a.mp4", "voiceId": "Cherry"})
    assert normalized["aspect"] == "source"
    assert normalized["subtitles"] is True
    with pytest.raises(ValidationError):
        normalize_create_input({"videoUri": "/tmp/a.mp4", "voiceId": "Cherry"})


@pytest.mark.parametrize("value", ["false", 0, 1, None])
def test_create_input_requires_real_boolean_subtitles(value):
    raw = {"videoUri": "https://cdn.test/a.mp4", "voiceId": "Cherry", "subtitles": value}
    if value is None:
        raw.pop("subtitles")
        assert normalize_create_input(raw)["subtitles"] is True
    else:
        with pytest.raises(ValidationError, match="subtitles"):
            normalize_create_input(raw)


def test_script_rejects_wrong_version_boolean_ost_and_empty_narration():
    wrong_version = script()
    wrong_version["version"] = "2"
    with pytest.raises(ValidationError, match="version"):
        validate_documentary_script(wrong_version)

    boolean_ost = script()
    boolean_ost["segments"][0]["ost"] = True
    with pytest.raises(ValidationError, match="ost"):
        validate_documentary_script(boolean_ost)

    empty_narration = script()
    empty_narration["segments"][0]["narration"] = "   "
    with pytest.raises(ValidationError, match="narration"):
        validate_documentary_script(empty_narration)


@pytest.mark.parametrize("volume", [-0.1, 2.1, math.nan, math.inf, True])
def test_render_input_rejects_invalid_narration_volume(volume):
    with pytest.raises(ValidationError, match="narrationVolume"):
        normalize_render_input({"script": script(), "voiceId": "Cherry", "narrationVolume": volume})


def test_ost_rules_resolve_real_tts_duration():
    segments = [
        {"id": "s0", "sourceStartSeconds": 0, "sourceEndSeconds": 10, "narration": "a", "ost": 0},
        {
            "id": "s1",
            "sourceStartSeconds": 10,
            "sourceEndSeconds": 14,
            "narration": "保留原声",
            "ost": 1,
        },
        {"id": "s2", "sourceStartSeconds": 14, "sourceEndSeconds": 20, "narration": "b", "ost": 2},
    ]
    normalized = validate_documentary_script(script(segments, duration=30))
    result = resolve_render_segments(
        normalized,
        {
            "s0": {
                "artifacts": [{"uri": "https://cdn.test/a.mp3"}],
                "metadata": {"audioLengthMs": 2500},
            },
            "s2": {
                "artifacts": [{"uri": "https://cdn.test/b.mp3"}],
                "metadata": {"audioLengthMs": 3750},
            },
        },
    )
    assert [item.duration_seconds for item in result] == [2.5, 4.0, 3.75]
    assert build_process_operations(result[0], "9:16") == [
        {"type": "trim", "startSeconds": 0.0, "durationSeconds": 2.5},
        {"type": "mute"},
        {"type": "reframe", "width": 1080, "height": 1920, "fit": "cover"},
    ]


def test_render_rejects_missing_tts_duration_and_source_overrun():
    normalized = validate_documentary_script(
        script(
            [
                {
                    "id": "segment-001",
                    "sourceStartSeconds": 0,
                    "sourceEndSeconds": 5,
                    "narration": "第一段",
                    "ost": 2,
                }
            ],
            duration=5,
        )
    )
    with pytest.raises(ValidationError, match="audioLengthMs"):
        resolve_render_segments(
            normalized,
            {"segment-001": {"artifacts": [{"uri": "https://cdn.test/a.mp3"}], "metadata": {}}},
        )
    with pytest.raises(ValidationError, match="exceeds"):
        resolve_render_segments(
            normalized,
            {
                "segment-001": {
                    "artifacts": [{"uri": "https://cdn.test/a.mp3"}],
                    "metadata": {"audioLengthMs": 6000},
                }
            },
        )


@pytest.mark.parametrize(
    "result",
    [
        "not-an-object",
        {"artifacts": ["not-an-artifact"], "metadata": {"audioLengthMs": 1000}},
        {"artifacts": [{"uri": "https://cdn.test/a.mp3"}], "metadata": "bad"},
    ],
)
def test_render_rejects_malformed_tts_result_without_attribute_errors(result):
    normalized = validate_documentary_script(script())
    with pytest.raises(ValidationError, match="TTS result"):
        resolve_render_segments(normalized, {"segment-001": result})


def test_nine_narrated_segments_split_into_two_compose_batches():
    segments = [
        RenderSegment(f"s{i}", float(i), 1.0, f"n{i}", 2, f"https://cdn.test/{i}.mp3")
        for i in range(9)
    ]
    clips = {item.id: f"https://cdn.test/{item.id}.mp4" for item in segments}
    batches = build_compose_batches(segments, clips, narration_volume=1.0, subtitles=True)
    assert [len(batch["audioTracks"]) for batch in batches] == [8, 1]
    assert batches[0]["audioTracks"][7]["startSeconds"] == 7.0
    assert batches[1]["subtitleSegments"][0]["startSeconds"] == 0.0


def test_nine_ost_one_segments_also_split_by_clip_limit():
    segments = [RenderSegment(f"s{i}", float(i), 1.0, "原声", 1, None) for i in range(9)]
    clips = {item.id: f"https://cdn.test/{item.id}.mp4" for item in segments}
    batches = build_compose_batches(
        segments,
        clips,
        narration_volume=1.0,
        subtitles=True,
    )
    assert [len(batch["videoUrls"]) for batch in batches] == [8, 1]
