package websearch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// brightOKBody 按实测响应裁剪：字段名与顺序取自真实返回。organic 之外还有大量
// 段落，每条结果还内联 base64 图标——一并放进来，验证它们会被解码丢弃。
const brightOKBody = `{
  "general": {"search_engine": "google", "results_cnt": 12300, "language": "zh-CN"},
  "input": {"original_url": "https://www.google.com/search?q=x&brd_json=1"},
  "navigation": [{"title": "Images", "href": "https://www.google.com/search?udm=2"}],
  "organic": [
    {"rank": 1, "global_rank": 1, "title": "Go 1.26 Release Notes",
     "link": "https://go.dev/doc/go1.26", "description": "本次发布的变更…",
     "display_link": "https://go.dev › doc › go1.26", "source": "go.dev",
     "snippet_highlighted_words": ["Go 1.26"],
     "icon": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB"},
    {"rank": 2, "title": "无链接的那条", "description": "应被丢弃"}
  ],
  "people_also_ask": [{"question": "Go 1.26 什么时候发布"}],
  "related": [{"text": "golang 新特性"}]
}`

func newBrightServer(t *testing.T, handler http.HandlerFunc) *BrightSERP {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client, err := NewBrightSERP(BrightSERPConfig{
		BaseURL: srv.URL, APIKey: "bd-test", Zone: "search_x", Client: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestBrightSERPSearch(t *testing.T) {
	var gotBody map[string]any
	var gotAuth string
	client := newBrightServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = io.WriteString(w, brightOKBody)
	})

	results, err := client.Search(context.Background(), Request{Query: "Go 1.26"})
	if err != nil {
		t.Fatal(err)
	}

	if gotAuth != "Bearer bd-test" {
		t.Fatalf("auth = %q", gotAuth)
	}
	// zone 来自目录条目的 modelName。format 必须是 raw：上游的 "json" 指的是
	// 把 HTTP 响应包成 {status_code, headers, body} 信封，body 仍是 Google 原始
	// HTML——它能正常解码成一个没有 organic 的对象，于是静默回零条。
	if gotBody["zone"] != "search_x" || gotBody["format"] != "raw" {
		t.Fatalf("body = %v", gotBody)
	}

	if len(results) != 1 {
		t.Fatalf("results = %d, want 1（无链接的那条应被丢弃）", len(results))
	}
	if results[0].Title != "Go 1.26 Release Notes" || results[0].URL != "https://go.dev/doc/go1.26" ||
		results[0].Summary != "本次发布的变更…" {
		t.Fatalf("首条 = %+v", results[0])
	}
}

// TestBuildGoogleURL 的核心是把公共请求编码成 Bright Data 要抓取的检索地址，
// 因此需要逐项固定 URL 的结构。
func TestBuildGoogleURL(t *testing.T) {
	got, err := buildGoogleURL(Request{Query: "深圳 天气", TimeRange: TimeRangeDay})
	if err != nil {
		t.Fatal(err)
	}
	// q 必须是第一个查询参数（上游文档明示顺序敏感），所以不能用 Values.Encode()
	if !strings.HasPrefix(got, googleSearchURL+"?q=") {
		t.Fatalf("q 不在最前: %s", got)
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	q := parsed.Query()
	if q.Get("q") != "深圳 天气" {
		t.Fatalf("q = %q", q.Get("q"))
	}
	if q.Get("tbs") != "qdr:d" {
		t.Fatalf("tbs = %q, want qdr:d", q.Get("tbs"))
	}
	if q.Get("hl") != "zh-CN" || q.Get("gl") != "cn" {
		t.Fatalf("语言/地区 = %q / %q", q.Get("hl"), q.Get("gl"))
	}
	// 少了这个开关，回来的就是 Google 原始 HTML 而不是结构化结果
	if q.Get("brd_json") != "1" {
		t.Fatalf("缺少 brd_json 开关: %s", got)
	}
	// num 已被 Google 弃用，且上游本来就回 10 条：不带
	if q.Has("num") {
		t.Fatalf("不该带已弃用的 num: %s", got)
	}

	// 不填档位就不带 tbs，而不是带个空值
	got, err = buildGoogleURL(Request{Query: "q"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "tbs=") {
		t.Fatalf("不限时不该带 tbs: %s", got)
	}
}

// TestBrightSERPFailuresInsideHTTP200 验证封装在 200 响应中的失败：抓取失败返回空体，
// 冷却期返回一段纯文本。两种都必须作为错误上报；如果当成"没搜到"，模型只会更换
// 检索词再次请求，而这正是应该退避重试的情形。
func TestBrightSERPFailuresInsideHTTP200(t *testing.T) {
	const cooldown = "This query recently failed and cannot be attempted at this time. " +
		"Please try again later, after a minimum of 15 seconds."
	cases := map[string]string{
		"空体":  "",
		"空白体": "  \n ",
		"纯文本": cooldown,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			client := newBrightServer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, body)
			})
			_, err := client.Search(context.Background(), Request{Query: "q"})
			if err == nil {
				t.Fatal("200 包着的失败必须上报为错误，不能当成零条结果")
			}
		})
	}

	// 冷却提示的原文要带进错误信息：没有它就只剩一个"decode 失败"，
	// 排障时看不出该等一会儿再试
	client := newBrightServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, cooldown)
	})
	_, err := client.Search(context.Background(), Request{Query: "q"})
	if err == nil || !strings.Contains(err.Error(), "15 seconds") {
		t.Fatalf("err = %v，应带上游原文", err)
	}
}

func TestBrightSERPHTTPError(t *testing.T) {
	client := newBrightServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "bad zone")
	})
	_, err := client.Search(context.Background(), Request{Query: "q"})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want 401", err)
	}
}

func TestNewBrightSERPValidation(t *testing.T) {
	if _, err := NewBrightSERP(BrightSERPConfig{Zone: "z"}); err == nil {
		t.Fatal("缺 apiKey 应报错")
	}
	// zone 决定计费归属与产品配置，缺了必须当场失败而不是发一个必然被拒的请求
	if _, err := NewBrightSERP(BrightSERPConfig{APIKey: "k"}); err == nil {
		t.Fatal("缺 zone 应报错")
	}
	if _, err := NewBrightSERP(BrightSERPConfig{APIKey: "k", Zone: "z"}); err != nil {
		t.Fatalf("省略 baseUrl 应回落官方端点: %v", err)
	}
}
