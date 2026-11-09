import pytest

from narrato_agentk.agentk_core import (
    CORE_CAPABILITY_IDS,
    DISCOVERABLE_CORE_CAPABILITY_IDS,
    first_artifact_uri,
    text_result,
)


def test_core_contract_contains_every_required_capability():
    assert CORE_CAPABILITY_IDS == {
        "agentk.probe_video",
        "agentk.understand_video_segment",
        "agentk.generate_text",
        "agentk.generate_speech_qwen",
        "agentk.process_video",
        "agentk.compose_video",
        "agentk.concat_videos",
    }
    assert DISCOVERABLE_CORE_CAPABILITY_IDS == CORE_CAPABILITY_IDS - {"agentk.concat_videos"}


def test_first_artifact_uri_requires_real_uri():
    assert (
        first_artifact_uri({"artifacts": [{"uri": "https://cdn/a.mp4"}]}, "op")
        == "https://cdn/a.mp4"
    )
    with pytest.raises(ValueError, match="no artifact"):
        first_artifact_uri({"artifacts": []}, "op")


def test_text_result_accepts_structured_or_json_string():
    assert text_result({"result": {"ok": True}}) == {"ok": True}
    assert text_result({"result": '{"ok":true}'}) == {"ok": True}
    with pytest.raises(ValueError, match="valid JSON"):
        text_result({"result": "not-json"})
