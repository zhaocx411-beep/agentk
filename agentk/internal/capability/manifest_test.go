package capability

import (
	"testing"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
)

func TestCoreManifest(t *testing.T) {
	manifest, err := CoreManifest()
	if err != nil {
		t.Fatal(err)
	}
	type expectedCapability struct {
		id           string
		temporalType string
		tool         string
		mode         sdkcapability.ToolMode
	}
	want := []expectedCapability{
		{"agentk.compose_video", "agentk.capability.compose_video", "compose_video", sdkcapability.ToolModeBlocking},
		{"agentk.edit_image", "agentk.capability.edit_image", "edit_image", sdkcapability.ToolModeBlocking},
		{"agentk.extract_audio", "agentk.capability.extract_audio", "extract_audio", sdkcapability.ToolModeBlocking},
		{"agentk.extract_frame", "agentk.capability.extract_frame", "extract_frame", sdkcapability.ToolModeBlocking},
		{"agentk.fetch_product_detail", "agentk.capability.fetch_product_detail", "fetch_product_detail", sdkcapability.ToolModeBlocking},
		{"agentk.generate_brand_ad", "agentk.capability.generate_brand_ad", "generate_brand_ad", sdkcapability.ToolModeBlocking},
		{"agentk.generate_image", "agentk.capability.generate_image", "generate_image", sdkcapability.ToolModeBlocking},
		{"agentk.generate_speech_cosyvoice", "agentk.capability.generate_speech_cosyvoice", "generate_speech_cosyvoice", sdkcapability.ToolModeBlocking},
		{"agentk.generate_speech_minimax", "agentk.capability.generate_speech_minimax", "generate_speech_minimax", sdkcapability.ToolModeBlocking},
		{"agentk.generate_speech_qwen", "agentk.capability.generate_speech_qwen", "generate_speech_qwen", sdkcapability.ToolModeBlocking},
		{"agentk.generate_text", "agentk.capability.generate_text", "", ""},
		{"agentk.generate_video", "agentk.capability.generate_video", "", ""},
		{"agentk.kling_image_to_video", "agentk.capability.kling_image_to_video", "kling_image_to_video", sdkcapability.ToolModeBlocking},
		{"agentk.kling_omni_video", "agentk.capability.kling_omni_video", "kling_omni_video", sdkcapability.ToolModeBlocking},
		{"agentk.kling_text_to_video", "agentk.capability.kling_text_to_video", "kling_text_to_video", sdkcapability.ToolModeBlocking},
		{"agentk.probe_video", "agentk.capability.probe_video", "probe_video", sdkcapability.ToolModeBlocking},
		{"agentk.process_video", "agentk.capability.process_video", "process_video", sdkcapability.ToolModeBlocking},
		{"agentk.read_file", "agentk.capability.read_file", "read_file", sdkcapability.ToolModeBlocking},
		{"agentk.seedance_generate_video", "agentk.capability.seedance_generate_video", "seedance_generate_video", sdkcapability.ToolModeBlocking},
		{"agentk.transcribe_audio", "agentk.capability.transcribe_audio", "transcribe_audio", sdkcapability.ToolModeBlocking},
		{"agentk.understand_media", "agentk.capability.understand_media", "understand_media", sdkcapability.ToolModeBlocking},
		{"agentk.understand_video_segment", "agentk.capability.understand_video_segment", "understand_video_segment", sdkcapability.ToolModeBlocking},
		{"agentk.web_search", "agentk.capability.web_search", "web_search", sdkcapability.ToolModeBlocking},
	}
	if manifest.TaskQueue != coreTaskQueue || len(manifest.Capabilities) != len(want) {
		t.Fatalf("manifest = %+v", manifest)
	}
	workflowCapabilities := map[string]bool{
		"agentk.generate_brand_ad":       true,
		"agentk.kling_image_to_video":    true,
		"agentk.kling_omni_video":        true,
		"agentk.kling_text_to_video":     true,
		"agentk.seedance_generate_video": true,
	}
	for i, expected := range want {
		def := manifest.Capabilities[i]
		if def.ID != expected.id || def.TemporalType != expected.temporalType {
			t.Fatalf("capability[%d] = %+v, want id=%q type=%q",
				i, def, expected.id, expected.temporalType)
		}
		if len(def.InputSchema) == 0 || len(def.OutputSchema) == 0 ||
			def.TimeoutSeconds <= 0 {
			t.Fatalf("capability %q incomplete: %+v", expected.id, def)
		}
		if expected.tool == "" {
			if def.Tool != nil {
				t.Fatalf("capability %q unexpectedly declares tool %+v", expected.id, def.Tool)
			}
		} else if def.Tool == nil || def.Tool.Mode != expected.mode {
			t.Fatalf("capability %q tool = %+v", expected.id, def.Tool)
		}
		wantKind := sdkcapability.CapabilityKindActivity
		if workflowCapabilities[def.ID] {
			wantKind = sdkcapability.CapabilityKindWorkflow
		}
		if def.Kind != wantKind {
			t.Fatalf("capability %q kind = %q, want %q", def.ID, def.Kind, wantKind)
		}
	}
}
