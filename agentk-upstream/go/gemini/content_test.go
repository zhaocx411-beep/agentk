package gemini_test

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
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gemini"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
)

func TestImageAndMediaContent(t *testing.T) {
	for _, image := range []bool{false, true} {
		t.Run(map[bool]string{false: "media", true: "image"}[image], func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" ||
					r.URL.Path != "/publishers/google/models/model-one:generateContent" ||
					r.Header.Get("x-api-key") != "secret" {
					t.Errorf("request=%s %s %v", r.Method, r.URL, r.Header)
				}
				var b struct {
					Contents []struct {
						Role  string
						Parts []struct {
							Text     string
							FileData struct {
								MIME string `json:"mimeType"`
								URI  string `json:"fileUri"`
							}
							InlineData struct {
								MIME string `json:"mimeType"`
								Data string
							}
							VideoMetadata *struct {
								StartOffset, EndOffset string
								FPS                    float64
							}
						}
					}
					GenerationConfig *struct {
						ResponseModalities []string
						ImageConfig        gemini.ImageConfig
					}
				}
				if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
					t.Error(err)
					return
				}
				if len(b.Contents) != 1 || b.Contents[0].Role != "user" || len(b.Contents[0].Parts) != 3 {
					t.Errorf("body=%+v", b)
					return
				}
				parts := b.Contents[0].Parts
				if parts[0].FileData.URI != "https://cdn.test/ref?signature=a%2Bb" ||
					parts[0].FileData.MIME != "image/png" ||
					parts[1].InlineData.MIME != "audio/wav" ||
					parts[1].InlineData.Data != "YXVkaW8=" ||
					parts[2].Text != "描述" {
					t.Errorf("parts=%+v", parts)
				}
				if image {
					cfg := b.GenerationConfig
					if cfg == nil ||
						len(cfg.ResponseModalities) != 2 ||
						cfg.ResponseModalities[0] != "TEXT" ||
						cfg.ResponseModalities[1] != "IMAGE" ||
						cfg.ImageConfig.AspectRatio != "16:9" ||
						cfg.ImageConfig.ImageSize != "2K" {
						t.Errorf("config=%+v", cfg)
					}
					_, _ = io.WriteString(
						w,
						`{"candidates":[{"content":{"parts":[{"text":"image"},{"inlineData":{"mimeType":"image/jpeg","data":"cG5n"}}]}}]}`,
					)
				} else {
					if b.GenerationConfig != nil {
						t.Errorf("config=%+v", b.GenerationConfig)
					}
					_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":" hello"},{"text":" world "}]}}]}`)
				}
			}))
			defer srv.Close()
			r := route.New(
				gateway.MustURL(srv.URL+"/publishers/google"),
				nil,
				gateway.HeaderKey{
					Name:  "x-api-key",
					Value: "secret",
				},
			)
			media := []gemini.Media{
				{
					MIME: "image/png",
					URL:  "https://cdn.test/ref?signature=a%2Bb",
				},
				{
					MIME: "audio/wav",
					Data: []byte("audio"),
				},
			}
			if image {
				api, err := gemini.NewImage(r, upstream.WithHTTPClient(srv.Client()))
				if err != nil {
					t.Fatal(err)
				}
				got, err := api.Generate(
					t.Context(),
					gemini.ImageRequest{
						Model:  "model-one",
						Prompt: "描述",
						Media:  media,
						Config: gemini.ImageConfig{
							AspectRatio: "16:9",
							ImageSize:   "2K",
						},
					},
				)
				if err != nil || string(got.Data) != "png" || got.MIME != "image/jpeg" {
					t.Fatalf("image=%+v error=%v", got, err)
				}
			} else {
				api, err := gemini.NewMedia(r, upstream.WithHTTPClient(srv.Client()))
				if err != nil {
					t.Fatal(err)
				}
				got, err := api.Understand(t.Context(), gemini.MediaRequest{Model: "model-one", Prompt: "描述", Media: media})
				if err != nil || got != "hello world" {
					t.Fatalf("text=%s error=%v", got, err)
				}
			}
			if calls != 1 {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}
func TestVideoSegment(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(map[bool]string{false: "inline", true: "remote"}[remote], func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var b struct {
					Contents []struct {
						Parts []struct {
							VideoMetadata *struct {
								StartOffset, EndOffset string
								FPS                    float64
							}
							FileData   *struct{ FileURI string }
							InlineData *struct{ Data string }
						}
					}
				}
				if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
					t.Error(err)
					return
				}
				if len(b.Contents) != 1 || len(b.Contents[0].Parts) != 2 {
					t.Errorf("body=%+v", b)
					return
				}
				p := b.Contents[0].Parts[0]
				v := p.VideoMetadata
				if v == nil || v.StartOffset != "1.25s" || v.EndOffset != "4.5s" || v.FPS != 2 {
					t.Errorf("metadata=%+v", v)
				}
				if remote {
					if p.FileData == nil || p.FileData.FileURI != "https://cdn.test/clip.mp4" {
						t.Errorf("part=%+v", p)
					}
				} else {
					if p.InlineData == nil || p.InlineData.Data != "dmlkZW8=" {
						t.Errorf("part=%+v", p)
					}
				}
				if b.Contents[0].Parts[1].VideoMetadata != nil {
					t.Error("text part contains video metadata")
				}
				_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"answer"}]}}]}`)
			}))
			defer srv.Close()
			api, err := gemini.NewMedia(route.New(gateway.MustURL(srv.URL), nil, nil), upstream.WithHTTPClient(srv.Client()))
			if err != nil {
				t.Fatal(err)
			}
			media := gemini.Media{MIME: "video/mp4", Data: []byte("video")}
			if remote {
				media.URL = "https://cdn.test/clip.mp4"
				media.Data = nil
			}
			_, err = api.Understand(
				t.Context(),
				gemini.MediaRequest{
					Model:  "m",
					Prompt: "p",
					Media: []gemini.Media{
						media,
					},
					Segment: &gemini.VideoSegment{
						StartSeconds: 1.25,
						EndSeconds:   4.5,
						FPS:          2,
					},
				},
			)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestContentFailures(t *testing.T) {
	for _, image := range []bool{false, true} {
		t.Run(map[bool]string{false: "media", true: "image"}[image], func(t *testing.T) {
			for _, tc := range []struct {
				name                string
				status              int
				body, code, message string
			}{
				{
					"rejected",
					400,
					`{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"bad input"}}`,
					"INVALID_ARGUMENT",
					"bad input",
				},
				{
					"business",
					200,
					`{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"bad input"}}`,
					"INVALID_ARGUMENT",
					"bad input",
				},
				{"non json", 502, `bad gateway`, "", "bad gateway"},
				{
					"empty",
					200,
					`{"candidates":[{"content":{"parts":[]},"finishReason":"SAFETY"}],` +
						`"promptFeedback":{"blockReason":"PROHIBITED_CONTENT"}}`,
					"",
					"",
				},
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
					r := route.New(gateway.MustURL(srv.URL), nil, nil)
					var err error
					if image {
						api, e := gemini.NewImage(r, upstream.WithHTTPClient(srv.Client()))
						if e != nil {
							t.Fatal(e)
						}
						_, err = api.Generate(t.Context(), gemini.ImageRequest{Model: "m", Prompt: "p"})
					} else {
						api, e := gemini.NewMedia(r, upstream.WithHTTPClient(srv.Client()))
						if e != nil {
							t.Fatal(e)
						}
						_, err = api.Understand(t.Context(), gemini.MediaRequest{Model: "m", Prompt: "p"})
					}
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
					if tc.name == "empty" && (!strings.Contains(
						err.Error(),
						"SAFETY",
					) || !strings.Contains(
						err.Error(),
						"PROHIBITED_CONTENT",
					)) {
						t.Fatal(err)
					}
				})
			}
		})
	}
}
func TestInputDownloadFailure(t *testing.T) {
	for _, tc := range []struct {
		name            string
		status, numeric int
		code, message   string
		want            bool
	}{
		{
			"dns",
			400,
			400,
			"INVALID_ARGUMENT",
			"Cannot fetch content from the provided URL. Status: URL_ERROR-ERROR_DNS",
			true,
		},
		{
			"too large",
			400,
			400,
			"INVALID_ARGUMENT",
			"Cannot fetch content from the provided URL. Status: URL_ERROR-PAYLOAD_TOO_LARGE",
			false,
		},
		{"wrong http status", 413, 400, "INVALID_ARGUMENT", "Cannot fetch content from the provided URL", false},
		{"wrong numeric code", 400, 403, "INVALID_ARGUMENT", "Cannot fetch content from the provided URL", false},
		{"wrong code", 400, 400, "PERMISSION_DENIED", "Cannot fetch content from the provided URL", false},
		{"unrelated", 400, 400, "INVALID_ARGUMENT", "invalid image", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				if err := json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{
						"code":    tc.numeric,
						"status":  tc.code,
						"message": tc.message,
					},
				}); err != nil {
					t.Error(err)
				}
			}))
			defer srv.Close()
			api, err := gemini.NewMedia(route.New(gateway.MustURL(srv.URL), nil, nil), upstream.WithHTTPClient(srv.Client()))
			if err != nil {
				t.Fatal(err)
			}
			_, err = api.Understand(
				t.Context(),
				gemini.MediaRequest{
					Model:  "m",
					Prompt: "p",
					Media: []gemini.Media{
						{
							MIME: "video/mp4",
							URL:  "https://cdn.test/clip.mp4",
						},
					},
				},
			)
			if err == nil || calls != 1 || gemini.IsInputDownloadFailure(err) != tc.want {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
		})
	}
	if gemini.IsInputDownloadFailure(nil) ||
		gemini.IsInputDownloadFailure(errors.New("Cannot fetch content from the provided URL")) {
		t.Fatal("unrelated error matched")
	}
}
func TestImageInvalidBase64(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(
			w,
			`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"!invalid!"}}]}}]}`,
		)
	}))
	defer srv.Close()
	api, err := gemini.NewImage(route.New(gateway.MustURL(srv.URL), nil, nil), upstream.WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = api.Generate(t.Context(), gemini.ImageRequest{Model: "m", Prompt: "p"}); err == nil {
		t.Fatal("expected base64 error")
	}
}
