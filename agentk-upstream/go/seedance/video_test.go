package seedance_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/seedance"
)

func TestCreateTask(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" ||
			r.URL.Path != "/api/v3/contents/generations/tasks" ||
			r.Header.Get("x-api-key") != "secret" {
			t.Errorf("request=%s %s %v", r.Method, r.URL, r.Header)
		}
		var b struct {
			Model, Ratio, Resolution string
			Duration                 int
			GenerateAudio            bool `json:"generate_audio"`
			Watermark                bool
			Content                  []struct {
				Type, Text, Role string
				Image            struct{ URL string } `json:"image_url"`
				Video            struct{ URL string } `json:"video_url"`
				Audio            struct{ URL string } `json:"audio_url"`
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Error(err)
			return
		}
		if b.Model != "ep-video" ||
			b.Ratio != "16:9" ||
			b.Resolution != "720p" ||
			b.Duration != 5 ||
			!b.GenerateAudio ||
			b.Watermark ||
			len(b.Content) != 4 {
			t.Errorf("body=%+v", b)
			return
		}
		if b.Content[0].Type != "text" ||
			b.Content[0].Text != "镜头移动" ||
			b.Content[1].Type != "image_url" ||
			b.Content[1].Role != "first_frame" ||
			b.Content[1].Image.URL != "https://cdn.test/image?signature=a%2Bb" ||
			b.Content[2].Type != "video_url" ||
			b.Content[2].Role != "reference_video" ||
			b.Content[2].Video.URL != "https://cdn.test/video" ||
			b.Content[3].Type != "audio_url" ||
			b.Content[3].Role != "reference_audio" ||
			b.Content[3].Audio.URL != "https://cdn.test/audio" {
			t.Errorf("content=%+v", b.Content)
		}
		_, _ = io.WriteString(w, `{"id":"task-1","model":"ep-video","status":"queued"}`)
	}))
	defer srv.Close()
	api, err := seedance.NewVideo(
		route.New(
			gateway.MustURL(srv.URL+"/api/v3/"),
			nil,
			gateway.HeaderKey{
				Name:  "x-api-key",
				Value: "secret",
			},
		),
		upstream.WithHTTPClient(srv.Client()),
	)
	if err != nil {
		t.Fatal(err)
	}
	got, err := api.CreateTask(
		t.Context(),
		seedance.Request{
			Model:         "ep-video",
			Prompt:        "镜头移动",
			Ratio:         "16:9",
			Resolution:    "720p",
			Duration:      5,
			GenerateAudio: true,
			Media: []seedance.Media{
				{
					Role: "first_frame",
					Kind: "image",
					URL:  "https://cdn.test/image?signature=a%2Bb",
				},
				{
					Role: "reference_video",
					Kind: "video",
					URL:  "https://cdn.test/video",
				},
				{
					Role: "reference_audio",
					Kind: "audio",
					URL:  "https://cdn.test/audio",
				},
			},
		},
	)
	if err != nil || calls != 1 || got.ID != "task-1" || got.Model != "ep-video" || got.Status != "queued" {
		t.Fatalf("task=%+v calls=%d error=%v", got, calls, err)
	}
}
func TestGetTask(t *testing.T) {
	for _, status := range []string{"queued", "running", "succeeded", "failed"} {
		t.Run(status, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.EscapedPath() != "/api/v3/contents/generations/tasks/task%2Fone" {
					t.Errorf("request=%s %s", r.Method, r.URL)
				}
				b, err := io.ReadAll(r.Body)
				if err != nil || len(b) != 0 {
					t.Errorf("body=%q error=%v", b, err)
				}
				if err := json.NewEncoder(w).Encode(map[string]any{
					"id":     "task/one",
					"status": status,
					"content": map[string]string{
						"video_url": "https://cdn.test/video",
					},
					"error": map[string]string{
						"code":    "ContentRejected",
						"message": "blocked",
					},
				}); err != nil {
					t.Error(err)
				}
			}))
			defer srv.Close()
			api, err := seedance.NewVideo(
				route.New(
					gateway.MustURL(srv.URL+"/api/v3"),
					nil,
					nil,
				),
				upstream.WithHTTPClient(srv.Client()),
			)
			if err != nil {
				t.Fatal(err)
			}
			got, err := api.GetTask(t.Context(), "task/one")
			if err != nil ||
				calls != 1 ||
				got.ID != "task/one" ||
				got.Status != status ||
				got.Content.VideoURL != "https://cdn.test/video" ||
				got.Error.Code != "ContentRejected" ||
				got.Error.Message != "blocked" {
				t.Fatalf("task=%+v calls=%d error=%v", got, calls, err)
			}
		})
	}
}
func TestTaskFailures(t *testing.T) {
	for _, tc := range []struct {
		name                string
		status              int
		body, code, message string
	}{
		{"rejected", 400, `{"error":{"code":"SensitiveContent","message":"blocked"}}`, "SensitiveContent", "blocked"},
		{"non json", 502, `bad gateway`, "", "bad gateway"},
		{"missing id", 200, `{}`, "", ""},
		{"invalid json", 200, `{`, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("x-request-id", "req-id")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			api, err := seedance.NewVideo(route.New(gateway.MustURL(srv.URL), nil, nil), upstream.WithHTTPClient(srv.Client()))
			if err != nil {
				t.Fatal(err)
			}
			_, err = api.CreateTask(t.Context(), seedance.Request{Model: "m", Prompt: "p"})
			if err == nil || calls != 1 {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
			if tc.message != "" {
				var e *upstream.APIError
				if !errors.As(
					err,
					&e,
				) || e.StatusCode != tc.status || e.Code != tc.code || e.Message != tc.message || e.RequestID != "req-id" {
					t.Fatalf("error=%v", err)
				}
			}
		})
	}
}
