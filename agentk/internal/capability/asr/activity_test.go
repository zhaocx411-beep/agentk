package asr

import (
	"context"
	"errors"
	"testing"

	"go.temporal.io/sdk/temporal"
)

type stubClient struct {
	last   Request
	result Result
	err    error
}

func (s *stubClient) Transcribe(_ context.Context, req Request) (Result, error) {
	s.last = req
	return s.result, s.err
}

type stubCatalog map[string]Client

func (c stubCatalog) ASRClient(name string) (Client, bool) {
	client, ok := c[name]
	return client, ok
}

func TestActivity(t *testing.T) {
	client := &stubClient{result: Result{
		Text: "hello", Language: "english", Duration: 1.5,
		Segments: []Segment{{ID: 0, Start: 0, End: 1.5, Text: "hello"}},
		Params:   map[string]any{"mime": "audio/wav"},
		Metadata: Metadata{RequestID: "request-1"},
	}}
	activity := NewActivity(stubCatalog{"whisper": client})
	out, err := activity.Run(context.Background(), CapabilityInput{
		Model: "whisper", AudioURL: "https://audio.test/a.wav",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != "hello" || out.Model != "whisper" || out.Task != TaskTranscribe ||
		out.Metadata.RequestID != "request-1" {
		t.Fatalf("out = %+v", out)
	}
	if out.Language != "english" || out.Duration != 1.5 || len(out.Segments) != 1 {
		t.Fatalf("timeline = %+v", out)
	}
	if client.last.Task != TaskTranscribe {
		t.Fatalf("request = %+v", client.last)
	}
}

func TestActivityValidation(t *testing.T) {
	activity := NewActivity(stubCatalog{"whisper": &stubClient{}})
	valid := CapabilityInput{Model: "whisper", AudioURL: "https://audio.test/a.wav"}
	tests := map[string]func(*CapabilityInput){
		"model":     func(in *CapabilityInput) { in.Model = "" },
		"audio":     func(in *CapabilityInput) { in.AudioURL = "" },
		"task":      func(in *CapabilityInput) { in.Task = "invalid" },
		"translate": func(in *CapabilityInput) { in.Task, in.Language = TaskTranslate, "zh" },
		"unknown":   func(in *CapabilityInput) { in.Model = "unknown" },
	}
	for name, mutate := range tests {
		in := valid
		mutate(&in)
		_, err := activity.Run(context.Background(), in)
		var appErr *temporal.ApplicationError
		if !errors.As(err, &appErr) || !appErr.NonRetryable() {
			t.Fatalf("%s: err = %v, want non-retryable", name, err)
		}
	}
}
