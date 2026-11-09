import pytest
from jsonschema import validate

from agentk_capability_demo.agent100.narrato.contracts import (
    CREATE_INPUT_SCHEMA,
    GENERATE_INPUT_SCHEMA,
    ValidationError,
    excessive_narration_gaps,
    normalize_create_input,
    resolve_style,
    style_options,
)
from agentk_capability_demo.agent100.narrato.contracts.models import (
    normalize_generate_input,
)
from agentk_capability_demo.agent100.narrato.contracts.styles import (
    DOCUMENTARY_STYLE,
    RECAP_STYLE,
)
from agentk_capability_demo.agent100.narrato.prompts import (
    analysis_prompt,
    documentary_script_prompt,
    narrative_plan_prompt,
    pacing_revision_prompt,
)

VIDEO_URI = "https://cdn.example/source.mp4"
TTS = {"provider": "qwen", "model": "qwen3-tts-flash", "voiceId": "Cherry"}


def _script(cues):
    return {
        "sourceDurationSeconds": 60.0,
        "narrationCues": [{"startSeconds": start, "endSeconds": end} for start, end in cues],
    }


def test_resolve_style_defaults_to_documentary():
    assert resolve_style(None).id == "documentary"
    assert resolve_style("").id == "documentary"
    assert resolve_style("recap").id == "recap"


def test_resolve_style_rejects_unknown_id():
    with pytest.raises(ValidationError):
        resolve_style("cinematic")


def test_generate_input_carries_style():
    assert normalize_generate_input({"videoUri": VIDEO_URI})["style"] == "documentary"
    assert normalize_generate_input({"videoUri": VIDEO_URI, "style": "recap"})["style"] == "recap"


def test_input_schemas_accept_style():
    validate({"videoUri": VIDEO_URI, "style": "recap"}, GENERATE_INPUT_SCHEMA)
    validate({"videoUri": VIDEO_URI, "style": "recap", "tts": TTS}, CREATE_INPUT_SCHEMA)


def test_style_supplies_original_audio_default():
    recap = normalize_create_input({"videoUri": VIDEO_URI, "style": "recap", "tts": TTS})
    assert recap["originalAudioVolume"] == RECAP_STYLE.default_original_audio_volume

    documentary = normalize_create_input({"videoUri": VIDEO_URI, "tts": TTS})
    assert documentary["originalAudioVolume"] == DOCUMENTARY_STYLE.default_original_audio_volume


def test_explicit_original_audio_overrides_style_default():
    result = normalize_create_input(
        {"videoUri": VIDEO_URI, "style": "recap", "tts": TTS, "originalAudioVolume": 0.5}
    )
    assert result["originalAudioVolume"] == 0.5


def test_dialogue_suppresses_gaps_only_for_documentary():
    """原片对白是否顶替解说，是纪录片与影视解说最关键的分歧。"""

    script = _script([(0.0, 4.0)])
    dialogue = [{"startSeconds": 4.0, "endSeconds": 60.0, "text": "一整段连续对白"}]

    assert (
        excessive_narration_gaps(
            script, dialogue, threshold_seconds=5.0, dialogue_counts_as_coverage=True
        )
        == []
    )

    recap_gaps = excessive_narration_gaps(
        script, dialogue, threshold_seconds=2.0, dialogue_counts_as_coverage=False
    )
    assert len(recap_gaps) == 1
    assert recap_gaps[0]["startSeconds"] == 4.0
    assert recap_gaps[0]["endSeconds"] == 60.0


def test_recap_prompts_invert_the_spoiler_and_dialogue_policies():
    plan = {"hook": "h", "storyBeats": [{"evidenceWindowIndexes": [0]}]}
    windows = [{"index": 0, "startSeconds": 0.0, "endSeconds": 30.0}]

    recap = documentary_script_prompt(
        video_uri=VIDEO_URI,
        duration_seconds=120.0,
        narrative_plan=plan,
        evidence_windows=windows,
        research_results=[],
        instructions="",
        style=RECAP_STYLE,
    )
    assert "不需要回避剧透" in recap
    assert "不需要为了避让人物对白而留空" in recap
    # 字数预算必须出现，否则模型会写超导致末尾 cue 在渲染时被丢弃。
    budget = int(120.0 * RECAP_STYLE.coverage_high * RECAP_STYLE.speech_rate_low)
    assert f"{budget}字以内" in recap

    documentary = documentary_script_prompt(
        video_uri=VIDEO_URI,
        duration_seconds=120.0,
        narrative_plan=plan,
        evidence_windows=windows,
        research_results=[],
        instructions="",
        style=DOCUMENTARY_STYLE,
    )
    assert "严格按事件发生后的视角安排解说" in documentary
    assert "不需要回避剧透" not in documentary


def test_recap_plan_and_analysis_prompts_target_plot_facts():
    plan = narrative_plan_prompt(
        evidence_windows=[{"index": 0, "startSeconds": 0.0, "endSeconds": 30.0}],
        duration_seconds=120.0,
        instructions="",
        style=RECAP_STYLE,
    )
    assert "你是影视解说策划" in plan
    assert RECAP_STYLE.coverage_percent_text() in plan

    analysis = analysis_prompt(
        start_seconds=0.0, end_seconds=30.0, instructions="", style=RECAP_STYLE
    )
    assert "为影视解说提供事实素材" in analysis
    assert "因果" in analysis


def test_recap_revision_prompt_asks_for_density_not_restraint():
    revision = pacing_revision_prompt(
        "原始提示",
        {"narrationCues": []},
        [{"startSeconds": 4.0, "endSeconds": 60.0, "durationSeconds": 56.0}],
        style=RECAP_STYLE,
    )
    assert "解说密度不够" in revision
    assert "不需要为对白让路" in revision


def test_style_options_expose_render_defaults_for_the_form():
    options = {item["id"]: item for item in style_options()}
    assert set(options) == {"documentary", "recap"}
    assert options["recap"]["defaultOriginalAudioVolume"] == 0.08
    assert options["recap"]["displayName"]
    assert options["recap"]["description"]
