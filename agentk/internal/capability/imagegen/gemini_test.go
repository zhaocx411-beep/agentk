package imagegen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentk/internal/artifact"
)

const geminiBasePath = "/normal/tenant/v1beta1/projects/p/locations/global/publishers/google"

type geminiGenRequest struct {
	Contents []struct {
		Role  string `json:"role"`
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"contents"`
	GenerationConfig struct {
		ResponseModalities []string `json:"responseModalities"`
		ImageConfig        struct {
			AspectRatio string `json:"aspectRatio"`
			ImageSize   string `json:"imageSize"`
		} `json:"imageConfig"`
	} `json:"generationConfig"`
}

func newGeminiImageServer(t *testing.T, status int, body string, capture *geminiGenRequest, headers *http.Header) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if headers != nil {
			*headers = r.Header.Clone()
		}
		if capture != nil {
			if err := json.NewDecoder(r.Body).Decode(capture); err != nil {
				t.Errorf("decode request: %v", err)
			}
		}
		if r.URL.Path != geminiBasePath+"/models/gemini-image-test:generateContent" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGeminiGenerate(t *testing.T) {
	var got geminiGenRequest
	var headers http.Header
	png := base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	// 响应图文交错：模型常在图前后各说一句，生图只要那张图
	body := `{"candidates":[{"content":{"parts":[` +
		`{"text":"这是你要的猫"},` +
		`{"inlineData":{"mimeType":"image/png","data":"` + png + `"}}` +
		`]},"finishReason":"STOP"}]}`
	srv := newGeminiImageServer(t, http.StatusOK, body, &got, &headers)

	client, err := NewGemini(GeminiConfig{
		BaseURL:   srv.URL + geminiBasePath,
		Model:     "gemini-image-test",
		ImageSize: "2K",
		Headers:   map[string]string{"x-api-key": "k", "x-ks-biz-scene": "offline"},
		Client:    srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	img, err := client.Generate(context.Background(), Request{Prompt: "一只猫", AspectRatio: "16:9"})
	if err != nil {
		t.Fatal(err)
	}
	if headers.Get("x-api-key") != "k" || headers.Get("Content-Type") != "application/json" {
		t.Fatalf("headers = %v", headers)
	}
	if len(got.Contents) != 1 || len(got.Contents[0].Parts) != 1 || got.Contents[0].Parts[0].Text != "一只猫" {
		t.Fatalf("contents = %+v", got.Contents)
	}
	// TEXT 必须和 IMAGE 一起给：只声明 IMAGE 时上游回 200 但 parts 为空
	if len(got.GenerationConfig.ResponseModalities) != 2 ||
		got.GenerationConfig.ResponseModalities[0] != "TEXT" ||
		got.GenerationConfig.ResponseModalities[1] != "IMAGE" {
		t.Fatalf("responseModalities = %v", got.GenerationConfig.ResponseModalities)
	}
	// 画幅不换算成像素 size：Gemini 用 aspectRatio + imageSize 两个独立参数。
	if got.GenerationConfig.ImageConfig.AspectRatio != "16:9" ||
		got.GenerationConfig.ImageConfig.ImageSize != "2K" {
		t.Fatalf("imageConfig = %+v", got.GenerationConfig.ImageConfig)
	}
	if string(img.Data) != "png-bytes" || img.MIME != "image/png" {
		t.Fatalf("image = %q (%s)", img.Data, img.MIME)
	}
	if img.Params["aspectRatio"] != "16:9" || img.Params["imageSize"] != "2K" {
		t.Fatalf("params = %+v", img.Params)
	}
}

func TestGeminiGenerateDefaults(t *testing.T) {
	var got geminiGenRequest
	png := base64.StdEncoding.EncodeToString([]byte("x"))
	srv := newGeminiImageServer(t, http.StatusOK,
		`{"candidates":[{"content":{"parts":[{"inlineData":{"data":"`+png+`"}}]}}]}`, &got, nil)

	client, err := NewGemini(GeminiConfig{
		BaseURL: srv.URL + geminiBasePath, Model: "gemini-image-test",
		Headers: map[string]string{"x-api-key": "k"}, Client: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	img, err := client.Generate(context.Background(), Request{Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if got.GenerationConfig.ImageConfig.AspectRatio != "1:1" ||
		got.GenerationConfig.ImageConfig.ImageSize != DefaultGeminiImageSize {
		t.Fatalf("defaults not applied: %+v", got.GenerationConfig.ImageConfig)
	}
	// 上游未返回 mimeType 时默认使用 PNG。
	if img.MIME != "image/png" {
		t.Fatalf("mime = %q", img.MIME)
	}
}

func TestGeminiGenerateFailures(t *testing.T) {
	t.Run("无图片段带出终止原因", func(t *testing.T) {
		srv := newGeminiImageServer(t, http.StatusOK,
			`{"candidates":[{"content":{"parts":[{"text":"抱歉"}]},"finishReason":"IMAGE_SAFETY"}],`+
				`"promptFeedback":{"blockReason":"PROHIBITED_CONTENT"}}`, nil, nil)
		client := mustGemini(t, srv)
		_, err := client.Generate(context.Background(), Request{Prompt: "p"})
		if err == nil || !strings.Contains(err.Error(), "IMAGE_SAFETY") ||
			!strings.Contains(err.Error(), "PROHIBITED_CONTENT") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("HTTP 状态码", func(t *testing.T) {
		srv := newGeminiImageServer(t, http.StatusBadRequest, `{"error":{"message":"bad model"}}`, nil, nil)
		client := mustGemini(t, srv)
		if _, err := client.Generate(context.Background(), Request{Prompt: "p"}); err == nil ||
			!strings.Contains(err.Error(), "HTTP 400") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("200 带 error 体", func(t *testing.T) {
		srv := newGeminiImageServer(t, http.StatusOK, `{"error":{"message":"quota exhausted"}}`, nil, nil)
		client := mustGemini(t, srv)
		if _, err := client.Generate(context.Background(), Request{Prompt: "p"}); err == nil ||
			!strings.Contains(err.Error(), "quota exhausted") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("入参校验", func(t *testing.T) {
		srv := newGeminiImageServer(t, http.StatusOK, `{}`, nil, nil)
		client := mustGemini(t, srv)
		if _, err := client.Generate(context.Background(), Request{Prompt: " "}); err == nil {
			t.Fatal("expected rejection for blank prompt")
		}
		if _, err := client.Generate(context.Background(), Request{Prompt: "p", AspectRatio: "7:3"}); err == nil {
			t.Fatal("expected rejection for unsupported aspect ratio")
		}
	})
}

// geminiEditRequest 解出 Edit 发出的请求：parts 里既有 inlineData 也有 text，
// imageConfig 的 aspectRatio 可能整个不下发（编辑时交给模型按输入图定）。
type geminiEditRequest struct {
	Contents []struct {
		Parts []struct {
			Text       string `json:"text"`
			InlineData *struct {
				MIMEType string `json:"mimeType"`
				Data     string `json:"data"`
			} `json:"inlineData"`
		} `json:"parts"`
	} `json:"contents"`
	GenerationConfig struct {
		ImageConfig map[string]any `json:"imageConfig"`
	} `json:"generationConfig"`
}

// pngBytes 一个最小 PNG 头，够 http.DetectContentType 认成 image/png。
var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), []byte("rest-of-the-file")...)

func TestGeminiEdit(t *testing.T) {
	var got geminiEditRequest
	out := base64.StdEncoding.EncodeToString([]byte("edited"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_, _ = io.WriteString(w,
			`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"`+out+`"}}]}}]}`)
	}))
	t.Cleanup(srv.Close)

	client, err := NewGemini(GeminiConfig{
		BaseURL: srv.URL + geminiBasePath, Model: "gemini-image-test", ImageSize: "2K",
		Headers: map[string]string{"x-api-key": "k"}, Client: srv.Client(),
		Artifacts: &inlineStorage{prefix: "http://blob.test/", data: pngBytes},
	})
	if err != nil {
		t.Fatal(err)
	}

	img, err := client.Edit(context.Background(), EditRequest{
		Prompt:          "换成卡通风格",
		ReferenceImages: []string{"http://blob.test/a.png", "http://blob.test/b.png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 参考图在前、指令在后（官方示例 contents=[image, prompt] 的顺序）
	parts := got.Contents[0].Parts
	if len(parts) != 3 {
		t.Fatalf("parts = %+v", parts)
	}
	for i := range 2 {
		if parts[i].InlineData == nil || parts[i].InlineData.MIMEType != "image/png" {
			t.Fatalf("part %d 不是内联图片: %+v", i, parts[i])
		}
		if parts[i].InlineData.Data != base64.StdEncoding.EncodeToString(pngBytes) {
			t.Fatalf("part %d 字节不对", i)
		}
	}
	if parts[2].Text != "换成卡通风格" {
		t.Fatalf("指令段 = %+v", parts[2])
	}
	// 画幅未指定：整个 aspectRatio 键不下发，清晰度仍按条目配置
	if _, ok := got.GenerationConfig.ImageConfig["aspectRatio"]; ok {
		t.Fatalf("未指定画幅时不该下发 aspectRatio: %+v", got.GenerationConfig.ImageConfig)
	}
	if got.GenerationConfig.ImageConfig["imageSize"] != "2K" {
		t.Fatalf("imageConfig = %+v", got.GenerationConfig.ImageConfig)
	}
	if string(img.Data) != "edited" {
		t.Fatalf("image = %q", img.Data)
	}
	// 未指定画幅时回报 auto，不伪造具体比例。
	if img.Params["aspectRatio"] != "auto" || img.Params["imageSize"] != "2K" {
		t.Fatalf("params = %+v", img.Params)
	}
}

func TestGeminiEditAspectRatioForwarded(t *testing.T) {
	var got geminiEditRequest
	out := base64.StdEncoding.EncodeToString([]byte("x"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"inlineData":{"data":"`+out+`"}}]}}]}`)
	}))
	t.Cleanup(srv.Close)
	client, err := NewGemini(GeminiConfig{
		BaseURL: srv.URL + geminiBasePath, Model: "gemini-image-test",
		Headers: map[string]string{"x-api-key": "k"}, Client: srv.Client(),
		Artifacts: &inlineStorage{prefix: "http://blob.test/", data: pngBytes},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Edit(context.Background(), EditRequest{
		Prompt: "p", ReferenceImages: []string{"http://blob.test/a.png"}, AspectRatio: "16:9",
	}); err != nil {
		t.Fatal(err)
	}
	if got.GenerationConfig.ImageConfig["aspectRatio"] != "16:9" {
		t.Fatalf("imageConfig = %+v", got.GenerationConfig.ImageConfig)
	}
}

func TestGeminiEditRejects(t *testing.T) {
	srv := newGeminiImageServer(t, http.StatusOK, `{}`, nil, nil)
	withStorage := func(s artifact.Storage) *Gemini {
		client, err := NewGemini(GeminiConfig{
			BaseURL: srv.URL + geminiBasePath, Model: "gemini-image-test",
			Headers: map[string]string{"x-api-key": "k"}, Client: srv.Client(), Artifacts: s,
		})
		if err != nil {
			t.Fatal(err)
		}
		return client
	}
	managed := &inlineStorage{prefix: "http://blob.test/", data: pngBytes}

	// 拒绝不受管 URL：该步骤由服务端代取字节，放开会形成 SSRF 能力。
	if _, err := withStorage(managed).Edit(context.Background(), EditRequest{
		Prompt: "p", ReferenceImages: []string{"http://169.254.169.254/latest/meta-data"},
	}); err == nil || !strings.Contains(err.Error(), "untrusted reference URL") {
		t.Fatalf("expected unmanaged URL rejection, got %v", err)
	}
	// 没有产物存储时不能悄悄放行
	if _, err := withStorage(nil).Edit(context.Background(), EditRequest{
		Prompt: "p", ReferenceImages: []string{"http://blob.test/a.png"},
	}); err == nil || !strings.Contains(err.Error(), "artifact storage") {
		t.Fatalf("expected missing-storage error, got %v", err)
	}
	// 拒绝无法识别的字节类型，不伪造 mimeType 再让上游返回含糊的 400。
	if _, err := withStorage(&inlineStorage{
		prefix: "http://blob.test/", data: []byte("not-an-image"),
	}).Edit(context.Background(), EditRequest{
		Prompt: "p", ReferenceImages: []string{"http://blob.test/a.txt"},
	}); err == nil || !strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("expected unsupported-type rejection, got %v", err)
	}
	for name, req := range map[string]EditRequest{
		"缺指令":   {ReferenceImages: []string{"http://blob.test/a.png"}},
		"缺参考图":  {Prompt: "p"},
		"参考图为空": {Prompt: "p", ReferenceImages: []string{" "}},
		"画幅不支持": {Prompt: "p", ReferenceImages: []string{"http://blob.test/a.png"}, AspectRatio: "7:3"},
	} {
		if _, err := withStorage(managed).Edit(context.Background(), req); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestNewGeminiValidatesConfig(t *testing.T) {
	base := GeminiConfig{
		BaseURL: "https://gw.test/publishers/google",
		Model:   "m",
		Headers: map[string]string{"x-api-key": "k"},
	}
	if _, err := NewGemini(base); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*GeminiConfig){
		"缺 baseUrl":     func(c *GeminiConfig) { c.BaseURL = "" },
		"baseUrl 相对":    func(c *GeminiConfig) { c.BaseURL = "/publishers/google" },
		"缺模型标识":         func(c *GeminiConfig) { c.Model = "" },
		"缺网关头":          func(c *GeminiConfig) { c.Headers = nil },
		"imageSize 未知":  func(c *GeminiConfig) { c.ImageSize = "8K" },
		"imageSize 小写k": func(c *GeminiConfig) { c.ImageSize = "2k" }, // 小写会被上游静默忽略
		"代理地址不合法":       func(c *GeminiConfig) { c.ProxyURL = "not-a-url" },
	} {
		cfg := base
		mutate(&cfg)
		if _, err := NewGemini(cfg); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func mustGemini(t *testing.T, srv *httptest.Server) *Gemini {
	t.Helper()
	client, err := NewGemini(GeminiConfig{
		BaseURL: srv.URL + geminiBasePath, Model: "gemini-image-test",
		Headers: map[string]string{"x-api-key": "k"}, Client: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}
