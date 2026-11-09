package minimax_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/minimax"
)

func TestSpeech(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" ||
			r.URL.Path != "/prefix/v1/t2a_v2" ||
			r.URL.Query().Get("GroupId") != "group & one" ||
			r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("request=%s %s %v", r.Method, r.URL, r.Header)
		}
		var b struct {
			Model    string `json:"model"`
			Text     string `json:"text"`
			Language string `json:"language_boost"`
			Voice    struct {
				ID      string  `json:"voice_id"`
				Speed   float64 `json:"speed"`
				Volume  float64 `json:"vol"`
				Pitch   int     `json:"pitch"`
				Emotion string  `json:"emotion"`
			} `json:"voice_setting"`
			Audio struct {
				Rate    int    `json:"sample_rate"`
				Bitrate int    `json:"bitrate"`
				Format  string `json:"format"`
				Channel int    `json:"channel"`
			} `json:"audio_setting"`
			Pronunciation struct {
				Tone []string `json:"tone"`
			} `json:"pronunciation_dict"`
		}
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Error(err)
			return
		}
		if b.Model != "speech-2.8-hd" ||
			b.Text != "(sighs) Fine. <#0.3#>" ||
			b.Language != "auto" ||
			b.Voice.ID != "Wise_Woman" ||
			b.Voice.Speed != 1.2 ||
			b.Voice.Volume != 0.8 ||
			b.Voice.Pitch != 2 ||
			b.Voice.Emotion != "calm" ||
			b.Audio.Rate != 32000 ||
			b.Audio.Bitrate != 128000 ||
			b.Audio.Format != "mp3" ||
			b.Audio.Channel != 1 ||
			len(b.Pronunciation.Tone) != 1 ||
			b.Pronunciation.Tone[0] != "Fine/(faɪn)" {
			t.Errorf("body=%+v", b)
		}
		_, _ = io.WriteString(
			w,
			`{"data":{"audio":"6d7033","status":2},"trace_id":"trace-1","extra_info":{"audio_length":1200,`+
				`"audio_sample_rate":32000,"audio_size":3,"bitrate":128000,"word_count":8,"usage_characters":10,`+
				`"invisible_character_ratio":0.1,"audio_format":"mp3","audio_channel":1},`+
				`"base_resp":{"status_code":0}}`,
		)
	}))
	defer srv.Close()
	api, err := minimax.NewSpeech(
		route.New(
			gateway.MustURL(srv.URL+"/prefix"),
			nil,
			gateway.Bearer("secret"),
		),
		upstream.WithHTTPClient(srv.Client()),
	)
	if err != nil {
		t.Fatal(err)
	}
	got, err := api.Synthesize(
		t.Context(),
		minimax.SpeechRequest{
			Model:         "speech-2.8-hd",
			GroupID:       "group & one",
			Text:          "(sighs) Fine. <#0.3#>",
			VoiceID:       "Wise_Woman",
			Speed:         1.2,
			Volume:        0.8,
			Pitch:         2,
			Emotion:       "calm",
			LanguageBoost: "auto",
			SampleRate:    32000,
			Bitrate:       128000,
			Format:        "mp3",
			Channel:       1,
			PronunciationDict: []string{
				"Fine/(faɪn)",
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := minimax.Metadata{
		TraceID:                 "trace-1",
		AudioLengthMs:           1200,
		AudioSizeBytes:          3,
		UsageCharacters:         10,
		WordCount:               8,
		InvisibleCharacterRatio: 0.1,
		SampleRate:              32000,
		Bitrate:                 128000,
		Format:                  "mp3",
		Channel:                 1,
	}
	if string(got.Audio) != "mp3" || got.Metadata != want {
		t.Fatalf("response=%+v", got)
	}
}
func TestSpeechFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body, contains string
		code                 string
	}{
		{"rate limit", `{"base_resp":{"status_code":1002,"status_msg":"rate limit"},"trace_id":"trace-1"}`, "", "1002"},
		{"partial", `{"data":{"audio":"6d7033","status":1},"trace_id":"trace-1"}`, "status is 1", ""},
		{"empty", `{"data":{"status":2},"trace_id":"trace-1"}`, "no audio", ""},
		{"bad hex", `{"data":{"audio":"invalid","status":2},"trace_id":"trace-1"}`, "decode audio", ""},
		{"bad json", `{`, "parse response", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				_, _ = io.WriteString(
					w,
					tc.body,
				)
			}))
			defer srv.Close()
			api, err := minimax.NewSpeech(route.New(gateway.MustURL(srv.URL), nil, nil), upstream.WithHTTPClient(srv.Client()))
			if err != nil {
				t.Fatal(err)
			}
			_, err = api.Synthesize(t.Context(), minimax.SpeechRequest{Model: "m", GroupID: "g", Text: "hello", VoiceID: "v"})
			if err == nil || calls != 1 || !strings.Contains(err.Error(), tc.contains) {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
			if tc.code != "" {
				var e *upstream.APIError
				if !errors.As(
					err,
					&e,
				) || e.StatusCode != 200 || e.Code != tc.code || e.Message != "rate limit" || e.RequestID != "trace-1" {
					t.Fatalf("error=%v", err)
				}
			}
		})
	}
}
