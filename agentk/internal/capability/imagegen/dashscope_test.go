package imagegen

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDashScopeGenerate(t *testing.T) {
	png := []byte("z-image-png")
	var gotAuth string
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/oss/tmp.png", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(png)
	})
	var srv *httptest.Server
	mux.HandleFunc("POST /api/v1/services/aigc/multimodal-generation/generation", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"content": []map[string]any{
							{"image": srv.URL + "/oss/tmp.png"},
							{"text": "回显的 prompt"},
						},
					},
				}},
			},
		})
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	d, err := NewDashScope(DashScopeConfig{BaseURL: srv.URL, APIKey: "sk-test", Model: "z-image-turbo"})
	if err != nil {
		t.Fatal(err)
	}
	img, err := d.Generate(context.Background(), Request{Prompt: "一只橘猫", AspectRatio: "16:9"})
	if err != nil {
		t.Fatal(err)
	}
	if string(img.Data) != string(png) {
		t.Fatalf("img.Data = %q", img.Data)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("auth = %q", gotAuth)
	}
	// 三段式请求体：model + input.messages（单轮单文本）+ parameters
	if gotBody["model"] != "z-image-turbo" {
		t.Fatalf("model = %v", gotBody["model"])
	}
	params, _ := gotBody["parameters"].(map[string]any)
	if params["size"] != "1536*864" || params["prompt_extend"] != false {
		t.Fatalf("parameters = %v", params)
	}
	input, _ := gotBody["input"].(map[string]any)
	raw, _ := json.Marshal(input)
	if !strings.Contains(string(raw), `"text":"一只橘猫"`) || !strings.Contains(string(raw), `"role":"user"`) {
		t.Fatalf("input = %s", raw)
	}
}

func TestDashScopeGenerateRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": "IPInfringementSuspect", "message": "内容审核未通过",
		})
	}))
	t.Cleanup(srv.Close)
	d, err := NewDashScope(DashScopeConfig{BaseURL: srv.URL, APIKey: "sk", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Generate(context.Background(), Request{Prompt: "p"})
	if err == nil || !strings.Contains(err.Error(), "IPInfringementSuspect") {
		t.Fatalf("err = %v", err)
	}
}

func TestDashScopeNoImage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": map[string]any{"choices": []map[string]any{{
				"message": map[string]any{"content": []map[string]any{{"text": "只有文本"}}},
			}}},
		})
	}))
	t.Cleanup(srv.Close)
	d, err := NewDashScope(DashScopeConfig{BaseURL: srv.URL, APIKey: "sk", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Generate(context.Background(), Request{Prompt: "p"}); err == nil {
		t.Fatal("expected error when response has no image")
	}
}

func TestNewDashScopeValidation(t *testing.T) {
	if _, err := NewDashScope(DashScopeConfig{Model: "m"}); err == nil {
		t.Fatal("expected error: missing api key")
	}
	if _, err := NewDashScope(DashScopeConfig{APIKey: "sk"}); err == nil {
		t.Fatal("expected error: missing model")
	}
	d, err := NewDashScope(DashScopeConfig{APIKey: "sk", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if got := d.endpoint(); got != DefaultDashScopeBaseURL+"/api/v1/services/aigc/multimodal-generation/generation" {
		t.Fatalf("default endpoint = %q", got)
	}
	if _, err := NewDashScope(DashScopeConfig{BaseURL: "relative", APIKey: "sk", Model: "m"}); err == nil {
		t.Fatal("expected error for non-absolute base URL")
	}
	if _, err := NewDashScope(DashScopeConfig{APIKey: "sk", Model: "m", ProxyURL: "proxy"}); err == nil {
		t.Fatal("expected error for non-absolute proxy URL")
	}
	if _, err := d.Generate(context.Background(), Request{Prompt: "p", AspectRatio: "2:1"}); err == nil {
		t.Fatal("expected error for unsupported aspect ratio")
	}
}
