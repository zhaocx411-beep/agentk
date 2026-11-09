package videogen

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.temporal.io/sdk/temporal"

	"agentk/internal/artifact"
	"agentk/internal/temporalx"
)

// fakeKling 最小可灵服务：记录提交请求，按脚本回任务状态。
type fakeKling struct {
	t          *testing.T
	lastSubmit map[string]any
	lastAuth   string
	lastPath   string
	lastQuery  string
	status     string // 轮询返回的 status
	videoURL   string
	statusMsg  string
	httpStatus int // 非 0 时所有请求直接回该状态码
}

func (f *fakeKling) handler() http.Handler {
	mux := http.NewServeMux()
	// 提交：POST /image-to-video/{model}
	mux.HandleFunc("POST /image-to-video/{model}", func(w http.ResponseWriter, r *http.Request) {
		if f.httpStatus != 0 {
			w.WriteHeader(f.httpStatus)
			return
		}
		f.lastAuth = r.Header.Get("Authorization")
		f.lastPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&f.lastSubmit); err != nil {
			f.t.Errorf("decode submit body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0, "data": map[string]any{"id": "task-1", "status": "submitted"},
		})
	})
	// 查询：GET /tasks?task_ids={id}
	mux.HandleFunc("GET /tasks", func(w http.ResponseWriter, r *http.Request) {
		if f.httpStatus != 0 {
			w.WriteHeader(f.httpStatus)
			return
		}
		f.lastAuth = r.Header.Get("Authorization")
		f.lastQuery = r.URL.RawQuery
		task := map[string]any{
			"id":      r.URL.Query().Get("task_ids"),
			"status":  f.status,
			"message": f.statusMsg,
		}
		if f.status == "succeeded" && f.videoURL != "" {
			task["outputs"] = []map[string]any{{"type": "video", "url": f.videoURL}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": []any{task}})
	})
	return mux
}

func newTestKling(t *testing.T, f *fakeKling) *KlingI2V {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	k, err := NewKlingI2V(KlingConfig{
		BaseURL: srv.URL, AccessKey: "ak-test", SecretKey: "sk-test", Model: "kling-3.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// submitContent 从提交体的 contents 里取指定 type 的素材项。
func submitContent(t *testing.T, body map[string]any, typ string) map[string]any {
	t.Helper()
	items, ok := body["contents"].([]any)
	if !ok {
		t.Fatalf("contents = %v", body["contents"])
	}
	for _, it := range items {
		m, ok := it.(map[string]any)
		if ok && m["type"] == typ {
			return m
		}
	}
	return nil
}

func submitSettings(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	s, ok := body["settings"].(map[string]any)
	if !ok {
		t.Fatalf("settings = %v", body["settings"])
	}
	return s
}

func TestKlingSubmit(t *testing.T) {
	f := &fakeKling{t: t}
	k := newTestKling(t, f)

	taskID, err := k.Submit(context.Background(), KlingI2VRequest{FirstFrame: "https://cdn/x.png", Prompt: "缓慢推近"})
	if err != nil {
		t.Fatal(err)
	}
	if taskID != "task-1" {
		t.Fatalf("taskID = %q", taskID)
	}
	// 模型进路径（固定标识 kling-3.0），不进请求体
	if f.lastPath != "/image-to-video/kling-3.0" {
		t.Fatalf("submit path = %q", f.lastPath)
	}
	// contents：prompt 文本 + 首帧图 url；无尾帧
	if c := submitContent(t, f.lastSubmit, "prompt"); c == nil || c["text"] != "缓慢推近" {
		t.Fatalf("prompt content = %v", c)
	}
	if c := submitContent(t, f.lastSubmit, "first_frame"); c == nil || c["url"] != "https://cdn/x.png" {
		t.Fatalf("first_frame content = %v", c)
	}
	if c := submitContent(t, f.lastSubmit, "last_frame"); c != nil {
		t.Fatalf("unexpected last_frame content = %v", c)
	}
	// settings：缺省 1080p / 5s / native / multi_shot=true
	s := submitSettings(t, f.lastSubmit)
	if s["resolution"] != "1080p" || s["duration"] != float64(5) || s["audio"] != "native" || s["multi_shot"] != true {
		t.Fatalf("settings = %v", s)
	}
	verifyJWT(t, f.lastAuth, "ak-test", "sk-test")

	// 覆写 resolution / duration / audio / 尾帧
	if _, err := k.Submit(context.Background(), KlingI2VRequest{
		FirstFrame: "u", LastFrame: "tail.png", Prompt: "p",
		Resolution: "4k", Duration: "10", Audio: "native",
	}); err != nil {
		t.Fatal(err)
	}
	s = submitSettings(t, f.lastSubmit)
	if s["resolution"] != "4k" || s["duration"] != float64(10) || s["audio"] != "native" {
		t.Fatalf("settings = %v", s)
	}
	if c := submitContent(t, f.lastSubmit, "last_frame"); c == nil || c["url"] != "tail.png" {
		t.Fatalf("last_frame content = %v", c)
	}

	// 坏时长：提交前报错，不打请求
	if _, err := k.Submit(context.Background(), KlingI2VRequest{FirstFrame: "u", Prompt: "p", Duration: "abc"}); err == nil {
		t.Fatal("expected error for invalid duration")
	}
	for _, request := range []KlingI2VRequest{
		{FirstFrame: "u", Prompt: "p", Duration: "2"},
		{FirstFrame: "u", Prompt: "p", Resolution: "480p"},
		{FirstFrame: "u", Prompt: "p", Audio: "surround"},
	} {
		if _, err := k.Submit(context.Background(), request); err == nil {
			t.Fatalf("expected validation error for %+v", request)
		}
	}

	// 缺参数直接拒绝，不打请求
	if _, err := k.Submit(context.Background(), KlingI2VRequest{Prompt: "p"}); err == nil {
		t.Fatal("expected error for missing image")
	}
	// 只有空白的尾帧等于没给：归一时抹掉，不进 contents
	if _, err := k.Submit(context.Background(), KlingI2VRequest{FirstFrame: "u", LastFrame: " ", Prompt: "p"}); err != nil {
		t.Fatal(err)
	}
	if c := submitContent(t, f.lastSubmit, "last_frame"); c != nil {
		t.Fatalf("blank last frame must be dropped, got %v", c)
	}
}

// TestKlingSubmitMultiShot 显式给 true 时下发 multi_shot。
func TestKlingSubmitMultiShot(t *testing.T) {
	f := &fakeKling{t: t}
	k := newTestKling(t, f)
	multiShot := true
	if _, err := k.Submit(context.Background(), KlingI2VRequest{
		FirstFrame: "u", Prompt: "镜头 1, 3, 推近; 镜头 2, 2, 拉远;", Duration: "5", MultiShot: &multiShot,
	}); err != nil {
		t.Fatal(err)
	}
	if s := submitSettings(t, f.lastSubmit); s["multi_shot"] != true {
		t.Fatalf("settings = %v", s)
	}
}

func TestKlingSubmitInlineImage(t *testing.T) {
	// 自有桶的私有产物图：Storage 读取后由 Kling adapter 转 base64。
	blobSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user-test/tenant/img.png" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("png-bytes"))
	}))
	t.Cleanup(blobSrv.Close)

	f := &fakeKling{t: t}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	storage, err := artifact.NewBlobStorage(artifact.BlobConfig{Endpoint: blobSrv.URL, Bucket: "user-test"})
	if err != nil {
		t.Fatal(err)
	}
	k, err := NewKlingI2V(KlingConfig{
		BaseURL: srv.URL, AccessKey: "ak", SecretKey: "sk", Model: "m",
		Artifacts: storage,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 私有对象：提交体里的首帧 url 是读取内容的纯 base64（无 data: 前缀）
	if _, err := k.Submit(context.Background(), KlingI2VRequest{
		FirstFrame: blobSrv.URL + "/user-test/tenant/img.png", Prompt: "p",
	}); err != nil {
		t.Fatal(err)
	}
	want := base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	if c := submitContent(t, f.lastSubmit, "first_frame"); c == nil || c["url"] != want {
		t.Fatalf("first_frame url = %v, want inline base64", c)
	}

	// 外部 URL：原样透传（公网 URL 可灵自己拉）
	if _, err := k.Submit(context.Background(), KlingI2VRequest{FirstFrame: "https://cdn.example/x.png", Prompt: "p"}); err != nil {
		t.Fatal(err)
	}
	if c := submitContent(t, f.lastSubmit, "first_frame"); c == nil || c["url"] != "https://cdn.example/x.png" {
		t.Fatalf("first_frame url = %v, want passthrough", c)
	}

	// 私有对象读取失败：提交前报错，不打可灵
	if _, err := k.Submit(context.Background(), KlingI2VRequest{
		FirstFrame: blobSrv.URL + "/user-test/missing.png", Prompt: "p",
	}); err == nil {
		t.Fatal("expected error when image fetch fails")
	}

	// 自有 CDN URL 显式标记为 Public，直接交给可灵，不再转 base64。
	cdnStorage, err := artifact.NewBlobStorage(artifact.BlobConfig{
		Endpoint: blobSrv.URL, Bucket: "user-test", CDNBase: "https://cdn.example/assets",
	})
	if err != nil {
		t.Fatal(err)
	}
	kPublic, err := NewKlingI2V(KlingConfig{
		BaseURL: srv.URL, AccessKey: "ak", SecretKey: "sk", Model: "m", Artifacts: cdnStorage,
	})
	if err != nil {
		t.Fatal(err)
	}
	cdnURL := "https://cdn.example/assets/tenant/img.png"
	if _, err := kPublic.Submit(context.Background(), KlingI2VRequest{FirstFrame: cdnURL, Prompt: "p"}); err != nil {
		t.Fatal(err)
	}
	if c := submitContent(t, f.lastSubmit, "first_frame"); c == nil || c["url"] != cdnURL {
		t.Fatalf("first_frame url = %v, want CDN passthrough", c)
	}

	// LocalStorage 不依赖 /files/ HTTP 路由，直接从磁盘读取后内联。
	localStorage := artifact.NewLocalStorage(t.TempDir(), "http://127.0.0.1:8080")
	localObject, err := localStorage.Put(context.Background(), "tenant/local.png", []byte("local-png"), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	kLocal, err := NewKlingI2V(KlingConfig{
		BaseURL: srv.URL, AccessKey: "ak", SecretKey: "sk", Model: "m", Artifacts: localStorage,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kLocal.Submit(context.Background(), KlingI2VRequest{FirstFrame: localObject.URL, Prompt: "p"}); err != nil {
		t.Fatal(err)
	}
	wantLocal := base64.StdEncoding.EncodeToString([]byte("local-png"))
	if c := submitContent(t, f.lastSubmit, "first_frame"); c == nil || c["url"] != wantLocal {
		t.Fatalf("first_frame url = %v, want local inline base64", c)
	}
}

// verifyJWT 校验 Authorization：Bearer JWT，签名可用 sk 验证，iss 为 ak。
func verifyJWT(t *testing.T, header, ak, sk string) {
	t.Helper()
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok {
		t.Fatalf("authorization = %q, want Bearer token", header)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt parts = %d", len(parts))
	}
	mac := hmac.New(sha256.New, []byte(sk))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	wantSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if parts[2] != wantSig {
		t.Fatal("jwt signature mismatch")
	}
	payloadRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Iss string  `json:"iss"`
		Exp float64 `json:"exp"`
		Nbf float64 `json:"nbf"`
	}
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Iss != ak {
		t.Fatalf("iss = %q, want %q", payload.Iss, ak)
	}
	now := float64(time.Now().Unix())
	if payload.Exp <= now || payload.Nbf > now {
		t.Fatalf("exp/nbf out of range: exp=%v nbf=%v now=%v", payload.Exp, payload.Nbf, now)
	}
}

func TestKlingSubmitRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 1102, "message": "risk control rejected"})
	}))
	t.Cleanup(srv.Close)
	k, err := NewKlingI2V(KlingConfig{BaseURL: srv.URL, AccessKey: "ak", SecretKey: "sk", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = k.Submit(context.Background(), KlingI2VRequest{FirstFrame: "u", Prompt: "p"})
	if err == nil || !strings.Contains(err.Error(), "risk control rejected") {
		t.Fatalf("err = %v", err)
	}
	assertClassifiedRetryability(t, err, true)
}

func TestKlingHTTP429Classification(t *testing.T) {
	tests := []struct {
		name             string
		code             int
		message          string
		wantNonRetryable bool
	}{
		{
			name:             "insufficient balance",
			code:             1102,
			message:          "Account balance not enough",
			wantNonRetryable: true,
		},
		{
			name:             "ordinary rate limit",
			code:             4290,
			message:          "rate limit exceeded",
			wantNonRetryable: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code": test.code, "message": test.message,
				})
			}))
			t.Cleanup(srv.Close)
			k, err := NewKlingI2V(KlingConfig{
				BaseURL: srv.URL, AccessKey: "ak", SecretKey: "sk", Model: "m",
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = k.Submit(context.Background(), KlingI2VRequest{
				FirstFrame: "u", Prompt: "p",
			})
			if err == nil {
				t.Fatal("expected submit error")
			}
			var providerErr *providerError
			if !errors.As(err, &providerErr) || providerErr.code != strconv.Itoa(test.code) {
				t.Fatalf("err = %v, want provider code %d", err, test.code)
			}
			assertClassifiedRetryability(t, err, test.wantNonRetryable)
		})
	}
}

func assertClassifiedRetryability(t *testing.T, err error, wantNonRetryable bool) {
	t.Helper()
	classified := temporalx.ClassifyActivityError("test", err)
	var appErr *temporal.ApplicationError
	if !errors.As(classified, &appErr) {
		t.Fatalf("classified error = %T, want ApplicationError", classified)
	}
	if appErr.NonRetryable() != wantNonRetryable {
		t.Fatalf("classified error = %v, nonRetryable=%v, want %v",
			classified, appErr.NonRetryable(), wantNonRetryable)
	}
}

func TestKlingSubmitAPIKeyAuth(t *testing.T) {
	// 新版静态 API Key：Authorization 直接下发 Bearer <key>，不签 JWT（老 AK/SK 才签）。
	f := &fakeKling{t: t}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	k, err := NewKlingI2V(KlingConfig{BaseURL: srv.URL, APIKey: "api-key-kling-xxx", Model: "kling-3.0"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.Submit(context.Background(), KlingI2VRequest{FirstFrame: "https://cdn/x.png", Prompt: "p"}); err != nil {
		t.Fatal(err)
	}
	if f.lastAuth != "Bearer api-key-kling-xxx" {
		t.Fatalf("authorization = %q, want raw \"Bearer api-key-kling-xxx\"", f.lastAuth)
	}
	// 明确不是三段式 JWT（JWT 有两个 '.'，静态 key 没有）。
	if strings.Count(strings.TrimPrefix(f.lastAuth, "Bearer "), ".") == 2 {
		t.Fatalf("authorization looks like a JWT, want raw api key: %q", f.lastAuth)
	}
	// 轮询同样用静态 key。
	f.status = "processing"
	if _, err := k.Poll(context.Background(), "task-1"); err != nil {
		t.Fatal(err)
	}
	if f.lastAuth != "Bearer api-key-kling-xxx" {
		t.Fatalf("poll authorization = %q, want raw api key", f.lastAuth)
	}
}

func TestKlingPollStates(t *testing.T) {
	f := &fakeKling{t: t}
	k := newTestKling(t, f)
	ctx := context.Background()

	// 进行中
	f.status = "processing"
	res, err := k.Poll(ctx, "task-1")
	if err != nil || res.Done || res.Failed {
		t.Fatalf("processing: res = %+v err = %v", res, err)
	}
	// 查询按 task_ids 传任务 id
	if f.lastQuery != "task_ids=task-1" {
		t.Fatalf("query = %q", f.lastQuery)
	}

	// 成功：结果归一为固定 artifact 信封
	f.status, f.videoURL = "succeeded", "https://cdn/video.mp4"
	res, err = k.Poll(ctx, "task-1")
	if err != nil || !res.Done {
		t.Fatalf("succeeded: res = %+v err = %v", res, err)
	}
	var out struct {
		Artifacts []struct {
			URI string `json:"uri"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(res.Result, &out); err != nil ||
		len(out.Artifacts) != 1 || out.Artifacts[0].URI != "https://cdn/video.mp4" {
		t.Fatalf("result = %s", res.Result)
	}

	// 成功但缺链接：终态失败（无限重试无意义）
	f.videoURL = ""
	res, err = k.Poll(ctx, "task-1")
	if err != nil || !res.Failed {
		t.Fatalf("succeeded w/o url: res = %+v err = %v", res, err)
	}

	// 失败：带失败原因
	f.status, f.statusMsg = "failed", "内容审核未通过"
	res, err = k.Poll(ctx, "task-1")
	if err != nil || !res.Failed || res.FailureReason != "内容审核未通过" {
		t.Fatalf("failed: res = %+v err = %v", res, err)
	}
}

func TestKlingPollPersistsSignedVideoURL(t *testing.T) {
	f := &fakeKling{t: t, status: "succeeded"}
	const videoBody = "fake-mp4-bytes"
	mux := http.NewServeMux()
	mux.HandleFunc("GET /signed.mp4", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("pkey") != "valid" {
			http.Error(w, "missing signature", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte(videoBody))
	})
	mux.Handle("/", f.handler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	f.videoURL = srv.URL + "/signed.mp4?pkey=valid"

	storage := artifact.NewLocalStorage(t.TempDir(), "http://local.test")
	k, err := NewKlingI2V(KlingConfig{
		BaseURL: srv.URL, AccessKey: "ak", SecretKey: "sk", Model: "kling-3.0",
		Artifacts: storage, Client: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := k.Poll(context.Background(), "task-signed")
	if err != nil || !res.Done {
		t.Fatalf("poll: res=%+v err=%v", res, err)
	}
	var out struct {
		Artifacts []struct {
			URI string `json:"uri"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(res.Result, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Artifacts) != 1 || strings.Contains(out.Artifacts[0].URI, "pkey=") ||
		!strings.HasPrefix(out.Artifacts[0].URI, "http://local.test/files/") {
		t.Fatalf("artifacts = %+v, want one stable managed URL", out.Artifacts)
	}
	object, ok := storage.Resolve(out.Artifacts[0].URI)
	if !ok {
		t.Fatalf("stored URL not resolvable: %s", out.Artifacts[0].URI)
	}
	reader, err := storage.Open(context.Background(), object)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != videoBody {
		t.Fatalf("stored video = %q, want %q", got, videoBody)
	}
}

func TestKlingTransientErrors(t *testing.T) {
	// HTTP 5xx：Submit 与 Poll 都返回 error（poll 走退避重试）
	f := &fakeKling{t: t, httpStatus: http.StatusInternalServerError}
	k := newTestKling(t, f)
	if _, err := k.Submit(context.Background(), KlingI2VRequest{FirstFrame: "u", Prompt: "p"}); err == nil {
		t.Fatal("expected submit error on HTTP 500")
	}
	if _, err := k.Poll(context.Background(), "task-1"); err == nil {
		t.Fatal("expected poll error on HTTP 500")
	}

	// 轮询业务码非 0：同样按瞬时故障处理
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 1000, "message": "auth expired"})
	}))
	t.Cleanup(srv.Close)
	k2, err := NewKlingI2V(KlingConfig{BaseURL: srv.URL, AccessKey: "ak", SecretKey: "sk", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k2.Poll(context.Background(), "t"); err == nil || !strings.Contains(err.Error(), "auth expired") {
		t.Fatalf("err = %v", err)
	}

	// 查无任务：按瞬时故障重试（避免刚提交的可见性延迟误判终态）
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": []any{}})
	}))
	t.Cleanup(srv2.Close)
	k3, err := NewKlingI2V(KlingConfig{BaseURL: srv2.URL, AccessKey: "ak", SecretKey: "sk", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k3.Poll(context.Background(), "t"); err == nil || !strings.Contains(err.Error(), "empty task list") {
		t.Fatalf("err = %v", err)
	}
	if _, err := k3.Poll(context.Background(), " "); err == nil {
		t.Fatal("expected empty task id error")
	}
}

func TestNewKlingValidation(t *testing.T) {
	cases := []KlingConfig{
		{SecretKey: "sk", Model: "m"},      // 缺 AK
		{AccessKey: "ak", Model: "m"},      // 缺 SK
		{AccessKey: "ak", SecretKey: "sk"}, // 缺 model
		{BaseURL: "relative", AccessKey: "ak", SecretKey: "sk", Model: "m"},
		{AccessKey: "ak", SecretKey: "sk", Model: "m", ProxyURL: ":::"}, // 坏代理
		{AccessKey: "ak", SecretKey: "sk", Model: "m", ProxyURL: "proxy"},
	}
	for i, cfg := range cases {
		if _, err := NewKlingI2V(cfg); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
	k, err := NewKlingI2V(KlingConfig{AccessKey: "ak", SecretKey: "sk", Model: "kling-3.0"})
	if err != nil {
		t.Fatal(err)
	}
	if got := k.queryEndpoint("t/1"); !strings.HasSuffix(got, "/tasks?task_ids=t%2F1") {
		t.Fatalf("query endpoint = %q (want query-escaped)", got)
	}
}
