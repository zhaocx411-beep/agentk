package imagegen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentk/internal/artifact"
)

func b64PNG(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func TestKSGptImageGenerate(t *testing.T) {
	var gotHeaders http.Header
	var gotBody map[string]any
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		gotQuery = r.URL.RawQuery
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"b64_json": b64PNG("gen-png")}}})
	}))
	t.Cleanup(srv.Close)

	k, err := NewKSGptImage(KSGptImageConfig{
		BaseURL:    srv.URL + "/openai/deployments/llm-68436-openai-gpt-image-2",
		APIVersion: "2024-02-01",
		Quality:    "medium",
		Headers: map[string]string{
			"x-api-key":      "secret",
			"x-ks-user-key":  "mmu-yangjingchao03-68436",
			"x-ks-llm-model": "llm-68436-openai-gpt-image-2",
			"x-ks-biz-scene": "offline",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	img, err := k.Generate(context.Background(), Request{Prompt: "一只橘猫", AspectRatio: "16:9"})
	if err != nil {
		t.Fatal(err)
	}
	if string(img.Data) != "gen-png" {
		t.Fatalf("img.Data = %q", img.Data)
	}
	if !strings.Contains(gotQuery, "api-version=2024-02-01") {
		t.Fatalf("query = %q", gotQuery)
	}
	if gotHeaders.Get("x-api-key") != "secret" || gotHeaders.Get("Content-Type") != "application/json" {
		t.Fatalf("headers = %v", gotHeaders)
	}
	if gotBody["prompt"] != "一只橘猫" || gotBody["size"] != "1536x1024" || gotBody["quality"] != "medium" {
		t.Fatalf("body = %v", gotBody)
	}
}

func TestKSGptImageEdit(t *testing.T) {
	png := []byte("ref-bytes")
	var gotHeaders http.Header
	var parts []string
	var fields = map[string]string{}
	mux := http.NewServeMux()
	mux.HandleFunc("/ref/1.png", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(png) })
	mux.HandleFunc("/ref/2.png", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(png) })
	mux.HandleFunc("/openai/deployments/llm-68436-openai-gpt-image-2/images/edits", func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		mediaType, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if mediaType != "multipart/form-data" {
			t.Errorf("content-type = %q", r.Header.Get("Content-Type"))
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if p.FileName() != "" {
				parts = append(parts, p.FormName()+":"+p.FileName())
			} else {
				b, _ := io.ReadAll(p)
				fields[p.FormName()] = string(b)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"b64_json": b64PNG("edited-png")}}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	storage := artifact.NewLocalStorage(t.TempDir(), "http://artifacts.local")
	ref1, err := storage.Put(context.Background(), "ref/1.png", png, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	ref2, err := storage.Put(context.Background(), "ref/2.png", png, "image/png")
	if err != nil {
		t.Fatal(err)
	}

	k, err := NewKSGptImage(KSGptImageConfig{
		BaseURL:    srv.URL + "/openai/deployments/llm-68436-openai-gpt-image-2",
		APIVersion: "2025-04-01-preview",
		Quality:    "high",
		Headers:    map[string]string{"x-api-key": "secret", "x-ks-biz-scene": "offline"},
		Artifacts:  storage,
	})
	if err != nil {
		t.Fatal(err)
	}
	img, err := k.Edit(context.Background(), EditRequest{
		Prompt:          "把卡通形象放进城市照片",
		ReferenceImages: []string{ref1.URL, ref2.URL},
		AspectRatio:     "1:1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(img.Data) != "edited-png" {
		t.Fatalf("img.Data = %q", img.Data)
	}
	// 两张参考图各成一个 image[] 文件段
	if len(parts) != 2 || parts[0] != "image[]:image_0.png" || parts[1] != "image[]:image_1.png" {
		t.Fatalf("file parts = %v", parts)
	}
	if fields["prompt"] != "把卡通形象放进城市照片" || fields["size"] != "1024x1024" || fields["quality"] != "high" || fields["n"] != "1" {
		t.Fatalf("form fields = %v", fields)
	}
	if gotHeaders.Get("x-api-key") != "secret" {
		t.Fatalf("headers = %v", gotHeaders)
	}
	if _, err := k.Edit(context.Background(), EditRequest{
		Prompt: "p", ReferenceImages: []string{" "},
	}); err == nil {
		t.Fatal("expected blank reference image error")
	}
}

func TestKSGptImageEditRequiresReference(t *testing.T) {
	k, _ := NewKSGptImage(KSGptImageConfig{BaseURL: "http://x/openai/deployments/d", Headers: map[string]string{"x-api-key": "k"}})
	if _, err := k.Edit(context.Background(), EditRequest{Prompt: "p"}); err == nil {
		t.Fatal("expected error without reference images")
	}
	if _, err := k.Generate(context.Background(), Request{}); err == nil {
		t.Fatal("expected error without prompt")
	}
}

func TestKSGptImageEditRejected(t *testing.T) {
	png := []byte("x")
	mux := http.NewServeMux()
	mux.HandleFunc("/ref/1.png", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(png) })
	mux.HandleFunc("/openai/deployments/d/images/edits", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"content policy violation"}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	storage := artifact.NewLocalStorage(t.TempDir(), "http://artifacts.local")
	ref, err := storage.Put(context.Background(), "ref/1.png", png, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	k, _ := NewKSGptImage(KSGptImageConfig{
		BaseURL: srv.URL + "/openai/deployments/d", Headers: map[string]string{"x-api-key": "k"}, Artifacts: storage,
	})
	if _, err := k.Edit(context.Background(), EditRequest{Prompt: "p", ReferenceImages: []string{ref.URL}}); err == nil ||
		!strings.Contains(err.Error(), "HTTP 400") {
		t.Fatalf("expected HTTP 400 error, got %v", err)
	}
}

func TestKSGptImageEditRejectsUnmanagedReferenceURL(t *testing.T) {
	storage := artifact.NewLocalStorage(t.TempDir(), "http://artifacts.local")
	k, _ := NewKSGptImage(KSGptImageConfig{
		BaseURL:   "http://gateway/openai/deployments/d",
		Headers:   map[string]string{"x-api-key": "k"},
		Artifacts: storage,
	})
	_, err := k.Edit(context.Background(), EditRequest{
		Prompt: "p", ReferenceImages: []string{"http://169.254.169.254/latest/meta-data"},
	})
	if err == nil || !strings.Contains(err.Error(), "untrusted reference URL") {
		t.Fatalf("expected unmanaged reference rejection, got %v", err)
	}
}

func TestNewKSGptImageValidation(t *testing.T) {
	if _, err := NewKSGptImage(KSGptImageConfig{Headers: map[string]string{"x-api-key": "k"}}); err == nil {
		t.Fatal("expected error without baseUrl")
	}
	if _, err := NewKSGptImage(KSGptImageConfig{BaseURL: "u"}); err == nil {
		t.Fatal("expected error without headers")
	}
	// quality 缺省 medium
	k, err := NewKSGptImage(KSGptImageConfig{BaseURL: "http://x", Headers: map[string]string{"x-api-key": "k"}})
	if err != nil || k.cfg.Quality != DefaultGptImageQuality {
		t.Fatalf("default quality not applied: %+v err=%v", k, err)
	}
	if _, err := NewKSGptImage(KSGptImageConfig{
		BaseURL: "http://x", Headers: map[string]string{"x-api-key": "k"}, Quality: "ultra",
	}); err == nil {
		t.Fatal("expected unsupported quality error")
	}
	if _, err := k.Generate(context.Background(), Request{Prompt: "p", AspectRatio: "2:1"}); err == nil {
		t.Fatal("expected unsupported aspect ratio error")
	}
}
