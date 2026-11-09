package tts

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"go.temporal.io/sdk/temporal"

	"agentk/internal/artifact"
)

type stubTTSClient struct {
	last  Request
	audio Audio
	err   error
}

func (s *stubTTSClient) Synthesize(_ context.Context, req Request) (Audio, error) {
	s.last = req
	return s.audio, s.err
}

func (*stubTTSClient) ValidateRequest(req Request) error {
	_, err := normalizeRequest(req)
	return err
}

type ttsCatalog map[string]Client

func (c ttsCatalog) TTSClient(name string) (Client, bool) {
	client, ok := c[name]
	return client, ok
}

type stubStorage struct {
	key, mime string
	data      []byte
}

func (s *stubStorage) Put(_ context.Context, key string, data []byte, mime string) (artifact.Object, error) {
	s.key, s.data, s.mime = key, data, mime
	return artifact.Object{Key: key, URL: "http://blob.test/" + key}, nil
}
func (*stubStorage) Resolve(string) (artifact.Object, bool) { return artifact.Object{}, false }
func (*stubStorage) Open(context.Context, artifact.Object) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

func TestCapabilityActivity(t *testing.T) {
	client := &stubTTSClient{audio: Audio{
		Data: []byte("mp3"), MIME: "audio/mpeg",
		Params:   map[string]any{"voiceId": "Wise_Woman", "format": "mp3"},
		Metadata: Metadata{TraceID: "trace-1", AudioLengthMs: 1234},
	}}
	storage := &stubStorage{}
	activity := NewActivity(ttsCatalog{"minimax-speech-2.8-hd": client}, storage)
	out, err := activity.RunMiniMax(context.Background(), MiniMaxInput{
		Model: "minimax-speech-2.8-hd", Text: "(sighs) hello", VoiceID: "Wise_Woman",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Artifacts) != 1 || out.Artifacts[0].URI != "http://blob.test/"+storage.key ||
		out.Artifacts[0].Description == "" || out.Artifacts[0].Model != "minimax-speech-2.8-hd" {
		t.Fatalf("out = %+v", out)
	}
	if out.Metadata.TraceID != "trace-1" || out.Metadata.AudioLengthMs != 1234 {
		t.Fatalf("metadata = %+v", out.Metadata)
	}
	if !strings.HasPrefix(storage.key, "capabilities/audio/") ||
		!strings.HasSuffix(storage.key, ".mp3") ||
		string(storage.data) != "mp3" || storage.mime != "audio/mpeg" {
		t.Fatalf("stored key=%q data=%q mime=%q", storage.key, storage.data, storage.mime)
	}
	if client.last.Text != "(sighs) hello" || client.last.VoiceID != "Wise_Woman" {
		t.Fatalf("request = %+v", client.last)
	}
}

func TestCapabilityActivityValidation(t *testing.T) {
	activity := NewActivity(ttsCatalog{"minimax-speech-2.8-hd": &stubTTSClient{}}, &stubStorage{})
	valid := MiniMaxInput{Model: "minimax-speech-2.8-hd", Text: "hello", VoiceID: "Wise_Woman"}
	tests := map[string]func(*MiniMaxInput){
		"model":    func(in *MiniMaxInput) { in.Model = "" },
		"text":     func(in *MiniMaxInput) { in.Text = " " },
		"voice":    func(in *MiniMaxInput) { in.VoiceID = "" },
		"speed":    func(in *MiniMaxInput) { in.Speed = 2.1 },
		"language": func(in *MiniMaxInput) { in.LanguageBoost = "Klingon" },
		"emotion":  func(in *MiniMaxInput) { in.Emotion = "excited" },
		"pronunciation": func(in *MiniMaxInput) {
			in.PronunciationDict = []string{"invalid"}
		},
		"too long": func(in *MiniMaxInput) {
			in.Text = strings.Repeat("你", maxTextCharacters+1)
		},
		"wrong family": func(in *MiniMaxInput) { in.Model = "qwen3-tts-flash" },
	}
	for name, mutate := range tests {
		in := valid
		mutate(&in)
		_, err := activity.RunMiniMax(context.Background(), in)
		var appErr *temporal.ApplicationError
		if !errors.As(err, &appErr) || !appErr.NonRetryable() {
			t.Fatalf("%s: err = %v, want non-retryable", name, err)
		}
	}
}

func TestKnownUnconfiguredModelIsConfigurationError(t *testing.T) {
	activity := NewActivity(ttsCatalog{}, &stubStorage{})
	_, err := activity.RunQwen(context.Background(), QwenInput{
		Model: "qwen3-tts-flash", Text: "hello", VoiceID: "Cherry",
	})
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) || appErr.Type() != "Configuration" ||
		!strings.Contains(err.Error(), "not configured") {
		t.Fatalf("error = %v", err)
	}
}

func TestTextLimitCountsUnicodeCharacters(t *testing.T) {
	req := Request{Text: strings.Repeat("你", maxTextCharacters), VoiceID: "v"}
	if _, err := normalizeRequest(req); err != nil {
		t.Fatalf("9999 Chinese characters should be accepted: %v", err)
	}
	req.Text += "你"
	if _, err := normalizeRequest(req); err == nil {
		t.Fatal("10000 Chinese characters should be rejected")
	}
}

func TestManifestCapabilities(t *testing.T) {
	manifests := ManifestCapabilities()
	if len(manifests) != 3 {
		t.Fatalf("manifest count = %d", len(manifests))
	}
	schema := string(manifests[0].InputSchema)
	for _, want := range []string{
		`"minimax-speech-2.8-hd"`, `"voiceId"`, `"emotion"`,
		`"pronunciationDict"`, `"speed"`, `"languageBoost"`, `MiniMax 高清语音`,
	} {
		if !strings.Contains(schema, want) {
			t.Fatalf("schema missing %q: %s", want, schema)
		}
	}
	if strings.Contains(schema, `"languageType"`) || strings.Contains(schema, `"instructions"`) {
		t.Fatalf("MiniMax schema leaked Qwen fields: %s", schema)
	}
	if qwen := string(manifests[1].InputSchema); !strings.Contains(qwen, `"languageType"`) ||
		strings.Contains(qwen, `"emotion"`) {
		t.Fatalf("Qwen schema = %s", qwen)
	}
	if cosy := string(manifests[2].InputSchema); !strings.Contains(cosy, `"instructions"`) ||
		strings.Contains(cosy, `"languageType"`) {
		t.Fatalf("CosyVoice schema = %s", cosy)
	}
}
