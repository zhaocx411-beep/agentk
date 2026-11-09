package videogen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentk/internal/artifact"
)

// fakeOmni 复用 fakeKling 的响应脚本，只把提交路由换成 /omni-video/{model}
// ——两个端点共用同一套鉴权与任务查询，这正是它们共享 klingTransport 的原因。
func omniHandler(f *fakeKling) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /omni-video/{model}", func(w http.ResponseWriter, r *http.Request) {
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
			"code": 0, "data": map[string]any{"id": "task-omni", "status": "submitted"},
		})
	})
	mux.Handle("GET /tasks", f.handler())
	return mux
}

func newTestOmni(t *testing.T, f *fakeKling, storage artifact.Storage) *KlingOmni {
	t.Helper()
	srv := httptest.NewServer(omniHandler(f))
	t.Cleanup(srv.Close)
	k, err := NewKlingOmni(KlingConfig{
		BaseURL: srv.URL, APIKey: "api-key-kling-xxx", Model: "kling-3.0-omni", Artifacts: storage,
	})
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// submitContents 取提交体里某个 type 的全部素材项（参考图会有多条）。
func submitContents(t *testing.T, body map[string]any, typ string) []map[string]any {
	t.Helper()
	items, ok := body["contents"].([]any)
	if !ok {
		t.Fatalf("contents = %v", body["contents"])
	}
	var out []map[string]any
	for _, it := range items {
		if m, ok := it.(map[string]any); ok && m["type"] == typ {
			out = append(out, m)
		}
	}
	return out
}

// TestOmniSubmitAssignsAssetIDs 参考素材按给定顺序编号 image_1..N / video_1，
// 这就是 prompt 里 @image_n 能对上号的依据——编号规则由客户端定死。
func TestOmniSubmitAssignsAssetIDs(t *testing.T) {
	f := &fakeKling{t: t}
	k := newTestOmni(t, f, nil)

	taskID, err := k.Submit(context.Background(), KlingOmniRequest{
		Prompt:       "让 @image_1 里的人按 @video_1 的运镜走一遍",
		FirstFrame:   "https://cdn/first.png",
		ReferImages:  []string{"https://cdn/a.png", "https://cdn/b.png"},
		FeatureVideo: "https://cdn/ref.mp4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if taskID != "task-omni" {
		t.Fatalf("task id = %q", taskID)
	}
	// 模型进路径，不进请求体（与图生视频同一约定，只是路由不同）
	if f.lastPath != "/omni-video/kling-3.0-omni" {
		t.Fatalf("submit path = %q", f.lastPath)
	}
	refs := submitContents(t, f.lastSubmit, "refer_image")
	if len(refs) != 2 || refs[0]["id"] != "image_1" || refs[1]["id"] != "image_2" {
		t.Fatalf("refer_image ids = %v", refs)
	}
	if refs[0]["url"] != "https://cdn/a.png" || refs[1]["url"] != "https://cdn/b.png" {
		t.Fatalf("refer_image order must follow input: %v", refs)
	}
	videos := submitContents(t, f.lastSubmit, "feature_video")
	if len(videos) != 1 || videos[0]["id"] != "video_1" || videos[0]["url"] != "https://cdn/ref.mp4" {
		t.Fatalf("feature_video = %v", videos)
	}
	// 首帧不编号：它的角色由 type 定死，prompt 不需要 @ 它
	if frames := submitContents(t, f.lastSubmit, "first_frame"); len(frames) != 1 || frames[0]["id"] != nil {
		t.Fatalf("first_frame = %v", frames)
	}
	// 有首帧时画幅跟随素材，不下发 aspect_ratio
	s := submitSettings(t, f.lastSubmit)
	if _, ok := s["aspect_ratio"]; ok {
		t.Fatalf("aspect_ratio must be omitted when a first frame is given: %v", s)
	}
	if s["resolution"] != "1080p" || s["duration"] != float64(5) ||
		s["audio"] != "off" || s["multi_shot"] != true {
		t.Fatalf("settings = %v", s)
	}
}

// TestOmniSubmitDefaultsAspectRatio 既无首帧也无参考视频时画幅无从推断，
// 归一补 16:9 并如实下发。
func TestOmniSubmitDefaultsAspectRatio(t *testing.T) {
	f := &fakeKling{t: t}
	k := newTestOmni(t, f, nil)

	if _, err := k.Submit(context.Background(), KlingOmniRequest{Prompt: "一只猫跳上桌子"}); err != nil {
		t.Fatal(err)
	}
	if s := submitSettings(t, f.lastSubmit); s["aspect_ratio"] != "16:9" || s["audio"] != "native" {
		t.Fatalf("settings = %v", s)
	}
}

func TestOmniDefaultsBaseVideoToOriginalAudio(t *testing.T) {
	f := &fakeKling{t: t}
	k := newTestOmni(t, f, nil)
	multiShot := false

	if _, err := k.Submit(context.Background(), KlingOmniRequest{
		Prompt: "提高亮度", BaseVideo: "https://cdn/base.mp4", MultiShot: &multiShot,
	}); err != nil {
		t.Fatal(err)
	}
	if s := submitSettings(t, f.lastSubmit); s["audio"] != "original" {
		t.Fatalf("settings = %v", s)
	}
}

// TestOmniSubmitInlinesPrivateImage 图片素材的内联规则与图生视频一致：
// 私有产物转纯 base64，公网 URL 原样透传。视频素材不走内联。
func TestOmniSubmitInlinesPrivateImage(t *testing.T) {
	f := &fakeKling{t: t}
	storage := artifact.NewLocalStorage(t.TempDir(), "http://127.0.0.1:8080")
	object, err := storage.Put(context.Background(), "tenant/ref.png", []byte("ref-png"), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	k := newTestOmni(t, f, storage)

	if _, err := k.Submit(context.Background(), KlingOmniRequest{
		Prompt: "@image_1 的风格", ReferImages: []string{object.URL},
	}); err != nil {
		t.Fatal(err)
	}
	refs := submitContents(t, f.lastSubmit, "refer_image")
	want := base64.StdEncoding.EncodeToString([]byte("ref-png"))
	if len(refs) != 1 || refs[0]["url"] != want {
		t.Fatalf("refer_image url = %v, want inline base64", refs)
	}
}

// TestOmniRejectsInvalidCombinations 验证文档明确限制的素材组合会在提交前被拒绝；
// 单次生成会计费，不应把已知无效的请求发送到上游后再等待 400 响应。
func TestOmniRejectsInvalidCombinations(t *testing.T) {
	f := &fakeKling{t: t}
	k := newTestOmni(t, f, nil)
	multiShot := true

	cases := map[string]KlingOmniRequest{
		"缺 prompt":     {FirstFrame: "https://cdn/x.png"},
		"只给尾帧":         {Prompt: "p", LastFrame: "https://cdn/x.png"},
		"两段参考视频":       {Prompt: "p", FeatureVideo: "https://cdn/a.mp4", BaseVideo: "https://cdn/b.mp4"},
		"待编辑视频 + 首帧":   {Prompt: "p", BaseVideo: "https://cdn/b.mp4", FirstFrame: "https://cdn/x.png"},
		"待编辑视频 + 原生声音": {Prompt: "p", BaseVideo: "https://cdn/b.mp4", Audio: "native"},
		"待编辑视频 + 多镜头":  {Prompt: "p", BaseVideo: "https://cdn/b.mp4", MultiShot: &multiShot},
		"特征参考视频 + 声音":  {Prompt: "p", FeatureVideo: "https://cdn/a.mp4", Audio: "native"},
		"无参考视频却要原声":    {Prompt: "p", FirstFrame: "https://cdn/x.png", Audio: "original"},
		"参考图超额":        {Prompt: "p", ReferImages: make([]string, 8)},
		"有参考视频时参考图超额":  {Prompt: "p", FeatureVideo: "https://cdn/a.mp4", Audio: "off", ReferImages: make([]string, 5)},
		"坏画幅":          {Prompt: "p", AspectRatio: "21:9"},
		"坏时长":          {Prompt: "p", Duration: "2"},
	}
	for label, req := range cases {
		if _, err := k.Submit(context.Background(), req); err == nil {
			t.Errorf("%s: expected validation error", label)
		}
	}
}

func TestOmniPollSharesKlingTaskQuery(t *testing.T) {
	f := &fakeKling{t: t, status: "succeeded", videoURL: "https://cdn/omni.mp4"}
	k := newTestOmni(t, f, nil)

	res, err := k.Poll(context.Background(), "task-omni")
	if err != nil || !res.Done {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	if !strings.Contains(string(res.Result), "https://cdn/omni.mp4") {
		t.Fatalf("result = %s", res.Result)
	}
	if f.lastQuery != "task_ids=task-omni" {
		t.Fatalf("query = %q", f.lastQuery)
	}
}
