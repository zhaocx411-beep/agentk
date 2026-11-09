package tts

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMiniMaxSynthesize(t *testing.T) {
	wantAudio := []byte("fake-mp3")
	var gotAuthorization, gotGroupID string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		gotGroupID = r.URL.Query().Get("GroupId")
		if r.URL.Path != "/prefix/v1/t2a_v2" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":     map[string]any{"audio": hex.EncodeToString(wantAudio), "status": 2},
			"trace_id": "trace-1",
			"extra_info": map[string]any{
				"audio_length": 1200, "audio_sample_rate": 32000, "audio_size": len(wantAudio),
				"bitrate": 128000, "word_count": 8, "usage_characters": 10,
				"invisible_character_ratio": 0.1, "audio_format": "mp3", "audio_channel": 1,
			},
			"base_resp": map[string]any{"status_code": 0, "status_msg": "success"},
		})
	}))
	t.Cleanup(srv.Close)

	client, err := NewMiniMax(MiniMaxConfig{
		BaseURL: srv.URL + "/prefix", APIKey: "secret", GroupID: "group-1", Model: "speech-2.8-hd",
	})
	if err != nil {
		t.Fatal(err)
	}
	audio, err := client.Synthesize(context.Background(), Request{
		Text: "(sighs) Fine. <#0.3#> Take them all.", VoiceID: "Wise_Woman",
		Emotion: "calm", PronunciationDict: []string{"Fine/(faɪn)"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(audio.Data) != string(wantAudio) || audio.MIME != "audio/mpeg" {
		t.Fatalf("audio = %+v", audio)
	}
	if gotAuthorization != "Bearer secret" || gotGroupID != "group-1" {
		t.Fatalf("auth = %q, group = %q", gotAuthorization, gotGroupID)
	}
	if gotBody["model"] != "speech-2.8-hd" ||
		gotBody["text"] != "(sighs) Fine. <#0.3#> Take them all." ||
		gotBody["language_boost"] != defaultLanguageBoost {
		t.Fatalf("body = %v", gotBody)
	}
	voice := gotBody["voice_setting"].(map[string]any)
	if voice["voice_id"] != "Wise_Woman" || voice["speed"] != defaultSpeed || voice["emotion"] != "calm" {
		t.Fatalf("voice_setting = %v", voice)
	}
	audioSetting := gotBody["audio_setting"].(map[string]any)
	if audioSetting["sample_rate"] != float64(32000) {
		t.Fatalf("audio_setting = %v", audioSetting)
	}
	pronunciation := gotBody["pronunciation_dict"].(map[string]any)
	if len(pronunciation["tone"].([]any)) != 1 {
		t.Fatalf("pronunciation_dict = %v", pronunciation)
	}
	if audio.Params["format"] != "mp3" || audio.Params["voiceId"] != "Wise_Woman" ||
		audio.Params["emotion"] != "calm" {
		t.Fatalf("params = %v", audio.Params)
	}
	if audio.Metadata.TraceID != "trace-1" || audio.Metadata.AudioLengthMs != 1200 ||
		audio.Metadata.SampleRate != 32000 || audio.Metadata.UsageCharacters != 10 {
		t.Fatalf("metadata = %+v", audio.Metadata)
	}
}

func TestMiniMaxRetriesRateLimit(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 3 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"base_resp": map[string]any{"status_code": miniMaxRateLimitCode, "status_msg": "rate limit"},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":      map[string]any{"audio": hex.EncodeToString([]byte("mp3")), "status": 2},
			"base_resp": map[string]any{"status_code": 0},
		})
	}))
	t.Cleanup(srv.Close)

	var delays []time.Duration
	client, err := NewMiniMax(MiniMaxConfig{
		BaseURL: srv.URL, APIKey: "key", GroupID: "group", Model: "speech-2.8-hd",
		MaxRateLimitRetries: 3,
		Sleep: func(_ context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Synthesize(context.Background(), Request{Text: "hello", VoiceID: "Wise_Woman"}); err != nil {
		t.Fatal(err)
	}
	if calls != 3 || len(delays) != 2 || delays[0] != 5*time.Second || delays[1] != 10*time.Second {
		t.Fatalf("calls = %d, delays = %v", calls, delays)
	}
}

func TestMiniMaxErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":      map[string]any{"audio": "not-hex", "status": 2},
			"base_resp": map[string]any{"status_code": 0},
		})
	}))
	t.Cleanup(srv.Close)
	client, err := NewMiniMax(MiniMaxConfig{
		BaseURL: srv.URL, APIKey: "key", GroupID: "group", Model: "speech-2.8-hd",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Synthesize(context.Background(), Request{Text: "hello", VoiceID: "v"}); err == nil ||
		!strings.Contains(err.Error(), "decode audio") {
		t.Fatalf("err = %v", err)
	}
	for _, req := range []Request{
		{VoiceID: "v"},
		{Text: "hello"},
		{Text: "hello", VoiceID: "v", Speed: 2.1},
		{Text: "hello", VoiceID: "v", LanguageBoost: "Klingon"},
		{Text: "hello", VoiceID: "v", Emotion: "excited"},
		{Text: "hello", VoiceID: "v", PronunciationDict: []string{"invalid"}},
	} {
		if _, err := client.Synthesize(context.Background(), req); err == nil {
			t.Fatalf("expected validation error for %+v", req)
		}
	}
}

func TestMiniMaxRejectsNonFinalStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":      map[string]any{"audio": hex.EncodeToString([]byte("partial")), "status": 1},
			"trace_id":  "trace-partial",
			"base_resp": map[string]any{"status_code": 0},
		})
	}))
	t.Cleanup(srv.Close)
	client, err := NewMiniMax(MiniMaxConfig{
		BaseURL: srv.URL, APIKey: "key", GroupID: "group", Model: "speech-2.8-hd",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Synthesize(context.Background(), Request{Text: "hello", VoiceID: "v"})
	if err == nil || !strings.Contains(err.Error(), "status is 1") ||
		!strings.Contains(err.Error(), "trace-partial") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewMiniMaxValidation(t *testing.T) {
	for i, cfg := range []MiniMaxConfig{
		{BaseURL: "relative", APIKey: "k", GroupID: "g", Model: "m"},
		{APIKey: "", GroupID: "g", Model: "m"},
		{APIKey: "k", Model: "m"},
		{APIKey: "k", GroupID: "g"},
		{APIKey: "k", GroupID: "g", Model: "m", ProxyURL: "relative"},
		{APIKey: "k", GroupID: "g", Model: "m", MaxRateLimitRetries: -1},
	} {
		if _, err := NewMiniMax(cfg); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
}
