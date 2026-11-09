package chat

import (
	"encoding/json"
	"reflect"
	"testing"

	"studio"
	"studio/platform/core"
)

func TestAllEmbeddedProfilesUseAgentKV02Schema(t *testing.T) {
	for _, agent := range studio.MustAgents() {
		for name, raw := range agent.Profiles {
			if _, err := core.DecodeAgentKProfile(raw); err != nil {
				t.Errorf("%s/%s.json: %v", agent.Meta.ID, name, err)
			}
		}
	}
}

func TestDefaultAgentRegistry(t *testing.T) {
	registry, err := NewRegistry(studio.MustAgents())
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	// PublicList 含全部条目（漫剧工坊在首位，kind=workshop；assistant 是
	// 自动路由入口，排在 chat 形态首位）；只有 chat 形态可以 Get 出来创建会话。
	want := []string{
		"comic-drama",
		"assistant",
		"creative-studio",
		"media-creator",
		"web-research",
		"document-research",
		"video-understanding",
		"audio",
		"video-editor",
	}
	public := registry.PublicList()
	if len(public) != len(want) {
		t.Fatalf("public agents = %d, want %d", len(public), len(want))
	}
	for index, id := range want {
		if public[index].ID != id {
			t.Fatalf("agent %d id = %q, want %q", index, public[index].ID, id)
		}
		if id == "comic-drama" {
			if public[index].Kind != "workshop" || public[index].Href != "/agents/comic-drama/" {
				t.Fatalf("comic-drama card = %+v", public[index])
			}
			continue
		}
		definition, ok := registry.Get(id)
		if !ok {
			t.Fatalf("agent %q is missing", id)
		}
		var profile map[string]json.RawMessage
		if err := json.Unmarshal(definition.Profile, &profile); err != nil {
			t.Fatalf("agent %q profile: %v", id, err)
		}
		if len(definition.StarterPrompts) < 3 {
			t.Fatalf("agent %q has too few starter prompts", id)
		}
	}
	// workshop 形态与已下线的 Agent 都不能创建聊天会话。
	for _, forbidden := range []string{"comic-drama", "short-drama", "basic-assistant"} {
		if _, ok := registry.Get(forbidden); ok {
			t.Fatalf("forbidden chat agent %q was registered", forbidden)
		}
	}
	// 漫剧可作为 converse 子 agent（profile-agent），对话入口自己不行。
	if _, ok := registry.SubAgent("comic-drama"); !ok {
		t.Fatal("comic-drama should be a callable sub-agent")
	}
	if _, ok := registry.SubAgent("assistant"); ok {
		t.Fatal("assistant must not be a sub-agent")
	}
}

func TestAgentDTODoesNotExposeProfile(t *testing.T) {
	registry := DefaultRegistry()
	raw, err := json.Marshal(registry.PublicList())
	if err != nil {
		t.Fatalf("marshal public agents: %v", err)
	}
	var values []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		t.Fatalf("decode public agents: %v", err)
	}
	for _, value := range values {
		if _, exists := value["profile"]; exists {
			t.Fatal("public agent exposed profile")
		}
		if _, exists := value["profileVersion"]; exists {
			t.Fatal("public agent exposed profile version")
		}
	}
}

func TestArchivedWebSearchProfilesKeepConfiguredToolSets(t *testing.T) {
	registry := DefaultRegistry()
	expected := map[string][]string{
		"creative-studio": {
			"kling_text_to_video", "kling_image_to_video",
			"kling_omni_video", "seedance_generate_video", "generate_image",
			"edit_image", "understand_media", "process_video", "compose_video",
			"extract_audio", "extract_frame",
			"probe_video", "list_artifacts", "generate_speech",
			"transcribe_audio", "web_search", "web_fetch", "fetch_product_detail",
			"ask_user",
		},
		"media-creator": {
			"kling_text_to_video", "kling_image_to_video",
			"kling_omni_video", "seedance_generate_video", "generate_image",
			"edit_image", "understand_media", "process_video",
			"compose_video", "extract_audio", "extract_frame", "probe_video",
			"list_artifacts", "ask_user",
		},
		"web-research": {
			"web_search", "web_fetch",
			"fetch_product_detail", "ask_user",
		},
		"document-research": {
			"read_file", "list_artifacts", "web_search", "web_fetch",
			"fetch_product_detail", "ask_user",
		},
		"video-understanding": {
			"probe_video", "understand_media", "understand_video_segment",
			"list_artifacts", "ask_user",
		},
		"audio": {
			"generate_speech_qwen", "generate_speech_minimax",
			"transcribe_audio", "list_artifacts", "ask_user",
		},
		"video-editor": {
			"generate_image", "edit_image", "kling_text_to_video",
			"kling_image_to_video", "kling_omni_video", "seedance_generate_video",
			"understand_media", "understand_video_segment", "generate_speech",
			"transcribe_audio", "probe_video", "process_video", "compose_video",
			"extract_audio", "extract_frame",
			"list_artifacts", "ask_user",
		},
	}
	for agentID, want := range expected {
		definition, _ := registry.Get(agentID)
		var profile struct {
			DefaultModel string `json:"defaultModel"`
			System       string `json:"system"`
			Tools        []struct {
				Name       string `json:"name"`
				Capability string `json:"capability"`
			} `json:"tools"`
			ToolDefaults map[string]map[string]any `json:"toolDefaults"`
			Pipelines    []map[string]any          `json:"pipelines"`
		}
		if err := json.Unmarshal(definition.Profile, &profile); err != nil {
			t.Fatalf("decode %s: %v", agentID, err)
		}
		if profile.DefaultModel != "deepseek-v4-pro" {
			t.Fatalf("%s defaultModel = %q, want deepseek-v4-pro",
				agentID, profile.DefaultModel)
		}
		got := make([]string, len(profile.Tools))
		for index, tool := range profile.Tools {
			got[index] = tool.Name
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s tools = %v, want %v", agentID, got, want)
		}
		for _, toolName := range got {
			if toolName != "understand_media" && toolName != "understand_video_segment" {
				continue
			}
			if profile.ToolDefaults[toolName]["model"] != "gemini-3.1-flash-lite" {
				t.Fatalf("%s %s model = %v, want gemini-3.1-flash-lite",
					agentID, toolName, profile.ToolDefaults[toolName]["model"])
			}
		}
		if agentID == "creative-studio" {
			if len(profile.Pipelines) != 0 {
				t.Fatalf("full pipelines = %+v, want none", profile.Pipelines)
			}
			if profile.ToolDefaults["fetch_product_detail"]["maxImages"] != float64(6) {
				t.Fatal("full template lost fetch_product_detail maxImages")
			}
		}
		if agentID == "web-research" {
			if profile.ToolDefaults["fetch_product_detail"]["maxImages"] != float64(6) {
				t.Fatal("search template lost fetch_product_detail maxImages")
			}
		}
		if agentID == "document-research" {
			if profile.ToolDefaults["read_file"]["mode"] != "auto" ||
				profile.ToolDefaults["read_file"]["model"] != "gemini-3.1-flash-lite" {
				t.Fatalf("document read_file defaults = %v",
					profile.ToolDefaults["read_file"])
			}
		}
		for _, tool := range profile.Tools {
			if wantCapability, ok := coreToolCapabilities[tool.Name]; ok &&
				tool.Capability != wantCapability {
				t.Fatalf("%s tool %s capability = %q, want %q",
					agentID, tool.Name, tool.Capability, wantCapability)
			}
		}
	}
}

var coreToolCapabilities = map[string]string{
	"generate_image":           "agentk.generate_image",
	"edit_image":               "agentk.edit_image",
	"understand_media":         "agentk.understand_media",
	"understand_video_segment": "agentk.understand_video_segment",
	"process_video":            "agentk.process_video",
	"compose_video":            "agentk.compose_video",
	"extract_audio":            "agentk.extract_audio",
	"extract_frame":            "agentk.extract_frame",
	"probe_video":              "agentk.probe_video",
	"generate_speech":          "agentk.generate_speech_qwen",
	"generate_speech_qwen":     "agentk.generate_speech_qwen",
	"generate_speech_minimax":  "agentk.generate_speech_minimax",
	"transcribe_audio":         "agentk.transcribe_audio",
	"web_search":               "agentk.web_search",
	"read_file":                "agentk.read_file",
	"fetch_product_detail":     "agentk.fetch_product_detail",
}
