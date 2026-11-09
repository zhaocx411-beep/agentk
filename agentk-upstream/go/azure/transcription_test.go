package azure_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/azure"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
)

func TestTranscription(t *testing.T) {
	for _, task := range []string{"transcribe", "translate"} {
		t.Run(task, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				operation := "transcriptions"
				if task == "translate" {
					operation = "translations"
				}
				if r.Method != "POST" ||
					r.URL.Path != "/prefix/openai/deployments/whisper/audio/"+operation ||
					r.URL.Query().Get("api-version") != "2024-02-01" ||
					r.Header.Get("api-key") != "secret" {
					t.Errorf("request=%s %s %v", r.Method, r.URL, r.Header)
				}
				mr, err := r.MultipartReader()
				if err != nil {
					t.Error(err)
					return
				}
				fields := map[string]string{}
				files := 0
				for {
					p, err := mr.NextPart()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Error(err)
						return
					}
					data, err := io.ReadAll(p)
					closeErr := p.Close()
					if err != nil || closeErr != nil {
						t.Errorf("read=%v close=%v", err, closeErr)
						return
					}
					if p.FormName() == "file" {
						files++
						if p.FileName() != "clip.wav" ||
							p.Header.Get("Content-Type") != "audio/wav" ||
							string(data) != "RIFF audio" {
							t.Errorf("file=%s %v %q", p.FileName(), p.Header, data)
						}
					} else {
						fields[p.FormName()] = string(data)
					}
				}
				if files != 1 ||
					fields["model"] != "whisper-model" ||
					fields["prompt"] != "词汇" ||
					fields["response_format"] != "verbose_json" {
					t.Errorf("files=%d fields=%v", files, fields)
				}
				if task == "transcribe" {
					if fields["language"] != "zh" || fields["timestamp_granularities[]"] != "segment" {
						t.Errorf("fields=%v", fields)
					}
				} else {
					if _, ok := fields["language"]; ok {
						t.Errorf("fields=%v", fields)
					}
					if _, ok := fields["timestamp_granularities[]"]; ok {
						t.Errorf("fields=%v", fields)
					}
				}
				w.Header().Set("apim-request-id", "request-1")
				_, _ = io.WriteString(
					w,
					`{"text":"  hello  ","language":"chinese","duration":2.5,"segments":[{"id":1,"start":0.5,`+
						`"end":2.5,"text":"hello"}]}`,
				)
			}))
			defer srv.Close()
			api, err := azure.NewTranscription(
				route.New(
					gateway.MustURL(srv.URL+"/prefix"),
					nil,
					gateway.HeaderKey{
						Name:  "api-key",
						Value: "secret",
					},
				),
				upstream.WithHTTPClient(srv.Client()),
			)
			if err != nil {
				t.Fatal(err)
			}
			req := azure.TranscriptionRequest{
				Audio: azure.Audio{
					Data:     []byte("RIFF audio"),
					Filename: "clip.wav",
					MIME:     "audio/wav",
				},
				Model:          "whisper-model",
				Deployment:     "whisper",
				APIVersion:     "2024-02-01",
				ResponseFormat: "verbose_json",
				Task:           task,
				Prompt:         "词汇",
			}
			if task == "transcribe" {
				req.Language = "zh"
			}
			got, err := api.Transcribe(t.Context(), req)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 ||
				got.Text != "hello" ||
				got.Language != "chinese" ||
				got.Duration != 2.5 ||
				got.RequestID != "request-1" ||
				len(got.Segments) != 1 ||
				got.Segments[0] != (azure.Segment{
					ID:    1,
					Start: 0.5,
					End:   2.5,
					Text:  "hello",
				}) {
				t.Fatalf("response=%+v calls=%d", got, calls)
			}
		})
	}
}
func TestTranscriptionErrors(t *testing.T) {
	for _, tc := range []struct {
		status              int
		body, code, message string
	}{
		{400, `{"error":{"code":"BadAudio","message":"invalid audio"}}`, "BadAudio", "invalid audio"},
		{502, `bad gateway`, "", "bad gateway"},
		{200, `{"text":" "}`, "", ""},
		{200, `{`, "", ""},
	} {
		t.Run(fmt.Sprintf("%d/%s", tc.status, tc.body), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("apim-request-id", "request-1")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			api, err := azure.NewTranscription(
				route.New(
					gateway.MustURL(srv.URL),
					nil,
					nil,
				),
				upstream.WithHTTPClient(srv.Client()),
			)
			if err != nil {
				t.Fatal(err)
			}
			_, err = api.Transcribe(
				t.Context(),
				azure.TranscriptionRequest{
					Audio: azure.Audio{
						Data:     []byte("audio"),
						Filename: "clip.wav",
						MIME:     "audio/wav",
					},
					Deployment: "whisper",
					Task:       "transcribe",
				},
			)
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.status != 200 {
				var e *upstream.APIError
				if !errors.As(
					err,
					&e,
				) || e.StatusCode != tc.status || e.Code != tc.code || e.Message != tc.message || e.RequestID != "request-1" {
					t.Fatalf("error=%v", err)
				}
			} else if tc.body == "{" {
				var e *json.SyntaxError
				if !errors.As(err, &e) {
					t.Fatalf("lost JSON error: %v", err)
				}
			}
		})
	}
}
