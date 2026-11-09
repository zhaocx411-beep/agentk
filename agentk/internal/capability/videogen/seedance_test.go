package videogen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentk/internal/artifact"
)

type fakeSeedance struct {
	t          *testing.T
	lastSubmit map[string]any
	lastHeader http.Header
	lastPath   string
	status     string
	videoURL   string
	errorMsg   string
	httpStatus int
}

func (f *fakeSeedance) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v3/contents/generations/tasks", func(w http.ResponseWriter, r *http.Request) {
		if f.httpStatus != 0 {
			w.WriteHeader(f.httpStatus)
			return
		}
		f.lastHeader = r.Header.Clone()
		f.lastPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&f.lastSubmit); err != nil {
			f.t.Errorf("decode submit: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "task-1"})
	})
	mux.HandleFunc("GET /api/v3/contents/generations/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		if f.httpStatus != 0 {
			w.WriteHeader(f.httpStatus)
			return
		}
		f.lastHeader = r.Header.Clone()
		f.lastPath = r.URL.Path
		response := map[string]any{"id": r.PathValue("id"), "status": f.status}
		if f.videoURL != "" {
			response["content"] = map[string]any{"video_url": f.videoURL}
		}
		if f.errorMsg != "" {
			response["error"] = map[string]any{"message": f.errorMsg}
		}
		_ = json.NewEncoder(w).Encode(response)
	})
	return mux
}

func newTestSeedance(t *testing.T, fake *fakeSeedance) *Seedance {
	t.Helper()
	server := httptest.NewServer(fake.handler())
	t.Cleanup(server.Close)
	client, err := NewSeedance(SeedanceConfig{
		BaseURL: server.URL + "/api/v3",
		Model:   "ep-test",
		Headers: map[string]string{
			"x-api-key":      "secret",
			"x-ks-user-key":  "tenant",
			"x-ks-llm-model": "ep-test",
			"x-ks-biz-scene": "offline",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func seedanceContent(t *testing.T, body map[string]any, role string) map[string]any {
	t.Helper()
	items, ok := body["content"].([]any)
	if !ok {
		t.Fatalf("content = %v", body["content"])
	}
	for _, item := range items {
		value, ok := item.(map[string]any)
		if ok && value["role"] == role {
			return value
		}
	}
	return nil
}

func TestSeedanceSubmit(t *testing.T) {
	fake := &fakeSeedance{t: t}
	client := newTestSeedance(t, fake)

	generateAudio := true
	taskID, err := client.Submit(context.Background(), SeedanceRequest{
		FirstFrame:    "https://cdn/first.png",
		LastFrame:     "https://cdn/last.png",
		Prompt:        "缓慢推近",
		Resolution:    "1080p",
		Duration:      "11",
		GenerateAudio: &generateAudio,
	})
	if err != nil {
		t.Fatal(err)
	}
	if taskID != "task-1" {
		t.Fatalf("task id = %q", taskID)
	}
	if fake.lastPath != "/api/v3/contents/generations/tasks" {
		t.Fatalf("path = %q", fake.lastPath)
	}
	if fake.lastHeader.Get("x-api-key") != "secret" ||
		fake.lastHeader.Get("x-ks-llm-model") != "ep-test" {
		t.Fatalf("headers = %v", fake.lastHeader)
	}
	if fake.lastSubmit["model"] != "ep-test" ||
		fake.lastSubmit["resolution"] != "1080p" ||
		fake.lastSubmit["ratio"] != "adaptive" ||
		fake.lastSubmit["duration"] != float64(11) ||
		fake.lastSubmit["generate_audio"] != true ||
		fake.lastSubmit["watermark"] != false {
		t.Fatalf("submit = %+v", fake.lastSubmit)
	}
	items := fake.lastSubmit["content"].([]any)
	if text := items[0].(map[string]any); text["type"] != "text" || text["text"] != "缓慢推近" {
		t.Fatalf("prompt content = %v", text)
	}
	first := seedanceContent(t, fake.lastSubmit, "first_frame")
	last := seedanceContent(t, fake.lastSubmit, "last_frame")
	if first == nil || first["image_url"].(map[string]any)["url"] != "https://cdn/first.png" {
		t.Fatalf("first frame = %v", first)
	}
	if last == nil || last["image_url"].(map[string]any)["url"] != "https://cdn/last.png" {
		t.Fatalf("last frame = %v", last)
	}
}

func TestSeedanceSubmitDefaultsAndValidation(t *testing.T) {
	fake := &fakeSeedance{t: t}
	client := newTestSeedance(t, fake)
	if _, err := client.Submit(context.Background(), SeedanceRequest{
		FirstFrame: "https://cdn/first.png", Prompt: "动起来",
	}); err != nil {
		t.Fatal(err)
	}
	if fake.lastSubmit["resolution"] != "1080p" ||
		fake.lastSubmit["duration"] != float64(5) ||
		fake.lastSubmit["generate_audio"] != false {
		t.Fatalf("defaults = %+v", fake.lastSubmit)
	}
	// 三个视频接口里只有它能不给任何素材：纯文生视频。
	if _, err := client.Submit(context.Background(), SeedanceRequest{Prompt: "海边日落"}); err != nil {
		t.Fatal(err)
	}
	if seedanceContent(t, fake.lastSubmit, "first_frame") != nil {
		t.Fatal("text-to-video must not carry a first frame")
	}
	if _, err := client.Submit(context.Background(), SeedanceRequest{
		FirstFrame: "https://cdn/x.png", Prompt: "p", Duration: "bad",
	}); err == nil {
		t.Fatal("expected duration error")
	}
	for _, request := range []SeedanceRequest{
		{Prompt: ""},
		{FirstFrame: "https://cdn/x.png", Prompt: "p", Duration: "3"},
		{FirstFrame: "https://cdn/x.png", Prompt: "p", Resolution: "240p"},
		{FirstFrame: "https://cdn/x.png", Prompt: "p", Ratio: "2:1"},
		{FirstFrame: "relative", Prompt: "p"},
		{LastFrame: "https://cdn/x.png", Prompt: "p"},
		{Prompt: "p", ReferenceAudios: []string{"https://cdn/a.mp3"}},
		{Prompt: "p", FirstFrame: "https://cdn/x.png", ReferenceImages: make([]string, 10)},
	} {
		if _, err := client.Submit(context.Background(), request); err == nil {
			t.Fatalf("expected validation error for %+v", request)
		}
	}
}

// TestSeedanceSubmitMultimodalReferences 多模态参考：图片按 role 内联/透传，
// 视频与音频只走链接，三类素材各按给定顺序进 content（prompt 里「图片n」按此对号）。
func TestSeedanceSubmitMultimodalReferences(t *testing.T) {
	fake := &fakeSeedance{t: t}
	client := newTestSeedance(t, fake)

	if _, err := client.Submit(context.Background(), SeedanceRequest{
		Prompt:          "把视频1里的商品换成图片1中的商品，配上音频1",
		ReferenceImages: []string{"https://cdn/ref1.png", "https://cdn/ref2.png"},
		ReferenceVideos: []string{"https://cdn/ref.mp4"},
		ReferenceAudios: []string{"https://cdn/ref.mp3"},
	}); err != nil {
		t.Fatal(err)
	}
	items := fake.lastSubmit["content"].([]any)
	var images, videos, audios []string
	for _, item := range items {
		value := item.(map[string]any)
		switch value["role"] {
		case "reference_image":
			images = append(images, value["image_url"].(map[string]any)["url"].(string))
		case "reference_video":
			videos = append(videos, value["video_url"].(map[string]any)["url"].(string))
		case "reference_audio":
			audios = append(audios, value["audio_url"].(map[string]any)["url"].(string))
		}
	}
	if len(images) != 2 || images[0] != "https://cdn/ref1.png" || images[1] != "https://cdn/ref2.png" {
		t.Fatalf("reference images = %v (order must match input)", images)
	}
	if len(videos) != 1 || videos[0] != "https://cdn/ref.mp4" {
		t.Fatalf("reference videos = %v", videos)
	}
	if len(audios) != 1 || audios[0] != "https://cdn/ref.mp3" {
		t.Fatalf("reference audios = %v", audios)
	}
}

// TestSeedanceRejectsPrivateReferenceVideo 视频/音频只能给上游可直接取用的链接：
// 受管私有产物无法内联，应在提交前返回明确错误，避免发送已知会失败的计费请求。
func TestSeedanceRejectsPrivateReferenceVideo(t *testing.T) {
	fake := &fakeSeedance{t: t}
	server := httptest.NewServer(fake.handler())
	t.Cleanup(server.Close)
	storage := artifact.NewLocalStorage(t.TempDir(), "http://127.0.0.1:8080")
	clip, err := storage.Put(context.Background(), "tenant/clip.mp4", []byte("mp4"), "video/mp4")
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewSeedance(SeedanceConfig{
		BaseURL: server.URL + "/api/v3", Model: "ep-test",
		Headers: map[string]string{"x-api-key": "secret"}, Artifacts: storage,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Submit(context.Background(), SeedanceRequest{
		Prompt: "沿用视频1的运镜", FirstFrame: "https://cdn/x.png",
		ReferenceVideos: []string{clip.URL},
	})
	if err == nil || !strings.Contains(err.Error(), "publicly reachable") {
		t.Fatalf("err = %v, want a clear not-publicly-reachable error", err)
	}
}

func TestSeedanceInlinesPrivateManagedFrames(t *testing.T) {
	fake := &fakeSeedance{t: t}
	server := httptest.NewServer(fake.handler())
	t.Cleanup(server.Close)
	storage := artifact.NewLocalStorage(t.TempDir(), "http://127.0.0.1:8080")
	frameData := []byte("private-png")
	frame, err := storage.Put(context.Background(), "tenant/frame.png", frameData, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewSeedance(SeedanceConfig{
		BaseURL: server.URL + "/api/v3", Model: "ep-test",
		Headers: map[string]string{"x-api-key": "secret"}, Artifacts: storage,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Submit(context.Background(), SeedanceRequest{FirstFrame: frame.URL, Prompt: "动起来"}); err != nil {
		t.Fatal(err)
	}
	first := seedanceContent(t, fake.lastSubmit, "first_frame")
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(frameData)
	if first == nil || first["image_url"].(map[string]any)["url"] != want {
		t.Fatalf("first frame = %v, want private artifact data URI", first)
	}
}

func TestSeedancePassesThroughPublicManagedFrame(t *testing.T) {
	fake := &fakeSeedance{t: t}
	server := httptest.NewServer(fake.handler())
	t.Cleanup(server.Close)
	storage, err := artifact.NewBlobStorage(artifact.BlobConfig{
		Endpoint: "http://blob.internal",
		Bucket:   "bucket",
		CDNBase:  "https://cdn.example/assets",
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewSeedance(SeedanceConfig{
		BaseURL: server.URL + "/api/v3", Model: "ep-test",
		Headers: map[string]string{"x-api-key": "secret"}, Artifacts: storage,
	})
	if err != nil {
		t.Fatal(err)
	}
	frameURL := "https://cdn.example/assets/tenant/frame.png"
	if _, err = client.Submit(context.Background(), SeedanceRequest{FirstFrame: frameURL, Prompt: "动起来"}); err != nil {
		t.Fatal(err)
	}
	first := seedanceContent(t, fake.lastSubmit, "first_frame")
	if first == nil || first["image_url"].(map[string]any)["url"] != frameURL {
		t.Fatalf("first frame = %v, want public artifact URL", first)
	}
}

func TestSeedancePollStates(t *testing.T) {
	fake := &fakeSeedance{t: t}
	client := newTestSeedance(t, fake)

	fake.status = "running"
	result, err := client.Poll(context.Background(), "task-1")
	if err != nil || result.Done || result.Failed || result.StatusDetail != "running" {
		t.Fatalf("running result = %+v, err = %v", result, err)
	}
	if fake.lastPath != "/api/v3/contents/generations/tasks/task-1" {
		t.Fatalf("poll path = %q", fake.lastPath)
	}

	fake.status, fake.videoURL = "succeeded", "https://cdn/video.mp4"
	result, err = client.Poll(context.Background(), "task-1")
	if err != nil || !result.Done {
		t.Fatalf("succeeded result = %+v, err = %v", result, err)
	}
	if !strings.Contains(string(result.Result), "https://cdn/video.mp4") {
		t.Fatalf("result = %s", result.Result)
	}

	fake.status, fake.videoURL, fake.errorMsg = "failed", "", "内容审核未通过"
	result, err = client.Poll(context.Background(), "task-1")
	if err != nil || !result.Failed || result.FailureReason != "内容审核未通过" {
		t.Fatalf("failed result = %+v, err = %v", result, err)
	}
}

func TestSeedanceErrorsAndConfigValidation(t *testing.T) {
	cases := []SeedanceConfig{
		{Model: "m", Headers: map[string]string{"x": "y"}},
		{BaseURL: "http://x", Headers: map[string]string{"x": "y"}},
		{BaseURL: "http://x", Model: "m"},
		{BaseURL: "://", Model: "m", Headers: map[string]string{"x": "y"}},
		{BaseURL: "http://x", Model: "m", Headers: map[string]string{"x": "y"}, ProxyURL: ":::"},
		{BaseURL: "http://x", Model: "m", Headers: map[string]string{"x": "y"}, ProxyURL: "proxy"},
	}
	for index, cfg := range cases {
		if _, err := NewSeedance(cfg); err == nil {
			t.Fatalf("case %d: expected error", index)
		}
	}

	fake := &fakeSeedance{t: t, httpStatus: http.StatusBadGateway}
	client := newTestSeedance(t, fake)
	if _, err := client.Submit(context.Background(), SeedanceRequest{
		FirstFrame: "https://cdn/x.png", Prompt: "p",
	}); err == nil {
		t.Fatal("expected submit HTTP error")
	}
	if _, err := client.Poll(context.Background(), "task-1"); err == nil {
		t.Fatal("expected poll HTTP error")
	}
	if _, err := client.Poll(context.Background(), " "); err == nil {
		t.Fatal("expected empty task id error")
	}
}

func TestSeedanceSensitiveContentIsNonRetryable(t *testing.T) {
	const errorCode = "InputImageSensitiveContentDetected.PrivacyInformation"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    errorCode,
				"message": "input image may contain real person",
				"type":    "BadRequest",
			},
		})
	}))
	t.Cleanup(srv.Close)
	client, err := NewSeedance(SeedanceConfig{
		BaseURL: srv.URL,
		Model:   "ep-test",
		Headers: map[string]string{"x-api-key": "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Submit(context.Background(), SeedanceRequest{
		Prompt: "p", FirstFrame: "https://cdn.test/input.png",
	})
	if err == nil {
		t.Fatal("expected sensitive-content rejection")
	}
	var providerErr *providerError
	if !errors.As(err, &providerErr) ||
		providerErr.StatusCode() != http.StatusBadRequest ||
		providerErr.code != errorCode {
		t.Fatalf("err = %v, want HTTP 400 with provider code %q", err, errorCode)
	}
	assertClassifiedRetryability(t, err, true)
}
