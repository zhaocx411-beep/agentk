package websearch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// volcOKBody 是上游成功响应格式（字段名取自火山官方 sample 的解析代码：
// Result.WebResults[] 里是 Title / Url / Summary / Snippet）。
const volcOKBody = `{
  "ResponseMetadata": {"RequestId": "req-1"},
  "Result": {
    "ResultCount": 3,
    "TimeCost": 120,
    "WebResults": [
      {"SortId": 1, "Title": "深圳天气预报", "Url": "https://weather.test/sz",
       "Summary": "明天多云转晴", "SiteName": "中国天气网", "AuthInfoDes": "权威"},
      {"SortId": 2, "Title": "没有摘要只有片段", "Url": "https://b.test", "Snippet": "退回片段"},
      {"SortId": 3, "Title": "没有链接", "Summary": "应被丢弃"}
    ]
  }
}`

func newVolcServer(t *testing.T, handler http.HandlerFunc) (*Volc, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client, err := NewVolc(VolcConfig{BaseURL: srv.URL, APIKey: "sk-test", Client: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return client, srv
}

func TestVolcSearch(t *testing.T) {
	var gotBody map[string]any
	var gotAuth, gotContentType string
	client, _ := newVolcServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = io.WriteString(w, volcOKBody)
	})

	results, err := client.Search(context.Background(), Request{Query: "深圳天气", TimeRange: TimeRangeWeek})
	if err != nil {
		t.Fatal(err)
	}

	if gotAuth != "Bearer sk-test" || gotContentType != "application/json" {
		t.Fatalf("headers = %q / %q", gotAuth, gotContentType)
	}
	if gotBody["Query"] != "深圳天气" || gotBody["SearchType"] != "web" {
		t.Fatalf("body = %v", gotBody)
	}
	// 公共档位转换成火山取值，schema 中的写法不会直接传给上游。
	if gotBody["TimeRange"] != "OneWeek" {
		t.Fatalf("TimeRange = %v, want OneWeek", gotBody["TimeRange"])
	}
	if gotBody["NeedSummary"] != true {
		t.Fatalf("NeedSummary = %v, want true", gotBody["NeedSummary"])
	}
	filter, _ := gotBody["Filter"].(map[string]any)
	if filter["NeedUrl"] != true || filter["NeedContent"] != false {
		t.Fatalf("Filter = %v（要链接不要正文）", filter)
	}

	if len(results) != 2 {
		t.Fatalf("results = %d, want 2（无链接的那条应被丢弃）", len(results))
	}
	if results[0].Title != "深圳天气预报" || results[0].URL != "https://weather.test/sz" ||
		results[0].Summary != "明天多云转晴" {
		t.Fatalf("首条 = %+v", results[0])
	}
	// Summary 缺失时退回 Snippet
	if results[1].Summary != "退回片段" {
		t.Fatalf("次条摘要 = %q", results[1].Summary)
	}
}

// TestVolcSearchOmitsTimeRange 不填档位时不带 TimeRange 字段，而不是发个空串上去。
func TestVolcSearchOmitsTimeRange(t *testing.T) {
	var gotBody map[string]any
	client, _ := newVolcServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = io.WriteString(w, volcOKBody)
	})
	if _, err := client.Search(context.Background(), Request{Query: "q"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := gotBody["TimeRange"]; ok {
		t.Fatalf("不限时不该带 TimeRange: %v", gotBody)
	}
}

// TestVolcBusinessError 上游 HTTP 200 也可能带业务错误（额度耗尽、限流、参数非法）。
// 不检查该字段会把失败当成"没搜到"，模型只会更换检索词再次请求。
func TestVolcBusinessError(t *testing.T) {
	client, _ := newVolcServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ResponseMetadata":{"Error":{"Code":"10406","Message":"free quota exhausted"}}}`)
	})
	_, err := client.Search(context.Background(), Request{Query: "q"})
	if err == nil {
		t.Fatal("want error for business failure carried in a 200 response")
	}
	if !strings.Contains(err.Error(), "10406") || !strings.Contains(err.Error(), "quota") {
		t.Fatalf("错误信息应带上游码与说明: %v", err)
	}
}

func TestVolcHTTPError(t *testing.T) {
	client, _ := newVolcServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, "rate limited")
	})
	_, err := client.Search(context.Background(), Request{Query: "q"})
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("err = %v, want 429", err)
	}
}

// TestVolcConcurrencyCap 上游并发配额是 5：客户端要把超出的请求挡在发出去之前，
// 而不是等它回 429 再退避。
func TestVolcConcurrencyCap(t *testing.T) {
	var inflight, peak int64
	client, _ := newVolcServer(t, func(w http.ResponseWriter, _ *http.Request) {
		now := atomic.AddInt64(&inflight, 1)
		for {
			old := atomic.LoadInt64(&peak)
			if now <= old || atomic.CompareAndSwapInt64(&peak, old, now) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt64(&inflight, -1)
		_, _ = io.WriteString(w, volcOKBody)
	})

	var wg sync.WaitGroup
	for range volcMaxConcurrency * 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.Search(context.Background(), Request{Query: "q"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt64(&peak); got > volcMaxConcurrency {
		t.Fatalf("并发峰值 = %d，超过配额 %d", got, volcMaxConcurrency)
	}
}

// TestVolcCanceledWhileQueued 排队等配额时 ctx 取消要立即返回，
// 不能一直占着 activity 的槽位。
func TestVolcCanceledWhileQueued(t *testing.T) {
	release := make(chan struct{})
	client, _ := newVolcServer(t, func(w http.ResponseWriter, _ *http.Request) {
		<-release
		_, _ = io.WriteString(w, volcOKBody)
	})
	var wg sync.WaitGroup
	for range volcMaxConcurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = client.Search(context.Background(), Request{Query: "q"})
		}()
	}
	// 等配额被占满
	deadline := time.Now().Add(2 * time.Second)
	for len(client.sem) < volcMaxConcurrency && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.Search(ctx, Request{Query: "q"})
	close(release)
	wg.Wait()
	if err == nil {
		t.Fatal("已取消的 ctx 应立即失败")
	}
}

func TestNewVolcValidation(t *testing.T) {
	if _, err := NewVolc(VolcConfig{}); err == nil {
		t.Fatal("缺 apiKey 应报错")
	}
	if _, err := NewVolc(VolcConfig{APIKey: "k", BaseURL: "relative/path"}); err == nil {
		t.Fatal("相对 baseUrl 应报错")
	}
	if _, err := NewVolc(VolcConfig{APIKey: "k"}); err != nil {
		t.Fatalf("省略 baseUrl 应回落官方端点: %v", err)
	}
}
