import pytest

from agentk_capability_demo.agent100.narrato.agentk_core import (
    CORE_CAPABILITY_IDS,
    DISCOVERABLE_CORE_CAPABILITY_IDS,
    CoreTemporalTypes,
    first_artifact_uri,
    text_result,
)


def test_core_contract_contains_every_required_capability():
    assert CORE_CAPABILITY_IDS == {
        "agentk.probe_video",
        "agentk.extract_audio",
        "agentk.transcribe_audio",
        "agentk.web_search",
        "agentk.understand_video_segment",
        "agentk.generate_text",
        "agentk.generate_speech_qwen",
        "agentk.generate_speech_minimax",
        "agentk.process_video",
        "agentk.compose_video",
        "agentk.concat_videos",
    }
    assert DISCOVERABLE_CORE_CAPABILITY_IDS == CORE_CAPABILITY_IDS - {"agentk.concat_videos"}


def test_core_capability_ids_are_distinct_from_temporal_abi_names():
    temporal_types = {
        value
        for name, value in vars(CoreTemporalTypes).items()
        if name.isupper() and isinstance(value, str)
    }
    assert {name.replace("agentk.capability.", "agentk.") for name in temporal_types} == (
        CORE_CAPABILITY_IDS
    )
    assert all(name.startswith("agentk.capability.") for name in temporal_types)
    assert CORE_CAPABILITY_IDS.isdisjoint(temporal_types)


def test_first_artifact_uri_requires_real_uri():
    assert (
        first_artifact_uri({"artifacts": [{"uri": "https://cdn/a.mp4"}]}, "op")
        == "https://cdn/a.mp4"
    )
    with pytest.raises(ValueError, match="no artifact"):
        first_artifact_uri({"artifacts": []}, "op")


def test_first_artifact_uri_rejects_wrong_artifact_type():
    with pytest.raises(ValueError, match="non-audio"):
        first_artifact_uri(
            {"artifacts": [{"type": "video", "uri": "https://cdn/a.mp4"}]},
            "tts",
            expected_type="audio",
        )


def test_text_result_accepts_structured_or_json_string():
    assert text_result({"result": {"ok": True}}) == {"ok": True}
    assert text_result({"result": '{"ok":true}'}) == {"ok": True}
    with pytest.raises(ValueError, match="valid JSON"):
        text_result({"result": "not-json"})
