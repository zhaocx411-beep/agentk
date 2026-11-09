package dashscope_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/dashscope"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
)

func TestSpeechProtocols(t *testing.T) {
	for _, cosy := range []bool{false, true} {
		t.Run(map[bool]string{false: "qwen", true: "cosyvoice"}[cosy], func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				path := "/prefix/api/v1/services/aigc/multimodal-generation/generation"
				if cosy {
					path = "/prefix/api/v1/services/audio/tts/SpeechSynthesizer"
				}
				if r.Method != "POST" ||
					r.URL.Path != path ||
					r.Header.Get("Authorization") != "Bearer secret" ||
					r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("request=%s %s %v", r.Method, r.URL, r.Header)
				}
				var body struct {
					Model string
					Input map[string]any
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				if body.Model != "speech-model" || body.Input["text"] != "你好" || body.Input["voice"] != "voice" {
					t.Errorf("body=%+v", body)
				}
				if cosy {
					if body.Input["instruction"] != "calm" ||
						body.Input["format"] != "wav" ||
						body.Input["sample_rate"] != float64(24000) {
						t.Errorf("input=%v", body.Input)
					}
				} else {
					if body.Input["instructions"] != "calm" ||
						body.Input["optimize_instructions"] != true ||
						body.Input["language_type"] != "Chinese" {
						t.Errorf("input=%v", body.Input)
					}
				}
				_, _ = io.WriteString(
					w,
					`{"request_id":"request-1","output":{"audio":{"data":"YXVkaW8=","id":"audio-1",`+
						`"expires_at":123}},"usage":{"characters":5,"input_tokens":2,"output_tokens":3,`+
						`"total_tokens":5}}`,
				)
			}))
			defer srv.Close()
			api, err := dashscope.NewSpeech(
				route.New(
					gateway.MustURL(srv.URL+"/prefix/api/v1/"),
					nil,
					gateway.Bearer("secret"),
				),
				upstream.WithHTTPClient(srv.Client()),
			)
			if err != nil {
				t.Fatal(err)
			}
			req := dashscope.SpeechRequest{
				Model:                "speech-model",
				Text:                 "你好",
				Voice:                "voice",
				LanguageType:         "Chinese",
				Instructions:         "calm",
				OptimizeInstructions: true,
				Format:               "wav",
				SampleRate:           24000,
			}
			var got dashscope.SpeechResponse
			if cosy {
				got, err = api.SynthesizeCosyVoice(t.Context(), req)
			} else {
				got, err = api.Synthesize(t.Context(), req)
			}
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 ||
				string(got.Audio) != "audio" ||
				got.RequestID != "request-1" ||
				got.Output.Audio.ID != "audio-1" ||
				got.Output.Audio.ExpiresAt != 123 ||
				got.Usage.Characters != 5 ||
				got.Usage.TotalTokens != 5 ||
				got.Usage.InputTokens != 2 ||
				got.Usage.OutputTokens != 3 {
				t.Fatalf("response=%+v calls=%d", got, calls)
			}
		})
	}
}
func TestSpeechURLDoesNotDownload(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/v1/services/aigc/multimodal-generation/generation" {
			t.Errorf("unexpected request=%s", r.URL)
			w.WriteHeader(500)
			return
		}
		var body struct{ Input map[string]any }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if _, ok := body.Input["instructions"]; ok {
			t.Errorf("input=%v", body.Input)
		}
		if _, ok := body.Input["optimize_instructions"]; ok {
			t.Errorf("input=%v", body.Input)
		}
		_, _ = io.WriteString(w, `{"output":{"audio":{"url":"http://`+r.Host+`/result.wav"}}}`)
	}))
	defer srv.Close()
	api, err := dashscope.NewSpeech(route.New(gateway.MustURL(srv.URL), nil, nil), upstream.WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	got, err := api.Synthesize(t.Context(), dashscope.SpeechRequest{Model: "m", Text: "hello", Voice: "v"})
	if err != nil || calls != 1 || got.Output.Audio.URL != srv.URL+"/result.wav" || len(got.Audio) != 0 {
		t.Fatalf("response=%+v calls=%d error=%v", got, calls, err)
	}
}
func TestSpeechErrors(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		status                  int
		body, code, message, id string
	}{
		{
			"business",
			200,
			`{"code":"InvalidParameter","message":"bad voice","request_id":"body-id"}`,
			"InvalidParameter",
			"bad voice",
			"body-id",
		},
		{"http", 400, `{"code":"InvalidParameter","message":"bad voice"}`, "InvalidParameter", "bad voice", "header-id"},
		{"non json", 502, `bad gateway`, "", "bad gateway", "header-id"},
		{"invalid json", 200, `{`, "", "", ""},
		{"invalid base64", 200, `{"output":{"audio":{"data":"!invalid!"}}}`, "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("x-request-id", "header-id")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			api, err := dashscope.NewSpeech(route.New(gateway.MustURL(srv.URL), nil, nil), upstream.WithHTTPClient(srv.Client()))
			if err != nil {
				t.Fatal(err)
			}
			_, err = api.Synthesize(t.Context(), dashscope.SpeechRequest{Model: "m", Text: "hello", Voice: "v"})
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.message != "" {
				var e *upstream.APIError
				if !errors.As(
					err,
					&e,
				) || e.StatusCode != tc.status || e.Code != tc.code || e.Message != tc.message || e.RequestID != tc.id {
					t.Fatalf("error=%v", err)
				}
			}
		})
	}
}
