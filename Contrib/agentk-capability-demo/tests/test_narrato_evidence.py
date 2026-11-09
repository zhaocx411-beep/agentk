import pytest

from agentk_capability_demo.agent100.narrato.contracts import (
    ValidationError,
    normalize_asr_output,
    validate_corrected_transcript,
    validate_narration_draft,
    validate_story_analysis,
)


def test_asr_output_is_offset_to_source_timeline():
    result = normalize_asr_output(
        {
            "text": "你好世界",
            "language": "zh",
            "segments": [
                {"id": 0, "start": 0.25, "end": 1.5, "text": "你好"},
                {"id": 1, "start": 1.6, "end": 2.8, "text": "世界"},
            ],
        },
        window_index=2,
        start_seconds=60,
        end_seconds=65,
    )

    assert result["segments"] == [
        {"id": "asr-002-000", "startSeconds": 60.25, "endSeconds": 61.5, "text": "你好"},
        {"id": "asr-002-001", "startSeconds": 61.6, "endSeconds": 62.8, "text": "世界"},
    ]


def test_asr_output_ignores_empty_azure_sentinel_segment():
    result = normalize_asr_output(
        {
            "text": "All right.",
            "language": "english",
            "segments": [
                {"id": 0, "start": 0, "end": 1.64, "text": " All right."},
                {"id": 1, "start": 25.04, "end": 25.04, "text": ""},
            ],
        },
        window_index=5,
        start_seconds=150,
        end_seconds=180,
        audio_duration_seconds=30,
    )

    assert result["segments"] == [
        {
            "id": "asr-005-000",
            "startSeconds": 150.0,
            "endSeconds": 151.64,
            "text": "All right.",
        }
    ]


def test_asr_output_rejects_segments_that_start_well_outside_extracted_audio():
    with pytest.raises(ValidationError, match="outside its audio window"):
        normalize_asr_output(
            {"text": "越界", "segments": [{"start": 31, "end": 32, "text": "越界"}]},
            window_index=0,
            start_seconds=0,
            end_seconds=30,
        )


def test_asr_output_accepts_encoder_padding_measured_by_core_and_clamps_to_window():
    result = normalize_asr_output(
        {
            "text": "片尾对白",
            "segments": [{"start": 29.8, "end": 30.4, "text": "片尾对白"}],
        },
        window_index=0,
        start_seconds=0,
        end_seconds=30,
        audio_duration_seconds=30.35,
    )
    assert result["segments"][0]["endSeconds"] == 30


def test_asr_output_clamps_provider_word_boundary_overhang():
    result = normalize_asr_output(
        {
            "text": "末段",
            "segments": [{"start": 28.3, "end": 30.3, "text": "末段"}],
        },
        window_index=0,
        start_seconds=0,
        end_seconds=30,
        audio_duration_seconds=30,
    )
    assert result["segments"][0]["endSeconds"] == 30


def test_asr_output_clamps_large_end_overhang_when_segment_starts_inside_window():
    result = normalize_asr_output(
        {
            "text": "末段",
            "segments": [{"start": 28.3, "end": 32.1, "text": "末段"}],
        },
        window_index=0,
        start_seconds=0,
        end_seconds=30,
        audio_duration_seconds=30,
    )
    assert result["segments"][0] == {
        "id": "asr-000-000",
        "startSeconds": 28.3,
        "endSeconds": 30,
        "text": "末段",
    }


def test_asr_output_drops_segment_wholly_inside_encoder_padding():
    result = normalize_asr_output(
        {
            "text": "有效对白",
            "segments": [
                {"start": 28.0, "end": 29.0, "text": "有效对白"},
                {"start": 30.2, "end": 30.4, "text": "编码尾音"},
            ],
        },
        window_index=0,
        start_seconds=0,
        end_seconds=30,
        audio_duration_seconds=30.4,
    )
    assert [item["text"] for item in result["segments"]] == ["有效对白"]


def test_asr_output_clamps_small_negative_provider_start_to_zero():
    result = normalize_asr_output(
        {
            "text": "开场",
            "segments": [{"start": -0.2, "end": 0.8, "text": "开场"}],
        },
        window_index=2,
        start_seconds=60,
        end_seconds=90,
    )
    assert result["segments"][0]["startSeconds"] == 60


def test_asr_output_still_rejects_overlaps_after_boundary_normalization():
    with pytest.raises(ValidationError, match="non-overlapping"):
        normalize_asr_output(
            {
                "text": "重叠",
                "segments": [
                    {"start": -0.2, "end": 1.5, "text": "第一段"},
                    {"start": 1.4, "end": 2.0, "text": "第二段"},
                ],
            },
            window_index=0,
            start_seconds=0,
            end_seconds=30,
        )


def test_corrected_transcript_can_change_text_but_not_timeline_or_ids():
    source = [{"id": "asr-000-000", "startSeconds": 0.2, "endSeconds": 1.2, "text": "张山来了"}]
    corrected = validate_corrected_transcript(
        {
            "segments": [
                {
                    "id": "asr-000-000",
                    "startSeconds": 0.2,
                    "endSeconds": 1.2,
                    "text": "张三来了",
                }
            ]
        },
        source,
    )
    assert corrected[0]["text"] == "张三来了"

    changed_time = {"segments": [{**source[0], "endSeconds": 1.3}]}
    with pytest.raises(ValidationError, match="must preserve ASR timestamps"):
        validate_corrected_transcript(changed_time, source)


def test_story_analysis_validates_timeline_and_normalizes_fields():
    value = {
        "summary": " 山谷中的一天 ",
        "characters": [{"name": " 鹿 ", "role": "主体", "evidence": ["画面可见"]}],
        "scenes": [
            {
                "startSeconds": 0,
                "endSeconds": 10,
                "summary": "清晨",
                "visualFacts": ["晨雾"],
                "dialogueFacts": [],
                "uncertainties": [],
            }
        ],
        "facts": ["晨雾"],
        "uncertainties": [],
    }
    result = validate_story_analysis(value, duration_seconds=10)
    assert result["summary"] == "山谷中的一天"
    assert result["characters"][0]["name"] == "鹿"

    value["scenes"][0]["endSeconds"] = 11
    with pytest.raises(ValidationError, match="source timeline"):
        validate_story_analysis(value, duration_seconds=10)


def test_story_analysis_uses_same_millisecond_precision_as_model_prompt():
    value = {
        "summary": "片尾",
        "characters": [],
        "scenes": [
            {
                "startSeconds": 39.0,
                "endSeconds": 40.292,
                "summary": "结束",
                "visualFacts": [],
                "dialogueFacts": [],
                "uncertainties": [],
            }
        ],
        "facts": [],
        "uncertainties": [],
    }
    result = validate_story_analysis(value, duration_seconds=40.291666)
    assert result["scenes"][0]["endSeconds"] == 40.292


def test_narration_draft_requires_stable_ids_and_known_evidence_windows():
    valid = {
        "segments": [
            {
                "id": "draft-001",
                "narration": "晨雾从山谷间散去。",
                "evidenceWindowIndexes": [0, 1],
            }
        ]
    }
    assert validate_narration_draft(valid, window_count=2) == valid

    invalid_id = {"segments": [{**valid["segments"][0], "id": "draft-002"}]}
    with pytest.raises(ValidationError, match="draft-001"):
        validate_narration_draft(invalid_id, window_count=2)

    unknown_window = {"segments": [{**valid["segments"][0], "evidenceWindowIndexes": [2]}]}
    with pytest.raises(ValidationError, match="unknown window"):
        validate_narration_draft(unknown_window, window_count=2)
