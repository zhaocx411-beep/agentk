package imagegen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAzureGenerate(t *testing.T) {
	png := []byte("fake-png-bytes")
	var gotPath, gotQuery, gotAPIKey string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAPIKey = r.Header.Get("api-key")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(png)}},
		})
	}))
	t.Cleanup(srv.Close)

	a, err := NewAzure(AzureConfig{BaseURL: srv.URL + "/gw-prefix", APIKey: "key-1", Deployment: "gpt-image-2"})
	if err != nil {
		t.Fatal(err)
	}
	img, err := a.Generate(context.Background(), Request{Prompt: "一只猫", AspectRatio: "9:16"})
	if err != nil {
		t.Fatal(err)
	}
	if string(img.Data) != string(png) || img.MIME != "image/png" {
		t.Fatalf("img = %+v", img)
	}
	// deployment 嵌路径 + api-version 查询参数 + api-key 头
	if gotPath != "/gw-prefix/openai/deployments/gpt-image-2/images/generations" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotQuery != "api-version="+DefaultAzureAPIVersion {
		t.Fatalf("query = %q", gotQuery)
	}
	if gotAPIKey != "key-1" {
		t.Fatalf("api-key = %q", gotAPIKey)
	}
	// body 不放 model（deployment 已在路径，混放会触发路由错误），画幅映射为 size
	if _, ok := gotBody["model"]; ok {
		t.Fatalf("body must not contain model: %v", gotBody)
	}
	if gotBody["prompt"] != "一只猫" || gotBody["size"] != "1024x1536" {
		t.Fatalf("body = %v", gotBody)
	}
}

func TestAzureGenerateResultURL(t *testing.T) {
	// 返回 url 而非 b64：再下载一次拿字节
	png := []byte("png-via-url")
	mux := http.NewServeMux()
	mux.HandleFunc("/result.png", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(png)
	})
	var srv *httptest.Server
	mux.HandleFunc("POST /openai/deployments/{d}/images/generations", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"url": srv.URL + "/result.png"}},
		})
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	a, err := NewAzure(AzureConfig{BaseURL: srv.URL, APIKey: "k", Deployment: "d"})
	if err != nil {
		t.Fatal(err)
	}
	img, err := a.Generate(context.Background(), Request{Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if string(img.Data) != string(png) {
		t.Fatalf("img.Data = %q", img.Data)
	}
}

func TestAzureGenerateErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"content policy violation"}}`))
	}))
	t.Cleanup(srv.Close)
	a, err := NewAzure(AzureConfig{BaseURL: srv.URL, APIKey: "k", Deployment: "d"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Generate(context.Background(), Request{Prompt: "p"})
	if err == nil || !strings.Contains(err.Error(), "content policy violation") {
		t.Fatalf("err = %v", err)
	}
	// 缺 prompt 不打请求
	if _, err := a.Generate(context.Background(), Request{}); err == nil {
		t.Fatal("expected error for missing prompt")
	}
	if _, err := a.Generate(context.Background(), Request{Prompt: "p", AspectRatio: "2:1"}); err == nil {
		t.Fatal("expected error for unsupported aspect ratio")
	}
}

func TestNewAzureValidation(t *testing.T) {
	cases := []AzureConfig{
		{APIKey: "k", Deployment: "d"},                                         // 缺 baseUrl
		{BaseURL: "http://x", Deployment: "d"},                                 // 缺 key
		{BaseURL: "http://x", APIKey: "k"},                                     // 缺 deployment
		{BaseURL: "relative", APIKey: "k", Deployment: "d"},                    // 非绝对 URL
		{BaseURL: "http://x", APIKey: "k", Deployment: "d", ProxyURL: ":::"},   // 坏代理
		{BaseURL: "http://x", APIKey: "k", Deployment: "d", ProxyURL: "proxy"}, // 相对代理
	}
	for i, cfg := range cases {
		if _, err := NewAzure(cfg); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
}
