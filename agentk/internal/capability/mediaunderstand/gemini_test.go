package mediaunderstand

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentk/internal/artifact"
)

// mp4Bytes 是一段能被 http.DetectContentType 认出的最小 mp4 头（ftyp box）。
func mp4Bytes() []byte { return []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom") }

func pngBytes() []byte {
	return append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)
}

// fakeStorage 是按 URL 索引的受管产物存储。
type fakeStorage struct {
	objects map[string]artifact.Object
	data    map[string][]byte
	openErr error
}

func (f *fakeStorage) Put(context.Context, string, []byte, string) (artifact.Object, error) {
	return artifact.Object{}, fmt.Errorf("not implemented")
}

func (f *fakeStorage) Resolve(uri string) (artifact.Object, bool) {
	object, ok := f.objects[uri]
	return object, ok
}

func (f *fakeStorage) Open(_ context.Context, object artifact.Object) (io.ReadCloser, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	return io.NopCloser(bytes.NewReader(f.data[object.URL])), nil
}

const geminiBasePath = "/normal/tenant/v1beta1/projects/p/locations/global/publishers/google"

type geminiRequestBody struct {
	Contents []struct {
		Role  string `json:"role"`
		Parts []struct {
			Text       string `json:"text"`
			InlineData *struct {
				MIMEType string `json:"mimeType"`
				Data     string `json:"data"`
			} `json:"inlineData"`
			FileData *struct {
				MIMEType string `json:"mimeType"`
				FileURI  string `json:"fileUri"`
			} `json:"fileData"`
			VideoMetadata *struct {
				StartOffset string  `json:"startOffset"`
				EndOffset   string  `json:"endOffset"`
				FPS         float64 `json:"fps"`
			} `json:"videoMetadata"`
		} `json:"parts"`
	} `json:"contents"`
}

// TestGeminiUnderstandMixedDelivery 覆盖 Gemini provider 的主链路：一段公网视频（交给
// Gemini 自己去抓，走 fileData）加一张受管私有图片（只有本进程读得到，内联下发），
// 混在一次调用里各自带对的 MIME，问题跟在最后。
func TestGeminiUnderstandMixedDelivery(t *testing.T) {
	var gotPath string
	var gotHeaders http.Header
	var gotBody geminiRequestBody

	mux := http.NewServeMux()
	mux.HandleFunc("/cdn/clip.mp4", mediaHandler("video/mp4", mp4Bytes(), true))
	mux.HandleFunc(geminiBasePath+"/models/gemini-test:generateContent", func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotHeaders = r.URL.Path, r.Header.Clone()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_, _ = io.WriteString(w,
			`{"candidates":[{"content":{"parts":[{"text":"一只猫"},{"text":"在跳"}]},"finishReason":"STOP"}]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	frameURL := "http://managed.test/files/org_1/20260728/img_x.png"
	storage := &fakeStorage{
		objects: map[string]artifact.Object{frameURL: {
			Key: "org_1/20260728/img_x.png", URL: frameURL, Visibility: artifact.Private,
		}},
		data: map[string][]byte{frameURL: pngBytes()},
	}

	client, err := NewGemini(GeminiConfig{
		BaseURL:     srv.URL + geminiBasePath,
		Model:       "gemini-test",
		Headers:     map[string]string{"x-api-key": "k", "x-ks-llm-model": "gemini-test"},
		Client:      srv.Client(),
		Artifacts:   storage,
		RemoteFetch: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	clipURL := srv.URL + "/cdn/clip.mp4"
	got, err := client.Understand(context.Background(), Request{
		MediaURLs: []string{clipURL, frameURL},
		Prompt:    "这段视频和这张图风格一致吗",
	})
	if err != nil {
		t.Fatal(err)
	}

	if gotPath != geminiBasePath+"/models/gemini-test:generateContent" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotHeaders.Get("x-api-key") != "k" || gotHeaders.Get("Content-Type") != "application/json" {
		t.Fatalf("headers = %v", gotHeaders)
	}
	if len(gotBody.Contents) != 1 || gotBody.Contents[0].Role != "user" {
		t.Fatalf("contents = %+v", gotBody.Contents)
	}
	parts := gotBody.Contents[0].Parts
	if len(parts) != 3 {
		t.Fatalf("want 2 media parts + 1 text part, got %d: %+v", len(parts), parts)
	}
	// 公网视频只给 URL，字节不经过我们
	if parts[0].FileData == nil || parts[0].FileData.FileURI != clipURL ||
		parts[0].FileData.MIMEType != "video/mp4" || parts[0].InlineData != nil {
		t.Fatalf("part 0 = %+v; want fileData for the public clip", parts[0])
	}
	// 受管私有图片上游抓不到，内联
	if parts[1].InlineData == nil || parts[1].InlineData.MIMEType != "image/png" ||
		parts[1].FileData != nil {
		t.Fatalf("part 1 = %+v; want inlineData for the private artifact", parts[1])
	}
	decoded, err := base64.StdEncoding.DecodeString(parts[1].InlineData.Data)
	if err != nil || !bytes.Equal(decoded, pngBytes()) {
		t.Fatalf("inline png = %q (%v)", decoded, err)
	}
	// 媒体在前、问题在后
	if parts[2].Text != "这段视频和这张图风格一致吗" ||
		parts[2].InlineData != nil || parts[2].FileData != nil {
		t.Fatalf("part 2 = %+v", parts[2])
	}

	// 多个文本段拼成一段回答
	if got.Text != "一只猫在跳" {
		t.Fatalf("text = %q", got.Text)
	}
	media, ok := got.Params["media"].([]map[string]any)
	if !ok || len(media) != 2 {
		t.Fatalf("params = %+v", got.Params)
	}
	// 发送方式需要回报：同一段素材走 URL 还是内联会受到不同的上游限制
	if media[0]["kind"] != "video" || media[0]["type"] != "video/mp4" || media[0]["delivery"] != "url" {
		t.Fatalf("media[0] = %+v", media[0])
	}
	if media[1]["kind"] != "image" || media[1]["delivery"] != "inline" ||
		media[1]["bytes"] != len(pngBytes()) {
		t.Fatalf("media[1] = %+v", media[1])
	}
}

func TestGeminiUnderstandVideoSegmentUsesProviderMetadata(t *testing.T) {
	var gotBody geminiRequestBody
	mux := http.NewServeMux()
	mux.HandleFunc("/cdn/clip.mp4", mediaHandler("video/mp4", mp4Bytes(), true))
	mux.HandleFunc(geminiBasePath+"/models/gemini-test:generateContent", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_, _ = io.WriteString(w,
			`{"candidates":[{"content":{"parts":[{"text":"区间分析"}]},"finishReason":"STOP"}]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := NewGemini(GeminiConfig{
		BaseURL: srv.URL + geminiBasePath, Model: "gemini-test",
		Headers: map[string]string{"x-api-key": "k"}, Client: srv.Client(), RemoteFetch: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Understand(context.Background(), Request{
		MediaURLs: []string{srv.URL + "/cdn/clip.mp4"},
		Prompt:    "分析画面和音轨",
		VideoSegment: &VideoSegment{
			StartSeconds: 12.5,
			EndSeconds:   19.75,
			FPS:          4,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	parts := gotBody.Contents[0].Parts
	if len(parts) != 2 || parts[0].FileData == nil || parts[0].VideoMetadata == nil {
		t.Fatalf("parts = %+v", parts)
	}
	metadata := parts[0].VideoMetadata
	if metadata.StartOffset != "12.5s" || metadata.EndOffset != "19.75s" || metadata.FPS != 4 {
		t.Fatalf("videoMetadata = %+v", metadata)
	}
	segment, ok := got.Params["videoSegment"].(map[string]any)
	if !ok || segment["startSeconds"] != 12.5 || segment["endSeconds"] != 19.75 ||
		segment["fps"] != float64(4) {
		t.Fatalf("params = %+v", got.Params)
	}
}

func TestGeminiUnderstandSmallInlineVideoSegmentKeepsMetadata(t *testing.T) {
	var gotBody geminiRequestBody
	mux := http.NewServeMux()
	mux.HandleFunc("/clip.mp4", mediaHandler("video/mp4", mp4Bytes(), true))
	mux.HandleFunc(geminiBasePath+"/models/gemini-test:generateContent", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = io.WriteString(w,
			`{"candidates":[{"content":{"parts":[{"text":"区间分析"}]},"finishReason":"STOP"}]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client, err := NewGemini(GeminiConfig{
		BaseURL: srv.URL + geminiBasePath, Model: "gemini-test",
		Headers: map[string]string{"x-api-key": "k"}, Client: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Understand(context.Background(), Request{
		MediaURLs: []string{srv.URL + "/clip.mp4"}, Prompt: "分析",
		VideoSegment: &VideoSegment{StartSeconds: 0, EndSeconds: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	part := gotBody.Contents[0].Parts[0]
	if part.InlineData == nil || part.FileData != nil || part.VideoMetadata == nil ||
		part.VideoMetadata.StartOffset != "0s" || part.VideoMetadata.EndOffset != "3s" ||
		part.VideoMetadata.FPS != 0 {
		t.Fatalf("part = %+v", part)
	}
}

func TestGeminiUnderstandVideoSegmentRejectsNonVideo(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/frame.png", mediaHandler("image/png", pngBytes(), true))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client, err := NewGemini(GeminiConfig{
		BaseURL: srv.URL + geminiBasePath, Model: "gemini-test",
		Headers: map[string]string{"x-api-key": "k"}, Client: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Understand(context.Background(), Request{
		MediaURLs: []string{srv.URL + "/frame.png"}, Prompt: "p",
		VideoSegment: &VideoSegment{StartSeconds: 0, EndSeconds: 1},
	})
	if err == nil || !strings.Contains(err.Error(), "exactly one video") {
		t.Fatalf("err = %v", err)
	}
}

// 缺省不把 URL 交给上游：判断它能不能被上游取到我们这侧没有依据，
// 猜错的代价是一次用户可见的失败。连 HEAD 探测都不该发。
func TestGeminiUnderstandDefaultsToInline(t *testing.T) {
	var gotBody geminiRequestBody
	var methods []string

	mux := http.NewServeMux()
	mux.HandleFunc("/cdn/clip.mp4", func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		mediaHandler("video/mp4", mp4Bytes(), true)(w, r)
	})
	mux.HandleFunc(geminiBasePath+"/models/gemini-test:generateContent", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"好的"}]}}]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := newGeminiClient(t, srv, nil) // RemoteFetch 缺省 false
	if _, err := client.Understand(context.Background(), Request{
		MediaURLs: []string{srv.URL + "/cdn/clip.mp4"}, Prompt: "描述一下",
	}); err != nil {
		t.Fatal(err)
	}
	parts := gotBody.Contents[0].Parts
	if parts[0].InlineData == nil || parts[0].FileData != nil {
		t.Fatalf("part 0 = %+v; want inlineData by default", parts[0])
	}
	for _, m := range methods {
		if m == http.MethodHead {
			t.Fatalf("no HEAD probe should be issued when remote fetch is off: %v", methods)
		}
	}
}

// 打开 RemoteFetch 后上游仍取不到那个 URL（内网地址、robots.txt）时自动改内联重来：
// 这类失败对模型没有可操作性，而内联本来就走得通。
func TestGeminiUnderstandRetriesInlineWhenUpstreamCannotFetchURL(t *testing.T) {
	var sawFileData, sawInline bool
	calls := 0

	mux := http.NewServeMux()
	mux.HandleFunc("/cdn/clip.mp4", mediaHandler("video/mp4", mp4Bytes(), true))
	mux.HandleFunc(geminiBasePath+"/models/gemini-test:generateContent", func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body geminiRequestBody
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, p := range body.Contents[0].Parts {
			if p.FileData != nil {
				sawFileData = true
			}
			if p.InlineData != nil {
				sawInline = true
			}
		}
		if sawFileData && calls == 1 {
			// Vertex 无法获取 URL 时的真实响应格式
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"code":400,"status":"INVALID_ARGUMENT",`+
				`"message":"Cannot fetch content from the provided URL. Status: URL_ERROR-ERROR_DNS"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"内联这次成功了"}]}}]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := NewGemini(GeminiConfig{
		BaseURL: srv.URL + geminiBasePath, Model: "gemini-test",
		Headers: map[string]string{"x-api-key": "k"}, Client: srv.Client(), RemoteFetch: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Understand(context.Background(), Request{
		MediaURLs: []string{srv.URL + "/cdn/clip.mp4"}, Prompt: "描述一下",
	})
	if err != nil {
		t.Fatalf("should have recovered by inlining: %v", err)
	}
	if got.Text != "内联这次成功了" || calls != 2 || !sawFileData || !sawInline {
		t.Fatalf("text=%q calls=%d fileData=%v inline=%v", got.Text, calls, sawFileData, sawInline)
	}
	// 重试后的回报要反映实际发送方式，便于事后定位问题
	media := got.Params["media"].([]map[string]any)
	if media[0]["delivery"] != "inline" {
		t.Fatalf("params must report the delivery actually used: %+v", media[0])
	}
}

// 回落只补那几段交上游自取的，已经内联过的沿用原字节——重下一遍不改变结果，
// 只多给一次失败机会。
func TestGeminiUnderstandFallbackReusesInlinedBytes(t *testing.T) {
	var clipGets, frameGets int
	mux := http.NewServeMux()
	mux.HandleFunc("/clip.mp4", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			clipGets++
		}
		mediaHandler("video/mp4", mp4Bytes(), true)(w, r)
	})
	// HEAD 405 → 第一轮就只能内联
	mux.HandleFunc("/frame.png", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			frameGets++
		}
		mediaHandler("image/png", pngBytes(), false)(w, r)
	})
	calls := 0
	mux.HandleFunc(geminiBasePath+"/models/gemini-test:generateContent", func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"Cannot fetch content from the provided URL"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := NewGemini(GeminiConfig{
		BaseURL: srv.URL + geminiBasePath, Model: "gemini-test",
		Headers: map[string]string{"x-api-key": "k"}, Client: srv.Client(), RemoteFetch: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Understand(context.Background(), Request{
		MediaURLs: []string{srv.URL + "/clip.mp4", srv.URL + "/frame.png"}, Prompt: "p",
	}); err != nil {
		t.Fatal(err)
	}
	// 视频第一轮走 URL 没下载、回落时下一次；图片第一轮就内联了，不该再下第二次
	if clipGets != 1 {
		t.Fatalf("clip downloaded %d times, want 1 (only on fallback)", clipGets)
	}
	if frameGets != 1 {
		t.Fatalf("already-inlined frame re-downloaded (%d GETs); fallback must reuse its bytes", frameGets)
	}
}

// 回落本身也失败时，两个原因都要报出来：起因是上游取不到 URL，
// 但真正卡住的是内联这条退路。只报前者会把人引去查根本没问题的 URL 可达性。
func TestGeminiUnderstandFallbackFailureReportsBothCauses(t *testing.T) {
	served := false
	mux := http.NewServeMux()
	mux.HandleFunc("/clip.mp4", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && served {
			w.WriteHeader(http.StatusInternalServerError) // 回落下载时素材源挂了
			return
		}
		served = true
		mediaHandler("video/mp4", mp4Bytes(), true)(w, r)
	})
	mux.HandleFunc(geminiBasePath+"/models/gemini-test:generateContent", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"Cannot fetch content from the provided URL. Status: URL_ERROR-ERROR_DNS"}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := NewGemini(GeminiConfig{
		BaseURL: srv.URL + geminiBasePath, Model: "gemini-test",
		Headers: map[string]string{"x-api-key": "k"}, Client: srv.Client(), RemoteFetch: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Understand(context.Background(), Request{
		MediaURLs: []string{srv.URL + "/clip.mp4"}, Prompt: "p",
	})
	if err == nil {
		t.Fatal("expected an error when both paths fail")
	}
	if !strings.Contains(err.Error(), "URL_ERROR") {
		t.Fatalf("original cause lost: %v", err)
	}
	if !strings.Contains(err.Error(), "inline fallback also failed") {
		t.Fatalf("fallback cause not reported: %v", err)
	}
}

// 与 URL 获取无关的失败不应触发重试，否则只会增加一次无效的上游请求。
func TestGeminiUnderstandDoesNotRetryOtherFailures(t *testing.T) {
	calls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/cdn/clip.mp4", mediaHandler("video/mp4", mp4Bytes(), true))
	mux.HandleFunc(geminiBasePath+"/models/gemini-test:generateContent", func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"quota exhausted"}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := NewGemini(GeminiConfig{
		BaseURL: srv.URL + geminiBasePath, Model: "gemini-test",
		Headers: map[string]string{"x-api-key": "k"}, Client: srv.Client(), RemoteFetch: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Understand(context.Background(), Request{
		MediaURLs: []string{srv.URL + "/cdn/clip.mp4"}, Prompt: "p",
	}); err == nil {
		t.Fatal("expected the quota error to surface")
	}
	if calls != 1 {
		t.Fatalf("upstream called %d times; a non-fetch failure must not be retried", calls)
	}
}

// 空回答几乎都是被安全策略或长度上限截掉的：终止原因必须带出去，
// 否则模型只知道"失败了"，不知道原样重试没有意义。
func TestGeminiUnderstandSurfacesEmptyAnswerReason(t *testing.T) {
	srv := newGeminiServer(t, http.StatusOK,
		`{"candidates":[{"content":{"parts":[]},"finishReason":"SAFETY"}],"promptFeedback":{"blockReason":"PROHIBITED_CONTENT"}}`)
	client := newGeminiClient(t, srv, nil)

	_, err := client.Understand(context.Background(), Request{
		MediaURLs: []string{srv.URL + "/cdn/clip.mp4"}, Prompt: "描述一下",
	})
	if err == nil || !strings.Contains(err.Error(), "SAFETY") ||
		!strings.Contains(err.Error(), "PROHIBITED_CONTENT") {
		t.Fatalf("err = %v", err)
	}
}

func TestGeminiUnderstandUpstreamErrors(t *testing.T) {
	t.Run("HTTP 状态码", func(t *testing.T) {
		srv := newGeminiServer(t, http.StatusTooManyRequests, `{"error":{"message":"quota"}}`)
		client := newGeminiClient(t, srv, nil)
		_, err := client.Understand(context.Background(), Request{
			MediaURLs: []string{srv.URL + "/cdn/clip.mp4"}, Prompt: "p",
		})
		if err == nil || !strings.Contains(err.Error(), "HTTP 429") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("200 带 error 体", func(t *testing.T) {
		srv := newGeminiServer(t, http.StatusOK, `{"error":{"message":"model not found"}}`)
		client := newGeminiClient(t, srv, nil)
		_, err := client.Understand(context.Background(), Request{
			MediaURLs: []string{srv.URL + "/cdn/clip.mp4"}, Prompt: "p",
		})
		if err == nil || !strings.Contains(err.Error(), "model not found") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("素材取不回", func(t *testing.T) {
		srv := newGeminiServer(t, http.StatusOK, `{}`)
		client := newGeminiClient(t, srv, nil)
		_, err := client.Understand(context.Background(), Request{
			MediaURLs: []string{srv.URL + "/missing.mp4"}, Prompt: "p",
		})
		if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestGeminiUnderstandValidatesInput(t *testing.T) {
	srv := newGeminiServer(t, http.StatusOK, `{}`)
	client := newGeminiClient(t, srv, nil)
	for _, req := range []Request{
		{MediaURLs: []string{srv.URL + "/cdn/clip.mp4"}, Prompt: "  "},
		{MediaURLs: nil, Prompt: "p"},
		{MediaURLs: []string{"ftp://host/clip.mp4"}, Prompt: "p"},
		{MediaURLs: []string{"  "}, Prompt: "p"},
	} {
		if _, err := client.Understand(context.Background(), req); err == nil {
			t.Fatalf("expected rejection for %+v", req)
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
		"缺 baseUrl":  func(c *GeminiConfig) { c.BaseURL = "" },
		"baseUrl 相对": func(c *GeminiConfig) { c.BaseURL = "/publishers/google" },
		"缺模型标识":      func(c *GeminiConfig) { c.Model = "" },
		"缺网关头":       func(c *GeminiConfig) { c.Headers = nil },
		"代理地址不合法":    func(c *GeminiConfig) { c.ProxyURL = "not-a-url" },
	} {
		cfg := base
		mutate(&cfg)
		if _, err := NewGemini(cfg); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestValidatePreparedMediaAcceptsPDF(t *testing.T) {
	media, err := validatePreparedMedia([]Media{{
		URL: "managed.pdf", MIME: "application/pdf", Data: []byte("%PDF-test"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(media) != 1 || media[0].Kind != "document" || media[0].Bytes != len("%PDF-test") {
		t.Fatalf("prepared PDF = %#v", media)
	}
	if _, err := validatePreparedMedia([]Media{{
		MIME: "application/zip", Data: []byte("zip"),
	}}); err == nil {
		t.Fatal("unsupported prepared MIME must fail")
	}
}

// newGeminiServer 起一个假网关：/cdn/clip.mp4 供素材下载，其余路径当
// generateContent 端点并回给定响应。
func newGeminiServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/cdn/clip.mp4", mediaHandler("video/mp4", mp4Bytes(), true))
	mux.HandleFunc("/missing.mp4", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc(geminiBasePath+"/models/gemini-test:generateContent", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newGeminiClient(t *testing.T, srv *httptest.Server, storage artifact.Storage) *Gemini {
	t.Helper()
	client, err := NewGemini(GeminiConfig{
		BaseURL:   srv.URL + geminiBasePath,
		Model:     "gemini-test",
		Headers:   map[string]string{"x-api-key": "k"},
		Client:    srv.Client(),
		Artifacts: storage,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}
