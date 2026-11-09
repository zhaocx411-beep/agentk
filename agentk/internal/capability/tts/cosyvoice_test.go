package tts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCosyVoiceSynthesize(t *testing.T) {
	var gotAuthorization string
	var gotBody map[string]any
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/api/v1/services/audio/tts/SpeechSynthesizer", func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"request_id": "request-1",
			"output": map[string]any{"audio": map[string]any{
				"url": srv.URL + "/result.wav", "id": "audio-1", "expires_at": 123,
			}},
			"usage": map[string]any{"characters": 12},
		})
	})
	mux.HandleFunc("/result.wav", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write([]byte("RIFF\x00\x00\x00\x00WAVE"))
	})
	client, err := NewCosyVoice(CosyVoiceConfig{
		BaseURL: srv.URL + "/api/v1", APIKey: "secret", Model: "cosyvoice-v3.5-plus",
	})
	if err != nil {
		t.Fatal(err)
	}
	audio, err := client.Synthesize(context.Background(), Request{
		Text: "今天天气怎么样？", VoiceID: "myvoice-123", Instructions: "用轻快自然的语气",
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(audio.Data) != "RIFF\x00\x00\x00\x00WAVE" || audio.MIME != "audio/wav" {
		t.Fatalf("audio = %+v", audio)
	}
	if gotAuthorization != "Bearer secret" || gotBody["model"] != "cosyvoice-v3.5-plus" {
		t.Fatalf("authorization=%q body=%v", gotAuthorization, gotBody)
	}
	input := gotBody["input"].(map[string]any)
	if input["voice"] != "myvoice-123" || input["instruction"] != "用轻快自然的语气" ||
		input["format"] != "wav" || input["sample_rate"] != float64(24000) {
		t.Fatalf("input = %v", input)
	}
	if audio.Metadata.RequestID != "request-1" || audio.Metadata.AudioID != "audio-1" ||
		audio.Metadata.UsageCharacters != 12 {
		t.Fatalf("metadata = %+v", audio.Metadata)
	}
}

func TestCosyVoiceValidation(t *testing.T) {
	client, err := NewCosyVoice(CosyVoiceConfig{
		BaseURL: "https://workspace.test/api/v1", APIKey: "key", Model: "cosyvoice-v3.5-plus",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []Request{
		{VoiceID: "v"},
		{Text: "hello"},
		{Text: "hello", VoiceID: "v", Speed: 1},
		{Text: "hello", VoiceID: "v", LanguageType: "English"},
		{Text: "hello", VoiceID: "v", OptimizeInstructions: true},
		{Text: "hello", VoiceID: "v", Instructions: strings.Repeat("中", 51)},
	} {
		if err := client.ValidateRequest(req); err == nil {
			t.Fatalf("expected validation error for %+v", req)
		}
	}
	if err := client.ValidateRequest(Request{
		Text: "hello", VoiceID: "v", Instructions: strings.Repeat("a", 100),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestNewCosyVoiceValidation(t *testing.T) {
	for i, cfg := range []CosyVoiceConfig{
		{BaseURL: "relative", APIKey: "k", Model: "m"},
		{BaseURL: "https://x", Model: "m"},
		{BaseURL: "https://x", APIKey: "k"},
		{BaseURL: "https://x", APIKey: "k", Model: "m", ProxyURL: "relative"},
	} {
		if _, err := NewCosyVoice(cfg); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
}
