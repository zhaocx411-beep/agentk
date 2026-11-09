from __future__ import annotations

import pytest
from jsonschema import ValidationError as JsonSchemaValidationError
from jsonschema import validate

from agentk_capability_demo.agent100.narrato.contracts import (
    CREATE_INPUT_SCHEMA,
    DOCUMENTARY_SCRIPT_SCHEMA,
    ValidationError,
    excessive_narration_gaps,
    normalize_create_input,
    opening_hook_gap,
    pacing_warning,
    validate_documentary_script,
    validate_narrative_plan,
)
from agentk_capability_demo.agent100.narrato.prompts import (
    documentary_script_prompt,
    narrative_plan_prompt,
    pacing_revision_prompt,
)


def profile() -> dict:
    return {
        "primaryGenre": "科幻片",
        "secondaryGenres": ["动作片"],
        "thesis": "未知技术迫使人物作出选择",
        "hook": "那道门打开后，规则彻底变了。",
        "style": "抓重点、短句、逐步揭示",
        "pacing": "中快，关键揭示前留停顿",
        "globalTtsInstruction": "沉稳、有悬念，关键处稍作停顿",
    }


def script(*, cues: list[dict] | None = None, duration: float = 40) -> dict:
    return {
        "version": "2",
        "sourceVideoUri": "https://cdn.test/source.mp4",
        "sourceDurationSeconds": duration,
        "narrativeProfile": profile(),
        "narrationCues": cues
        or [
            {
                "id": "cue-001",
                "startSeconds": 2,
                "endSeconds": 8,
                "narration": "门后的世界，正在改写他们熟悉的规则。",
                "evidenceWindowIndexes": [0],
                "ttsInstruction": "压低声音，末尾留下悬念",
                "importance": 5,
                "emotion": "surprised",
            }
        ],
    }


def test_v2_contract_removes_theme_and_uses_provider_tts_object():
    value = script()
    validate(value, DOCUMENTARY_SCRIPT_SCHEMA)
    validate(
        {
            "videoUri": "https://cdn.test/source.mp4",
            "tts": {"provider": "qwen", "model": "qwen3-tts-flash", "voiceId": "Cherry"},
        },
        CREATE_INPUT_SCHEMA,
    )

    legacy = {**value, "version": "1", "theme": "科幻"}
    with pytest.raises(JsonSchemaValidationError):
        validate(legacy, DOCUMENTARY_SCRIPT_SCHEMA)
    with pytest.raises(ValidationError, match="theme"):
        normalize_create_input(
            {
                "videoUri": "https://cdn.test/source.mp4",
                "theme": "科幻",
                "tts": {
                    "provider": "qwen",
                    "model": "qwen3-tts-flash",
                    "voiceId": "Cherry",
                },
            }
        )


def test_v2_script_validation_keeps_soft_pacing_out_of_hard_contract():
    sparse = script(duration=120)
    normalized = validate_documentary_script(sparse, window_count=4)
    assert normalized["version"] == "2"
    assert normalized["narrationCues"][0]["id"] == "cue-001"


def test_narrative_plan_validates_genre_strategy_and_cross_window_beats():
    plan = {
        **profile(),
        "researchNeeded": False,
        "researchReason": "原创内容",
        "researchQueries": [],
        "storyBeats": [
            {
                "id": "beat-001",
                "startSeconds": 0,
                "endSeconds": 40,
                "summary": "人物发现异常并尝试进入",
                "narrationPurpose": "建立规则与核心冲突",
                "importance": 5,
                "evidenceWindowIndexes": [0, 1],
            }
        ],
    }
    result = validate_narrative_plan(plan, duration_seconds=40, window_count=2)
    assert result["primaryGenre"] == "科幻片"
    assert result["storyBeats"][0]["evidenceWindowIndexes"] == [0, 1]


def test_gap_over_five_seconds_without_dialogue_requests_pacing_revision():
    value = script(duration=40)
    gaps = excessive_narration_gaps(value, [])
    assert gaps == [{"startSeconds": 8.0, "endSeconds": 40.0, "durationSeconds": 32.0}]
    assert "8.000-40.000秒" in pacing_warning(gaps)

    assert "5秒" in pacing_warning(gaps)

    protected = [{"startSeconds": 8, "endSeconds": 40, "text": "连续人物对白"}]
    assert excessive_narration_gaps(value, protected) == []


@pytest.mark.parametrize(
    ("second_start", "expected"),
    [
        (7.0, []),
        (
            7.001,
            [
                {
                    "startSeconds": 2.0,
                    "endSeconds": 7.001,
                    "durationSeconds": 5.001,
                }
            ],
        ),
    ],
)
def test_adjacent_cue_gap_five_seconds_passes_but_five_point_zero_zero_one_fails(
    second_start,
    expected,
):
    value = script(
        duration=10,
        cues=[
            {"startSeconds": 0, "endSeconds": 2},
            {"startSeconds": second_start, "endSeconds": 10},
        ],
    )
    assert excessive_narration_gaps(value, []) == expected


def test_opening_without_dialogue_requests_hook_within_three_seconds():
    value = script(duration=15)
    value["narrationCues"][0]["startSeconds"] = 4
    value["narrationCues"][0]["endSeconds"] = 10

    assert opening_hook_gap(value) == {
        "startSeconds": 0.0,
        "endSeconds": 4.0,
        "durationSeconds": 4.0,
    }
    value["narrationCues"][0]["startSeconds"] = 3
    assert opening_hook_gap(value) is None


def test_prompts_auto_detect_genre_and_forbid_asr_one_to_one_narration():
    evidence = [{"index": 0, "startSeconds": 0, "endSeconds": 30}]
    plan_prompt = narrative_plan_prompt(
        evidence_windows=evidence,
        duration_seconds=30,
        instructions="真实、有吸引力",
    )
    assert "题材必须由证据自动判断" in plan_prompt
    assert all(name in plan_prompt for name in ("纪录片", "动作片", "科幻片", "爱情片"))

    plan = {
        **profile(),
        "researchNeeded": False,
        "researchReason": "",
        "researchQueries": [],
        "storyBeats": [],
    }
    final_prompt = documentary_script_prompt(
        video_uri="https://cdn.test/source.mp4",
        duration_seconds=30,
        narrative_plan=plan,
        evidence_windows=evidence,
        research_results=[],
        instructions="",
    )
    assert "不要把每一段ASR结果分别对应一段解说" in final_prompt
    assert "完整保留原片画面" in final_prompt
    assert "关键事实最晚何时已被观众确认" in plan_prompt
    assert "ASR" in plan_prompt and "endSeconds" in plan_prompt
    assert "不超过5秒" in plan_prompt
    assert "evidenceEndSeconds" in final_prompt
    assert "startSeconds >= evidenceEndSeconds" in final_prompt
    assert "segment.endSeconds" in final_prompt
    assert "不超过5秒" in final_prompt
    assert "定位 + 悬念" in plan_prompt
    assert "hook播放前已经可见或可听的信息" in plan_prompt
    assert "定位 + 悬念" in final_prompt
    assert "不得概括后续剧情" in final_prompt
    assert "时间正确性高于开场3秒" in final_prompt


def test_positioning_hook_guidance_can_be_replay_disabled():
    evidence = [{"index": 0, "startSeconds": 0, "endSeconds": 30}]
    plan = {
        **profile(),
        "researchNeeded": False,
        "researchReason": "",
        "researchQueries": [],
        "storyBeats": [],
    }
    plan_prompt = narrative_plan_prompt(
        evidence_windows=evidence,
        duration_seconds=30,
        instructions="",
        positioning_hook=False,
    )
    script_prompt = documentary_script_prompt(
        video_uri="https://cdn.test/source.mp4",
        duration_seconds=30,
        narrative_plan=plan,
        evidence_windows=evidence,
        research_results=[],
        instructions="",
        positioning_hook=False,
    )
    assert "定位 + 悬念" not in plan_prompt
    assert "定位 + 悬念" not in script_prompt


def test_legacy_prompts_keep_original_six_and_eighteen_second_guidance():
    evidence = [{"index": 0, "startSeconds": 0, "endSeconds": 30}]
    plan = {
        **profile(),
        "researchNeeded": False,
        "researchReason": "",
        "researchQueries": [],
        "storyBeats": [],
    }
    plan_prompt = narrative_plan_prompt(
        evidence_windows=evidence,
        duration_seconds=30,
        instructions="",
        opening_cadence=False,
        post_event_narration=False,
    )
    script_prompt = documentary_script_prompt(
        video_uri="https://cdn.test/source.mp4",
        duration_seconds=30,
        narrative_plan=plan,
        evidence_windows=evidence,
        research_results=[],
        instructions="",
        opening_cadence=False,
        post_event_narration=False,
    )
    revision_prompt = pacing_revision_prompt(
        script_prompt,
        script(duration=30),
        [{"startSeconds": 8, "endSeconds": 30, "durationSeconds": 22}],
        {"startSeconds": 0, "endSeconds": 4, "durationSeconds": 4},
        opening_cadence=False,
        post_event_narration=False,
    )

    assert "最长约6秒" in plan_prompt
    assert "最长约6秒" in script_prompt
    assert "0-3秒" not in plan_prompt + script_prompt
    assert "三倍（18秒）" in revision_prompt
    assert "openingHookGap" not in revision_prompt
