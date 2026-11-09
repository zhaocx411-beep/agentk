package capability_test

import (
	"encoding/json"
	"testing"

	"agentk/internal/capability/asr"
	"agentk/internal/capability/imagegen"
	"agentk/internal/capability/mediaunderstand"
	"agentk/internal/capability/readfile"
	"agentk/internal/capability/textgen"
	"agentk/internal/capability/tts"
	"agentk/internal/capability/videocompose"
	"agentk/internal/capability/videogen"
	"agentk/internal/capability/videoprobe"
)

func TestNativeCapabilityPayloadsDoNotLeakAgentKContext(t *testing.T) {
	payloads := []any{
		imagegen.CapabilityInput{},
		videogen.ActivityInput{},
		videogen.KlingT2VInput{},
		videogen.KlingI2VInput{},
		videogen.KlingOmniInput{},
		videogen.SeedanceInput{},
		videocompose.ActivityInput{},
		videoprobe.ActivityInput{},
		mediaunderstand.CapabilityInput{},
		mediaunderstand.SegmentInput{},
		readfile.ActivityInput{},
		textgen.Input{},
		tts.MiniMaxInput{},
		tts.QwenInput{},
		tts.CosyVoiceInput{},
		asr.CapabilityInput{},
	}
	for _, payload := range payloads {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"session", "sessionId", "tenant", "idemKey", "execPublicId"} {
			if _, exists := fields[forbidden]; exists {
				t.Fatalf("%T exposes forbidden field %q: %s", payload, forbidden, raw)
			}
		}
	}
}
