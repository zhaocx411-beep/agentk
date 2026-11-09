package imagegen

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.temporal.io/sdk/temporal"

	"agentk/internal/artifact"
)

type editCatalog map[string]EditEntry

func (c editCatalog) ImageClient(string) (Client, bool) { return nil, false }

func (c editCatalog) EditClient(name string) (EditClient, int, bool) {
	entry, ok := c[name]
	if !ok {
		return nil, 0, false
	}
	return entry.Client, entry.MaxReferenceImages, true
}

func validEditInput() EditCapabilityInput {
	return EditCapabilityInput{
		Model:           "qwen-edit",
		Prompt:          "换个背景",
		ReferenceImages: []string{"http://a.png", "http://b.png"},
	}
}

func TestEditCapabilityActivity(t *testing.T) {
	ctx := context.Background()
	client := &stubEditClient{img: Image{Data: []byte("png"), MIME: "image/png"}}
	storage := &stubStorage{}
	a := NewActivity(editCatalog{"qwen-edit": {Name: "qwen-edit", Client: client, MaxReferenceImages: 2}}, storage)

	out, err := a.RunEdit(ctx, validEditInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Artifacts) != 1 || out.Artifacts[0].URI != "http://blob.test/bucket/"+storage.lastKey ||
		out.Artifacts[0].Description == "" || out.Artifacts[0].Model != "qwen-edit" {
		t.Fatalf("out = %+v", out)
	}
	if len(client.last.ReferenceImages) != 2 || client.last.ReferenceImages[0] != "http://a.png" {
		t.Fatalf("reference images not forwarded: %+v", client.last.ReferenceImages)
	}
	if !strings.HasPrefix(storage.lastKey, "capabilities/images/") {
		t.Fatalf("key = %q", storage.lastKey)
	}
	// 未指定画幅报 auto，表达"交给模型定"
	if out.Artifacts[0].Params["aspectRatio"] != "auto" || out.Artifacts[0].Params["referenceImages"] != 2 {
		t.Fatalf("params = %+v", out.Artifacts[0].Params)
	}
}

func TestEditCapabilityActivityValidation(t *testing.T) {
	ctx := context.Background()
	catalog := editCatalog{"qwen-edit": {Name: "qwen-edit", Client: &stubEditClient{}, MaxReferenceImages: 2}}
	a := NewActivity(catalog, &stubStorage{})

	for name, mutate := range map[string]func(*EditCapabilityInput){
		"missing model":       func(in *EditCapabilityInput) { in.Model = "" },
		"missing prompt":      func(in *EditCapabilityInput) { in.Prompt = " " },
		"bad aspect ratio":    func(in *EditCapabilityInput) { in.AspectRatio = "2:1" },
		"no references":       func(in *EditCapabilityInput) { in.ReferenceImages = nil },
		"blank reference":     func(in *EditCapabilityInput) { in.ReferenceImages = []string{" "} },
		"model not available": func(in *EditCapabilityInput) { in.Model = "nope" },
		"over reference limit": func(in *EditCapabilityInput) {
			in.ReferenceImages = []string{"http://1.png", "http://2.png", "http://3.png"}
		},
	} {
		in := validEditInput()
		mutate(&in)
		_, err := a.RunEdit(ctx, in)
		var appErr *temporal.ApplicationError
		if !errors.As(err, &appErr) || !appErr.NonRetryable() {
			t.Fatalf("%s: err = %v, want non-retryable", name, err)
		}
	}

	// 参考图必须是受管存储产出的 URL（防 SSRF：能力会去下载这些地址）
	managed := artifact.NewLocalStorage(t.TempDir(), "http://artifacts.local")
	strict := NewActivity(catalog, managed)
	in := validEditInput()
	in.ReferenceImages = []string{"http://169.254.169.254/latest/meta-data"}
	if _, err := strict.RunEdit(ctx, in); err == nil ||
		!strings.Contains(err.Error(), "configured artifact storage") {
		t.Fatalf("expected unmanaged reference URL rejection, got %v", err)
	}

	// 不限上限的条目多张也 OK
	loose := NewActivity(editCatalog{"loose": {Name: "loose", Client: &stubEditClient{img: Image{Data: []byte("x")}}}}, &stubStorage{})
	many := EditCapabilityInput{Model: "loose", Prompt: "p",
		ReferenceImages: []string{"http://1.png", "http://2.png", "http://3.png", "http://4.png", "http://5.png"}}
	if _, err := loose.RunEdit(ctx, many); err != nil {
		t.Fatal(err)
	}
}

func TestEditCapabilityActivityUpstreamErrorRetryable(t *testing.T) {
	a := NewActivity(editCatalog{"m": {Name: "m", Client: &stubEditClient{err: errors.New("boom")}}}, &stubStorage{})
	in := validEditInput()
	in.Model = "m"
	_, err := a.RunEdit(context.Background(), in)
	if err == nil {
		t.Fatal("edit error swallowed")
	}
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) && appErr.NonRetryable() {
		t.Fatalf("transient edit error must stay retryable: %v", err)
	}
}
