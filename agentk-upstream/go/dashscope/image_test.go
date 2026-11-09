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

func TestImageEncoding(t *testing.T) {
	for _, size := range []string{"1536*1024", ""} {
		t.Run(size, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" ||
					r.URL.Path != "/prefix/api/v1/services/aigc/multimodal-generation/generation" ||
					r.Header.Get("Authorization") != "Bearer secret" {
					t.Errorf("request=%s %s %v", r.Method, r.URL, r.Header)
				}
				var body struct {
					Model string
					Input struct {
						Messages []struct {
							Role    string
							Content []map[string]string
						}
					}
					Parameters map[string]any
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				if body.Model != "qwen-image-edit" || len(body.Input.Messages) != 1 {
					t.Errorf("body=%+v", body)
					return
				}
				m := body.Input.Messages[0]
				if m.Role != "user" || len(m.Content) != 3 {
					t.Errorf("message=%+v", m)
					return
				}
				if m.Content[0]["image"] != "https://cdn.test/ref?signature=a%2Bb" ||
					m.Content[1]["image"] != "data:image/png;base64,cmVm" ||
					m.Content[2]["text"] != "画猫" {
					t.Errorf("content=%v", m.Content)
				}
				p := body.Parameters
				if p["n"] != float64(1) ||
					p["negative_prompt"] != "blur" ||
					p["prompt_extend"] != false ||
					p["watermark"] != false {
					t.Errorf("parameters=%v", p)
				}
				value, exists := p["size"]
				if size == "" && exists || size != "" && value != size {
					t.Errorf("size=%v", value)
				}
				_, _ = io.WriteString(
					w,
					`{"output":{"choices":[{"message":{"content":[{"text":"result"},{"image":"http://`+r.Host+`/image.png?signature=a%2Bb"}]}}]}}`,
				)
			}))
			defer srv.Close()
			api, err := dashscope.NewImage(
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
			watermark := false
			content := []dashscope.ImageContent{
				{
					Image: "https://cdn.test/ref?signature=a%2Bb",
				},
				{
					Data: []byte("ref"),
					MIME: "image/png",
				},
				{
					Text: "画猫",
				},
			}
			got, err := api.Generate(
				t.Context(),
				dashscope.ImageRequest{
					Model:   "qwen-image-edit",
					Content: content,
					Parameters: dashscope.ImageParameters{
						Size:           size,
						N:              1,
						NegativePrompt: "blur",
						Watermark:      &watermark,
					},
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || got != srv.URL+"/image.png?signature=a%2Bb" || content[1].Image != "" {
				t.Fatalf("calls=%d url=%s content=%v", calls, got, content)
			}
		})
	}
}
func TestImageErrorsAndDownloadClassification(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		status                  int
		body, code, message, id string
		download                bool
	}{
		{
			"connection",
			400,
			`{"code":"InvalidURL.ConnectionRefused","message":"request rejected","request_id":"body-id"}`,
			"InvalidURL.ConnectionRefused",
			"request rejected",
			"body-id",
			true,
		},
		{
			"timeout",
			400,
			`{"code":"InvalidURL.Timeout","message":"request rejected"}`,
			"InvalidURL.Timeout",
			"request rejected",
			"header-id",
			true,
		},
		{
			"unrelated",
			400,
			`{"code":"DataInspectionFailed","message":"cannot download image"}`,
			"DataInspectionFailed",
			"cannot download image",
			"header-id",
			false,
		},
		{
			"wrong status",
			500,
			`{"code":"InvalidURL.Timeout","message":"timeout"}`,
			"InvalidURL.Timeout",
			"timeout",
			"header-id",
			false,
		},
		{
			"business",
			200,
			`{"code":"InvalidParameter","message":"bad input"}`,
			"InvalidParameter",
			"bad input",
			"header-id",
			false,
		},
		{"non json", 502, `bad gateway`, "", "bad gateway", "header-id", false},
		{"empty result", 200, `{"output":{"choices":[{"message":{"content":[{"text":"only text"}]}}]}}`, "", "", "", false},
		{"invalid json", 200, `{`, "", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("x-request-id", "header-id")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			api, err := dashscope.NewImage(
				route.New(
					gateway.MustURL(srv.URL+"/api/v1/"),
					nil,
					nil,
				),
				upstream.WithHTTPClient(srv.Client()),
			)
			if err != nil {
				t.Fatal(err)
			}
			_, err = api.Generate(
				t.Context(),
				dashscope.ImageRequest{
					Model: "m",
					Content: []dashscope.ImageContent{
						{
							Text: "p",
						},
					},
				},
			)
			if err == nil || calls != 1 || dashscope.IsInputDownloadFailure(err) != tc.download {
				t.Fatalf("error=%v calls=%d", err, calls)
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
	if dashscope.IsInputDownloadFailure(errors.New("InvalidURL.Timeout")) || dashscope.IsInputDownloadFailure(nil) {
		t.Fatal("unrelated error matched")
	}
}
