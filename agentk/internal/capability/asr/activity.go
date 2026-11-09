package asr

import (
	"context"
	"fmt"

	"agentk/internal/temporalx"
)

const ActivityName = "agentk.capability.transcribe_audio"

type CapabilityInput struct {
	Model    string `json:"model"`
	AudioURL string `json:"audioUrl"`
	Task     string `json:"task,omitempty"`
	Language string `json:"language,omitempty"`
	Prompt   string `json:"prompt,omitempty"`
}

type CapabilityOutput struct {
	Text     string         `json:"text"`
	Language string         `json:"language,omitempty"`
	Duration float64        `json:"duration,omitempty"`
	Segments []Segment      `json:"segments,omitempty"`
	Model    string         `json:"model"`
	Task     string         `json:"task"`
	Params   map[string]any `json:"params,omitempty"`
	Metadata Metadata       `json:"metadata,omitempty"`
}

type ModelLookup interface {
	ASRClient(name string) (Client, bool)
}

type Activity struct {
	models ModelLookup
}

func NewActivity(models ModelLookup) *Activity {
	return &Activity{models: models}
}

func (a *Activity) Run(ctx context.Context, in CapabilityInput) (CapabilityOutput, error) {
	const op = "capability transcribe_audio"
	if a.models == nil {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("asr model lookup is required")))
	}
	if in.Model == "" {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("model is required"))
	}
	req, err := normalizeRequest(Request{
		AudioURL: in.AudioURL, Task: in.Task, Language: in.Language, Prompt: in.Prompt,
	})
	if err != nil {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("%v", err))
	}
	client, ok := a.models.ASRClient(in.Model)
	if !ok {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("asr capability %q is not available", in.Model)))
	}
	result, err := client.Transcribe(ctx, req)
	if err != nil {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op+": transcribe", err)
	}
	return CapabilityOutput{
		Text: result.Text, Language: result.Language, Duration: result.Duration,
		Segments: result.Segments, Model: in.Model, Task: req.Task,
		Params: result.Params, Metadata: result.Metadata,
	}, nil
}
