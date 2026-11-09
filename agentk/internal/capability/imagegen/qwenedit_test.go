package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentk/internal/artifact"
)

// inlineStorage 把命中 prefix 的 URL 当作私有产物：Resolve 认领、Open 回预置字节；
// 其余 URL 不认领（passthrough）。用于验证 qwenedit 对私有产物的 base64 内嵌。
type inlineStorage struct {
	prefix       string
	publicPrefix string
	data         []byte
}

func (s *inlineStorage) Put(context.Context, string, []byte, string) (artifact.Object, error) {
	return artifact.Object{}, errors.New("not implemented")
}

func (s *inlineStorage) Resolve(uri string) (artifact.Object, bool) {
	if strings.HasPrefix(uri, s.prefix) {
		return artifact.Object{URL: uri, Visibility: artifact.Private}, true
	}
	if s.publicPrefix != "" && strings.HasPrefix(uri, s.publicPrefix) {
		return artifact.Object{URL: uri, Visibility: artifact.Public}, true
	}
	return artifact.Object{}, false
}

func (s *inlineStorage) Open(context.Context, artifact.Object) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.data)), nil
}

func TestQwenEditGenerate(t *testing.T) {
	png := []byte("qwen-edit-png")
	var gotHeaders http.Header
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/oss/tmp.png", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(png) })
	var srv *httptest.Server
	mux.HandleFunc("POST /api/v1/services/aigc/multimodal-generation/generation", func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": map[string]any{"choices": []map[string]any{{
				"message": map[string]any{"content": []map[string]any{{"image": srv.URL + "/oss/tmp.png"}}},
			}}},
		})
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	q, err := NewQwenEdit(QwenEditConfig{
		BaseURL: srv.URL + "/api/v1",
		Model:   "qwen-image-edit-plus-2025-10-30",
		Headers: map[string]string{
			"x-api-key":      "secret",
			"x-ks-user-key":  "kling-lingeng-69865",
			"x-ks-llm-model": "qwen-image-edit-plus-2025-10-30",
			"x-ks-biz-scene": "online",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	img, err := q.Edit(context.Background(), EditRequest{
		Prompt:          "把卡通形象放到城市照片里",
		ReferenceImages: []string{"http://ref/1.png", "http://ref/2.png"},
		AspectRatio:     "1:1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(img.Data) != string(png) {
		t.Fatalf("img.Data = %q", img.Data)
	}

	// 网关头逐条按原样下发
	if gotHeaders.Get("x-api-key") != "secret" || gotHeaders.Get("x-ks-user-key") != "kling-lingeng-69865" ||
		gotHeaders.Get("x-ks-llm-model") != "qwen-image-edit-plus-2025-10-30" || gotHeaders.Get("x-ks-biz-scene") != "online" {
		t.Fatalf("headers = %v", gotHeaders)
	}
	if gotHeaders.Get("Authorization") != "" {
		t.Fatalf("qwenedit must not send Bearer auth, got %q", gotHeaders.Get("Authorization"))
	}

	// 请求体：model + input.messages + parameters
	if gotBody["model"] != "qwen-image-edit-plus-2025-10-30" {
		t.Fatalf("model = %v", gotBody["model"])
	}
	params, _ := gotBody["parameters"].(map[string]any)
	if params["size"] != "1328*1328" || params["prompt_extend"] != true || params["watermark"] != false {
		t.Fatalf("parameters = %v", params)
	}
	// content：两段 image 排在 text 之前
	raw, _ := json.Marshal(gotBody["input"])
	s := string(raw)
	if !strings.Contains(s, `"image":"http://ref/1.png"`) || !strings.Contains(s, `"image":"http://ref/2.png"`) {
		t.Fatalf("reference images not in content: %s", s)
	}
	if !strings.Contains(s, `"text":"把卡通形象放到城市照片里"`) {
		t.Fatalf("prompt text missing: %s", s)
	}
	if strings.Index(s, `"image"`) > strings.Index(s, `"text"`) {
		t.Fatalf("image segments should precede text: %s", s)
	}
}

func TestQwenEditInlineReferenceImage(t *testing.T) {
	blobBytes := []byte("internal-blob-png")
	var gotBody map[string]any
	mux := http.NewServeMux()
	// 内网 blob：私有产物由 artifact.Storage 读取转 base64（此路由留作占位，实际字节由 stub 直供）
	mux.HandleFunc("/user-x/tenant/img.png", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(blobBytes) })
	mux.HandleFunc("POST /api/v1/services/aigc/multimodal-generation/generation", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": map[string]any{"choices": []map[string]any{{
				"message": map[string]any{"content": []map[string]any{{"image": "data:noop"}}},
			}}},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	q, err := NewQwenEdit(QwenEditConfig{
		BaseURL: srv.URL + "/api/v1",
		Model:   "m",
		Headers: map[string]string{"x-api-key": "k"},
		Artifacts: &inlineStorage{
			prefix: srv.URL + "/user-x/", publicPrefix: "https://public.example/", data: blobBytes,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 一张内网 blob（内嵌 base64）+ 一张公网 URL（透传）
	_, _ = q.Edit(context.Background(), EditRequest{
		Prompt:          "p",
		ReferenceImages: []string{srv.URL + "/user-x/tenant/img.png", "https://public.example/pic.png"},
	})
	raw, _ := json.Marshal(gotBody["input"])
	s := string(raw)
	wantB64 := "data:image/png;base64," + base64.StdEncoding.EncodeToString(blobBytes)
	if !strings.Contains(s, wantB64) {
		t.Fatalf("internal blob not inlined as data URI: %s", s)
	}
	if !strings.Contains(s, "https://public.example/pic.png") {
		t.Fatalf("public URL should pass through: %s", s)
	}
	if _, err := q.Edit(context.Background(), EditRequest{
		Prompt: "p", ReferenceImages: []string{"http://169.254.169.254/latest/meta-data"},
	}); err == nil || !strings.Contains(err.Error(), "untrusted reference URL") {
		t.Fatalf("expected unmanaged URL rejection, got %v", err)
	}
}

func TestQwenEditNoSizeWhenNoAspectRatio(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": map[string]any{"choices": []map[string]any{{
				"message": map[string]any{"content": []map[string]any{{"image": "data:noop"}}},
			}}},
		})
	}))
	t.Cleanup(srv.Close)
	// download 会失败（data: 非 http），只关心请求体是否带 size
	q, _ := NewQwenEdit(QwenEditConfig{
		BaseURL: srv.URL, Model: "m", Headers: map[string]string{"x-api-key": "k"},
	})
	_, _ = q.Edit(context.Background(), EditRequest{Prompt: "p", ReferenceImages: []string{"http://r/1.png"}})
	params, _ := gotBody["parameters"].(map[string]any)
	if _, ok := params["size"]; ok {
		t.Fatalf("size should be omitted when aspectRatio empty: %v", params)
	}
}

func TestQwenEditRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "DataInspectionFailed", "message": "内容审核未通过"})
	}))
	t.Cleanup(srv.Close)
	q, _ := NewQwenEdit(QwenEditConfig{BaseURL: srv.URL, Model: "m", Headers: map[string]string{"x-api-key": "k"}})
	if _, err := q.Edit(context.Background(), EditRequest{Prompt: "p", ReferenceImages: []string{"http://r/1.png"}}); err == nil ||
		!strings.Contains(err.Error(), "rejected") {
		t.Fatalf("expected rejection, got %v", err)
	}
}

func TestNewQwenEditValidation(t *testing.T) {
	cases := []QwenEditConfig{
		{Model: "m", Headers: map[string]string{"x-api-key": "k"}},          // 缺 baseUrl
		{BaseURL: "http://x", Headers: map[string]string{"x-api-key": "k"}}, // 缺 model
		{BaseURL: "http://x", Model: "m"},                                   // 缺 headers
		{BaseURL: "relative", Model: "m", Headers: map[string]string{"x-api-key": "k"}},
	}
	for i, c := range cases {
		if _, err := NewQwenEdit(c); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}

func TestQwenEditRequiresReferenceImage(t *testing.T) {
	q, _ := NewQwenEdit(QwenEditConfig{BaseURL: "http://x", Model: "m", Headers: map[string]string{"x-api-key": "k"}})
	if _, err := q.Edit(context.Background(), EditRequest{Prompt: "p"}); err == nil {
		t.Fatal("expected error without reference images")
	}
	if _, err := q.Edit(context.Background(), EditRequest{ReferenceImages: []string{"x"}}); err == nil {
		t.Fatal("expected error without prompt")
	}
	if _, err := q.Edit(context.Background(), EditRequest{
		Prompt: "p", ReferenceImages: []string{" "},
	}); err == nil {
		t.Fatal("expected error for blank reference image")
	}
	if _, err := q.Edit(context.Background(), EditRequest{
		Prompt: "p", ReferenceImages: []string{"http://x"}, AspectRatio: "2:1",
	}); err == nil {
		t.Fatal("expected error for unsupported aspect ratio")
	}
}
