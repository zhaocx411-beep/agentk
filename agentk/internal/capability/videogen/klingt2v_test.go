package videogen

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// t2vHandler 复用 fakeKling 的响应脚本，只把提交路由换成 /text-to-video/{model}
// ——三个可灵端点共用同一套鉴权与任务查询，这正是它们共享 klingTransport 的原因。
func t2vHandler(f *fakeKling) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /text-to-video/{model}", func(w http.ResponseWriter, r *http.Request) {
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
			"code": 0, "data": map[string]any{"id": "task-t2v", "status": "submitted"},
		})
	})
	mux.Handle("GET /tasks", f.handler())
	return mux
}

func newTestT2V(t *testing.T, f *fakeKling) *KlingT2V {
	t.Helper()
	srv := httptest.NewServer(t2vHandler(f))
	t.Cleanup(srv.Close)
	k, err := NewKlingT2V(KlingConfig{
		BaseURL: srv.URL, APIKey: "api-key-kling-xxx", Model: "kling-3.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// TestT2VSubmitPutsPromptAtTopLevel 文生视频的请求体形状与另两个端点不同：
// 没有 contents，prompt 是顶层字段——一个素材都收不了。
func TestT2VSubmitPutsPromptAtTopLevel(t *testing.T) {
	f := &fakeKling{t: t}
	k := newTestT2V(t, f)

	taskID, err := k.Submit(context.Background(), KlingT2VRequest{
		Prompt: "少女坐在火车上望向窗外，神情忧郁", Resolution: "4k",
		Duration: "15", AspectRatio: "9:16", Audio: "native",
	})
	if err != nil {
		t.Fatal(err)
	}
	if taskID != "task-t2v" {
		t.Fatalf("task id = %q", taskID)
	}
	if f.lastPath != "/text-to-video/kling-3.0" {
		t.Fatalf("submit path = %q", f.lastPath)
	}
	if _, ok := f.lastSubmit["contents"]; ok {
		t.Fatalf("text-to-video must not send contents: %v", f.lastSubmit)
	}
	if f.lastSubmit["prompt"] != "少女坐在火车上望向窗外，神情忧郁" {
		t.Fatalf("prompt = %v", f.lastSubmit["prompt"])
	}
	s := submitSettings(t, f.lastSubmit)
	if s["resolution"] != "4k" || s["duration"] != float64(15) ||
		s["aspect_ratio"] != "9:16" || s["audio"] != "native" || s["multi_shot"] != true {
		t.Fatalf("settings = %v", s)
	}
}

// TestT2VSubmitDefaults 缺省全部显式下发：画幅没有素材可跟随，故与另两个端点
// 不同——它一定有值。
func TestT2VSubmitDefaults(t *testing.T) {
	f := &fakeKling{t: t}
	k := newTestT2V(t, f)

	if _, err := k.Submit(context.Background(), KlingT2VRequest{Prompt: "海边日落"}); err != nil {
		t.Fatal(err)
	}
	s := submitSettings(t, f.lastSubmit)
	if s["resolution"] != "1080p" || s["duration"] != float64(5) ||
		s["aspect_ratio"] != "16:9" || s["audio"] != "native" {
		t.Fatalf("settings = %v", s)
	}
}

func TestT2VSubmitMultiShot(t *testing.T) {
	f := &fakeKling{t: t}
	k := newTestT2V(t, f)
	multiShot := true

	if _, err := k.Submit(context.Background(), KlingT2VRequest{
		Prompt: "镜头 1, 3, 女孩上车; 镜头 2, 2, 列车驶离;", Duration: "5", MultiShot: &multiShot,
	}); err != nil {
		t.Fatal(err)
	}
	if s := submitSettings(t, f.lastSubmit); s["multi_shot"] != true {
		t.Fatalf("settings = %v", s)
	}
}

func TestT2VRejectsInvalidInput(t *testing.T) {
	f := &fakeKling{t: t}
	k := newTestT2V(t, f)

	cases := map[string]KlingT2VRequest{
		"缺 prompt":  {},
		"空白 prompt": {Prompt: "   "},
		"坏时长":       {Prompt: "p", Duration: "2"},
		"坏清晰度":      {Prompt: "p", Resolution: "480p"},
		"坏画幅":       {Prompt: "p", AspectRatio: "21:9"},
		"坏声音":       {Prompt: "p", Audio: "original"},
	}
	for label, req := range cases {
		if _, err := k.Submit(context.Background(), req); err == nil {
			t.Errorf("%s: expected validation error", label)
		}
	}
}

func TestT2VPollSharesKlingTaskQuery(t *testing.T) {
	f := &fakeKling{t: t, status: "succeeded", videoURL: "https://cdn/t2v.mp4"}
	k := newTestT2V(t, f)

	res, err := k.Poll(context.Background(), "task-t2v")
	if err != nil || !res.Done {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	if !strings.Contains(string(res.Result), "https://cdn/t2v.mp4") {
		t.Fatalf("result = %s", res.Result)
	}
	if f.lastQuery != "task_ids=task-t2v" {
		t.Fatalf("query = %q", f.lastQuery)
	}
}
