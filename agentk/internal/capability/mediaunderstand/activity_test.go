package mediaunderstand

import (
	"context"
	"errors"
	"math"
	"testing"

	"go.temporal.io/sdk/temporal"
)

type understandCatalog map[string]Client

func (c understandCatalog) UnderstandClient(name string) (Client, bool) {
	client, ok := c[name]
	return client, ok
}

func validInput() CapabilityInput {
	return CapabilityInput{
		Model:     "gemini-pro",
		MediaURLs: []string{"http://cdn/a.mp4", "http://cdn/b.png"},
		Prompt:    "讲了什么",
	}
}

func TestUnderstandCapabilityActivity(t *testing.T) {
	client := &stubClient{res: Result{
		Text:   "一只猫在跳",
		Params: map[string]any{"media": []map[string]any{{"kind": "video"}}},
	}}
	a := NewActivity(understandCatalog{"gemini-pro": client})

	out, err := a.Run(context.Background(), validInput())
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != "一只猫在跳" || out.Model != "gemini-pro" {
		t.Fatalf("out = %+v", out)
	}
	if len(client.last.MediaURLs) != 2 || client.last.MediaURLs[0] != "http://cdn/a.mp4" ||
		client.last.Prompt != "讲了什么" {
		t.Fatalf("client request = %+v", client.last)
	}
	// params 记录模型实际收到的素材信息
	if out.Params["media"] == nil {
		t.Fatalf("params = %+v", out.Params)
	}
}

func TestUnderstandCapabilityActivityValidation(t *testing.T) {
	client := &stubClient{}
	a := NewActivity(understandCatalog{"gemini-pro": client})

	for name, mutate := range map[string]func(*CapabilityInput){
		"missing model":       func(in *CapabilityInput) { in.Model = "" },
		"missing media":       func(in *CapabilityInput) { in.MediaURLs = nil },
		"missing prompt":      func(in *CapabilityInput) { in.Prompt = "  " },
		"model not available": func(in *CapabilityInput) { in.Model = "nope" },
	} {
		in := validInput()
		mutate(&in)
		_, err := a.Run(context.Background(), in)
		var appErr *temporal.ApplicationError
		if !errors.As(err, &appErr) || !appErr.NonRetryable() {
			t.Fatalf("%s: err = %v, want non-retryable", name, err)
		}
	}
	if client.last.Prompt != "" {
		t.Fatal("invalid input must not reach the upstream client")
	}
}

func TestUnderstandCapabilityActivityUpstreamErrorRetryable(t *testing.T) {
	a := NewActivity(understandCatalog{"m": &stubClient{err: errors.New("boom")}})
	in := validInput()
	in.Model = "m"
	_, err := a.Run(context.Background(), in)
	if err == nil {
		t.Fatal("understand error swallowed")
	}
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) && appErr.NonRetryable() {
		t.Fatalf("transient understand error must stay retryable: %v", err)
	}
}

func validSegmentInput() SegmentInput {
	return SegmentInput{
		Model:        "gemini-pro",
		VideoURL:     "http://cdn/a.mp4",
		StartSeconds: 12.5,
		EndSeconds:   19.75,
		FPS:          4,
		Prompt:       "按时间轴描述画面与声音",
	}
}

func TestUnderstandVideoSegmentActivity(t *testing.T) {
	client := &stubClient{res: Result{
		Text:   "人物走进房间",
		Params: map[string]any{"videoSegment": map[string]any{"startSeconds": 12.5}},
	}}
	a := NewActivity(understandCatalog{"gemini-pro": client})

	out, err := a.RunSegment(context.Background(), validSegmentInput())
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != "人物走进房间" || out.Model != "gemini-pro" {
		t.Fatalf("out = %+v", out)
	}
	if len(client.last.MediaURLs) != 1 || client.last.MediaURLs[0] != "http://cdn/a.mp4" ||
		client.last.VideoSegment == nil || client.last.VideoSegment.StartSeconds != 12.5 ||
		client.last.VideoSegment.EndSeconds != 19.75 || client.last.VideoSegment.FPS != 4 {
		t.Fatalf("client request = %+v", client.last)
	}
}

func TestUnderstandVideoSegmentActivityValidation(t *testing.T) {
	for name, mutate := range map[string]func(*SegmentInput){
		"missing model":       func(in *SegmentInput) { in.Model = "" },
		"missing video":       func(in *SegmentInput) { in.VideoURL = " " },
		"negative start":      func(in *SegmentInput) { in.StartSeconds = -1 },
		"non-finite start":    func(in *SegmentInput) { in.StartSeconds = math.NaN() },
		"end before start":    func(in *SegmentInput) { in.EndSeconds = in.StartSeconds },
		"fps above maximum":   func(in *SegmentInput) { in.FPS = 25 },
		"negative fps":        func(in *SegmentInput) { in.FPS = -1 },
		"missing prompt":      func(in *SegmentInput) { in.Prompt = " " },
		"model not available": func(in *SegmentInput) { in.Model = "nope" },
	} {
		client := &stubClient{}
		a := NewActivity(understandCatalog{"gemini-pro": client})
		in := validSegmentInput()
		mutate(&in)
		_, err := a.RunSegment(context.Background(), in)
		var appErr *temporal.ApplicationError
		if !errors.As(err, &appErr) || !appErr.NonRetryable() {
			t.Fatalf("%s: err = %v, want non-retryable", name, err)
		}
		if client.last.VideoSegment != nil {
			t.Fatalf("%s: invalid input reached client: %+v", name, client.last)
		}
	}
}
