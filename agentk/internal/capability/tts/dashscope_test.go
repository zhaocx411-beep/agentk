package tts

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDashScopeSynthesizeBase64(t *testing.T) {
	wantAudio := []byte("RIFF\x00\x00\x00\x00WAVE")
	var gotAuthorization string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/services/aigc/multimodal-generation/generation" {
			t.Errorf("path = %q", r.URL.Path)
		}
		gotAuthorization = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"request_id": "request-1",
			"output": map[string]any{"audio": map[string]any{
				"data": base64.StdEncoding.EncodeToString(wantAudio),
				"id":   "audio-1", "expires_at": 123,
			}},
			"usage": map[string]any{"characters": 5},
		})
	}))
	t.Cleanup(srv.Close)

	client, err := NewDashScope(DashScopeConfig{
		BaseURL: srv.URL, APIKey: "secret", Model: "qwen3-tts-flash",
	})
	if err != nil {
		t.Fatal(err)
	}
	audio, err := client.Synthesize(context.Background(), Request{
		Text: "你好", VoiceID: "Cherry", LanguageType: "Chinese",
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(audio.Data) != string(wantAudio) || audio.MIME != "audio/wav" {
		t.Fatalf("audio = %+v", audio)
	}
	if gotAuthorization != "Bearer secret" || gotBody["model"] != "qwen3-tts-flash" {
		t.Fatalf("authorization=%q body=%v", gotAuthorization, gotBody)
	}
	input := gotBody["input"].(map[string]any)
	if input["voice"] != "Cherry" || input["language_type"] != "Chinese" {
		t.Fatalf("input = %v", input)
	}
	if audio.Metadata.RequestID != "request-1" || audio.Metadata.AudioID != "audio-1" ||
		audio.Metadata.UsageCharacters != 5 {
		t.Fatalf("metadata = %+v", audio.Metadata)
	}
}

func TestDashScopeSynthesizeDownloadsURL(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/api/v1/services/aigc/multimodal-generation/generation", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": map[string]any{"audio": map[string]any{"url": srv.URL + "/result.wav"}},
		})
	})
	mux.HandleFunc("/result.wav", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write([]byte("RIFF\x00\x00\x00\x00WAVE"))
	})
	client, err := NewDashScope(DashScopeConfig{
		BaseURL: srv.URL + "/api/v1", APIKey: "key", Model: "qwen3-tts-flash",
	})
	if err != nil {
		t.Fatal(err)
	}
	audio, err := client.Synthesize(context.Background(), Request{Text: "hello", VoiceID: "Cherry"})
	if err != nil {
		t.Fatal(err)
	}
	if audio.MIME != "audio/wav" || audio.Params["languageType"] != "Auto" {
		t.Fatalf("audio = %+v", audio)
	}
}

func TestDashScopeValidation(t *testing.T) {
	client, err := NewDashScope(DashScopeConfig{
		BaseURL: "https://dashscope.test", APIKey: "key", Model: "qwen3-tts-flash",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []Request{
		{VoiceID: "Cherry"},
		{Text: "hello"},
		{Text: "hello", VoiceID: "Cherry", Speed: 1},
		{Text: "hello", VoiceID: "Cherry", LanguageType: "Klingon"},
		{Text: "hello", VoiceID: "Cherry", OptimizeInstructions: true},
		{Text: "hello", VoiceID: "Cherry", Instructions: "开心一些"},
	} {
		if err := client.ValidateRequest(req); err == nil {
			t.Fatalf("expected validation error for %+v", req)
		}
	}
	instruct, err := NewDashScope(DashScopeConfig{
		BaseURL: "https://dashscope.test", APIKey: "key", Model: "qwen3-tts-instruct-flash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := instruct.ValidateRequest(Request{
		Text: "hello", VoiceID: "Cherry", Instructions: "sound happy", OptimizeInstructions: true,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDashScopeErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"InvalidParameter","message":"bad voice"}`))
	}))
	t.Cleanup(srv.Close)
	client, err := NewDashScope(DashScopeConfig{BaseURL: srv.URL, APIKey: "key", Model: "qwen3-tts-flash"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Synthesize(context.Background(), Request{Text: "hello", VoiceID: "bad"})
	if err == nil || !strings.Contains(err.Error(), "InvalidParameter") {
		t.Fatalf("err = %v", err)
	}
	for i, cfg := range []DashScopeConfig{
		{BaseURL: "relative", APIKey: "key", Model: "m"},
		{BaseURL: "https://x", Model: "m"},
		{BaseURL: "https://x", APIKey: "key"},
		{BaseURL: "https://x", APIKey: "key", Model: "m", ProxyURL: "relative"},
	} {
		if _, err := NewDashScope(cfg); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
}
