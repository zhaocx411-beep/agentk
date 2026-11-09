package imagegen

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.temporal.io/sdk/temporal"
)

type imageCatalog map[string]Client

func (c imageCatalog) ImageClient(name string) (Client, bool) {
	client, ok := c[name]
	return client, ok
}

func (c imageCatalog) EditClient(string) (EditClient, int, bool) { return nil, 0, false }

func validImageInput() CapabilityInput {
	return CapabilityInput{
		Model:  "gpt-image",
		Prompt: "一只猫", AspectRatio: "9:16",
	}
}

func TestImageCapabilityActivity(t *testing.T) {
	ctx := context.Background()
	client := &stubClient{img: Image{
		Data: []byte("png"), MIME: "image/png",
		Params: map[string]any{"size": "1024x1792"},
	}}
	catalog := imageCatalog{"gpt-image": client}
	storage := &stubStorage{}
	a := NewActivity(catalog, storage)

	out, err := a.Run(ctx, validImageInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Artifacts) != 1 || out.Artifacts[0].URI != "http://blob.test/bucket/"+storage.lastKey ||
		out.Artifacts[0].Description == "" || out.Artifacts[0].Model != "gpt-image" {
		t.Fatalf("out = %+v", out)
	}
	// params 记录实际发送给上游的参数：画幅在公共层确定，供应商参数一并回报。
	if out.Artifacts[0].Params["aspectRatio"] != "9:16" || out.Artifacts[0].Params["size"] != "1024x1792" {
		t.Fatalf("params = %+v", out.Artifacts[0].Params)
	}
	if client.last.Prompt != "一只猫" || client.last.AspectRatio != "9:16" {
		t.Fatalf("client request = %+v", client.last)
	}
	if !strings.HasPrefix(storage.lastKey, "capabilities/images/") ||
		strings.Contains(storage.lastKey, "org_1") {
		t.Fatalf("key = %q", storage.lastKey)
	}
	firstKey := storage.lastKey

	// 画幅缺省落实为 1:1
	in := validImageInput()
	in.AspectRatio = ""
	if _, err := a.Run(ctx, in); err != nil {
		t.Fatal(err)
	}
	if client.last.AspectRatio != defaultAspectRatio {
		t.Fatalf("aspectRatio = %q", client.last.AspectRatio)
	}
	if storage.lastKey != firstKey {
		t.Fatalf("same activity call key changed: %q -> %q", firstKey, storage.lastKey)
	}
}

func TestImageCapabilityActivityValidation(t *testing.T) {
	ctx := context.Background()
	a := NewActivity(imageCatalog{"gpt-image": &stubClient{}}, &stubStorage{})

	for name, mutate := range map[string]func(*CapabilityInput){
		"missing model":       func(in *CapabilityInput) { in.Model = "" },
		"missing prompt":      func(in *CapabilityInput) { in.Prompt = " " },
		"bad aspect ratio":    func(in *CapabilityInput) { in.AspectRatio = "2:1" },
		"model not available": func(in *CapabilityInput) { in.Model = "nope" },
	} {
		in := validImageInput()
		mutate(&in)
		_, err := a.Run(ctx, in)
		var appErr *temporal.ApplicationError
		if !errors.As(err, &appErr) || !appErr.NonRetryable() {
			t.Fatalf("%s: err = %v, want non-retryable", name, err)
		}
	}
}

func TestImageCapabilityActivityGenerateErrorRetryable(t *testing.T) {
	// 生成失败缺省保持可重试（网络抖动等瞬时故障；4xx 由错误分类自动转不可重试）
	a := NewActivity(imageCatalog{"gpt-image": &stubClient{err: errors.New("boom")}}, &stubStorage{})
	_, err := a.Run(context.Background(), validImageInput())
	if err == nil {
		t.Fatal("generate error swallowed")
	}
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) && appErr.NonRetryable() {
		t.Fatalf("transient generate error must stay retryable: %v", err)
	}
}
