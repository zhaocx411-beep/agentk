from agentk_capability_demo.voice_options import (
    capability_is_public,
    enrich_capability_schema,
    enrich_capability_voice_options,
)


def _capability(
    capability_id: str, source: str = "CAPABILITY_SOURCE_CORE"
) -> dict:
    return {
        "id": capability_id,
        "source": source,
        "inputSchema": {
            "type": "object",
            "required": ["voiceId"],
            "properties": {"voiceId": {"type": "string"}},
        },
    }


def test_qwen_and_minimax_voice_ids_are_enriched_from_agentk_provider_defaults():
    qwen = enrich_capability_voice_options(_capability("agentk.generate_speech_qwen"))
    minimax = enrich_capability_voice_options(_capability("agentk.generate_speech_minimax"))

    assert qwen["inputSchema"]["properties"]["voiceId"]["default"] == "Cherry"
    assert qwen["inputSchema"]["properties"]["voiceId"]["x-options"][0] == {
        "value": "Cherry",
        "label": "芊晓：阳光亲切女声（Cherry）",
    }
    assert minimax["inputSchema"]["properties"]["voiceId"]["default"] == (
        "moss_audio_ce44fc67-7ce3-11f0-8de5-96e35d26fb85"
    )
    assert any(
        option["value"] == "Cantonese_podcast_host_1"
        for option in minimax["inputSchema"]["properties"]["voiceId"]["x-options"]
    )


def test_non_public_capabilities_are_filtered_from_the_catalog():
    assert capability_is_public(_capability("agentk.generate_speech_cosyvoice")) is False
    assert capability_is_public(_capability("agentk.generate_brand_ad")) is False
    assert (
        capability_is_public(
            _capability(
                "contrib.example.capability",
                source="CAPABILITY_SOURCE_CONTRIB",
            )
        )
        is False
    )
    assert capability_is_public(_capability("agentk.generate_speech_qwen")) is True


def test_unrelated_capabilities_and_missing_voice_fields_are_unchanged():
    unrelated = _capability("agentk.generate_text")
    assert enrich_capability_voice_options(unrelated) is unrelated

    missing = {"id": "agentk.generate_speech_qwen", "inputSchema": {"properties": {}}}
    assert enrich_capability_voice_options(missing) is missing


def test_generate_video_duration_contract_is_exposed_to_the_form():
    capability = {
        "id": "agentk.generate_video",
        "inputSchema": {
            "type": "object",
            "properties": {
                "duration": {"type": "string"},
                "resolution": {"type": "string"},
                "audio": {"type": "string"},
            },
        },
    }

    enriched = enrich_capability_schema(capability)

    duration = enriched["inputSchema"]["properties"]["duration"]
    assert duration["enum"] == [str(value) for value in range(4, 16)]
    assert duration["default"] == "5"
    assert "manifest" in duration["x-contract-warning"]
    assert enriched["inputSchema"]["properties"]["resolution"]["enum"] == [
        "720p",
        "1080p",
        "4k",
    ]
    assert enriched["inputSchema"]["properties"]["audio"]["enum"] == ["off", "native"]
    assert capability["inputSchema"]["properties"]["duration"] == {"type": "string"}
