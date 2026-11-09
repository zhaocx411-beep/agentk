package tts

import (
	"context"
	"strings"
	"testing"
)

func TestRestrictVoices(t *testing.T) {
	base := &stubTTSClient{audio: Audio{Data: []byte("audio"), MIME: "audio/wav"}}
	client := RestrictVoices(base, []VoiceOption{{ID: "allowed"}})

	if _, err := client.Synthesize(context.Background(), Request{
		Text: "hello", VoiceID: "other",
	}); err == nil || !strings.Contains(err.Error(), "not configured for the selected model") {
		t.Fatalf("wrong-model voice should be rejected, got %v", err)
	}
	if _, err := client.Synthesize(context.Background(), Request{
		Text: "hello", VoiceID: "allowed",
	}); err != nil {
		t.Fatalf("configured voice should pass: %v", err)
	}
}
