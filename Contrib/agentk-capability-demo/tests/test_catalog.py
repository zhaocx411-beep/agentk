from agentk.api.v1.profile_pb import Profile

from agentk_capability_demo.catalog import (
    DEFAULT_CHAT_MODEL,
    GEMINI_CREATIVE_MODEL,
    profile_for,
    public_agents,
)
from agentk_capability_demo.serialization import proto_from_dict


def test_catalog_contains_all_comic_drama_cases():
    ids = {item["id"] for item in public_agents()}
    assert ids == {
        "assistant",
        "gemini-creative",
        "comic-drama",
        "creative-studio",
        "media-creator",
        "web-research",
        "document-research",
        "video-understanding",
        "audio",
        "video-editor",
    }


def test_every_catalog_profile_matches_sdk_protocol():
    for item in public_agents():
        profile = proto_from_dict(Profile, profile_for(item["id"]))
        expected = (
            GEMINI_CREATIVE_MODEL
            if item["id"] == "gemini-creative"
            else DEFAULT_CHAT_MODEL
        )
        assert profile.default_model == expected
        assert profile.system
        assert profile.tools


def test_multi_model_tools_have_control_plane_bindings():
    for item in public_agents():
        profile = profile_for(item["id"])
        enabled = {tool["name"] for tool in profile["tools"]}
        for name in {"generate_image", "edit_image", "understand_media", "web_search"} & enabled:
            assert profile["toolDefaults"][name]["model"]


def test_assistant_does_not_require_unregistered_call_agent_capability():
    profile = profile_for("assistant")

    assert {tool["name"] for tool in profile["tools"]} == {
        "list_artifacts",
        "ask_user",
    }
    assert all(
        tool.get("capability") != "contrib.studio.call_agent"
        for tool in profile["tools"]
    )
    assert "call_agent" not in profile["system"]


def test_gemini_creative_profile_uses_native_visual_context_without_understand_media():
    profile = profile_for("gemini-creative")
    tools = {tool["name"]: tool.get("capability") for tool in profile["tools"]}

    assert profile["defaultModel"] == GEMINI_CREATIVE_MODEL
    assert profile["maxImagesPerRound"] == 8
    assert "understand_media" not in tools
    assert tools["list_artifacts"] is None
    assert tools["ask_user"] is None
    assert tools["view_images"] is None
    assert tools["kling_text_to_video"] == "agentk.kling_text_to_video"
    assert tools["probe_video"] == "agentk.probe_video"
    assert tools["extract_frame"] == "agentk.extract_frame"
    assert profile["toolDefaults"]["kling_text_to_video"] == {
        "model": "kling-3.0-t2v"
    }
    assert profile["toolDefaults"]["edit_image"] == {
        "model": "gemini-3-pro-image-edit"
    }
    assert profile["toolDefaults"]["kling_image_to_video"] == {
        "model": "kling-3.0"
    }


def test_document_research_supports_files_and_multimedia():
    profile = profile_for("document-research")
    tools = {(tool["name"], tool.get("capability")) for tool in profile["tools"]}

    assert ("read_file", "agentk.read_file") in tools
    assert ("understand_media", "agentk.understand_media") in tools
    assert "图片、音频和视频使用 understand_media" in profile["system"]
    assert profile["toolDefaults"]["understand_media"] == {
        "model": "gemini-3.1-flash-lite"
    }
