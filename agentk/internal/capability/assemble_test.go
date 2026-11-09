package capability

import (
	"slices"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/workflow"
)

type registrationRecorder struct {
	activities []string
	workflows  []string
}

func (r *registrationRecorder) RegisterActivityWithOptions(
	_ interface{},
	options activity.RegisterOptions,
) {
	r.activities = append(r.activities, options.Name)
}

func (r *registrationRecorder) RegisterWorkflowWithOptions(
	_ interface{},
	options workflow.RegisterOptions,
) {
	r.workflows = append(r.workflows, options.Name)
}

func TestRegisterAllDoesNotRequireConfiguredModels(t *testing.T) {
	recorder := &registrationRecorder{}
	RegisterAll(recorder, nil, nil)

	if len(recorder.activities) != 25 {
		t.Fatalf("activities = %v", recorder.activities)
	}
	for _, name := range []string{
		"agentk.capability.generate_text",
		"agentk.capability.generate_video",
		"agentk.capability.generate_image",
		"agentk.capability.edit_image",
		"agentk.capability.understand_media",
		"agentk.capability.understand_video_segment",
		"agentk.capability.web_search",
		"agentk.capability.fetch_product_detail",
		"agentk.capability.generate_speech_minimax",
		"agentk.capability.generate_speech_qwen",
		"agentk.capability.generate_speech_cosyvoice",
		"agentk.capability.transcribe_audio",
		"agentk.capability.read_file",
		"agentk.capability.kling_text_to_video_execute",
		"agentk.capability.kling_image_to_video_execute",
		"agentk.capability.kling_omni_video_execute",
		"agentk.capability.seedance_generate_video_execute",
	} {
		if !slices.Contains(recorder.activities, name) {
			t.Fatalf("activity %q was not registered: %v", name, recorder.activities)
		}
	}
	for _, name := range []string{
		"agentk.capability.generate_brand_ad",
		"agentk.capability.kling_text_to_video",
		"agentk.capability.kling_image_to_video",
		"agentk.capability.kling_omni_video",
		"agentk.capability.seedance_generate_video",
	} {
		if !slices.Contains(recorder.workflows, name) {
			t.Fatalf("workflow %q was not registered: %v", name, recorder.workflows)
		}
	}
}
