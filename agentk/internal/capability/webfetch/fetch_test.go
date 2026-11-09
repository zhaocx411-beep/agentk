package webfetch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentk/internal/tool"
)

// fetchResult 用于解析 web_fetch 返回结果。
type fetchResult struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
	Chars       int    `json:"chars"`
	Offset      int    `json:"offset"`
	NextOffset  int    `json:"nextOffset"`
	HasMore     bool   `json:"hasMore"`
	Note        string `json:"note"`
}

// runFetch 在给定的 spec 上执行一次 web_fetch（翻页用例要复用同一个 spec，
// 缓存挂在 spec 上）。
func runFetch(t *testing.T, spec tool.Spec, input string) fetchResult {
	t.Helper()
	out, err := spec.Run(context.Background(), json.RawMessage(input), tool.Context{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var got fetchResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

// newFetchTool 用注入的客户端构造一个 web_fetch。
func newFetchTool(t *testing.T, client *http.Client) tool.Spec {
	t.Helper()
	spec, err := BuildFetchTool(Config{Client: client})
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

// execFetch 单次抓取（不涉及翻页的用例用它）。
func execFetch(t *testing.T, client *http.Client, input string) fetchResult {
	t.Helper()
	return runFetch(t, newFetchTool(t, client), input)
}

const samplePage = `<!doctype html>
<html><head>
  <title>  测试页面  </title>
  <style>body { color: red }</style>
  <script>var noise = "不该出现";</script>
</head>
<body>
  <nav><a href="/">首页</a></nav>
  <h1>标题一</h1>
  <p>第一段
     跨行的<b>正文</b>内容。</p>
  <p>第二段。</p>
  <noscript>请开启脚本</noscript>
</body></html>`

// TestFetchHTML HTML 抓取：标题单列，脚本/样式整棵丢弃，段落边界保留成换行，
// 源码里的排版换行折叠成空格。
func TestFetchHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request must carry a User-Agent")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(samplePage))
	}))
	t.Cleanup(srv.Close)

	got := execFetch(t, srv.Client(), `{"url":"`+srv.URL+`"}`)
	if got.Title != "测试页面" || got.ContentType != "text/html" || got.HasMore {
		t.Fatalf("result = %+v", got)
	}
	for _, noise := range []string{"var noise", "color: red", "请开启脚本"} {
		if strings.Contains(got.Content, noise) {
			t.Fatalf("content must not contain %q:\n%s", noise, got.Content)
		}
	}
	// 段落各成一行，行内跨行文本折叠成空格且不丢内联标签里的字
	lines := strings.Split(got.Content, "\n")
	var joined string
	for _, line := range lines {
		joined += line + "|"
	}
	if !strings.Contains(joined, "第一段 跨行的正文内容。|") {
		t.Fatalf("paragraph handling:\n%s", got.Content)
	}
	if !strings.Contains(joined, "第二段。|") || !strings.Contains(joined, "标题一|") {
		t.Fatalf("block boundaries:\n%s", got.Content)
	}
}

// mediaPage 覆盖真实商品页上的几种图片写法：站点图标、追踪像素、内联 data URI、
// 矢量图标、把缩略图放 src 而原图放 data-old-hires 的主图、<picture> 候选、
// <video> 的 <source>，以及同一张图在页面上出现两次。
const mediaPage = `<!doctype html>
<html><head><title>商品页</title></head>
<body>
  <img src="/logo.png" width="24" height="24" alt="站点图标">
  <img src="https://tracker.example/p.gif" width="1" height="1">
  <img src="data:image/png;base64,iVBORw0K">
  <img src="/icons/cart.svg">
  <h1>无线鼠标</h1>
  <img src="https://img.example/mouse._SX300_QL70_.jpg"
       data-old-hires="https://img.example/mouse._SL1500_.jpg" alt="无线鼠标主图">
  <p>￥199</p>
  <div><img src="detail/photo.jpg"></div>
  <picture>
    <source srcset="https://img.example/side.webp 2x">
    <img src="https://img.example/side.jpg">
  </picture>
  <video poster="https://img.example/poster.jpg">
    <source src="https://vid.example/demo.mp4" type="video/mp4">
  </video>
  <video src="https://vid.example/inline.webm"></video>
  <img srcset="https://img.example/only-srcset.jpg 2x">
  <div><img src="https://img.example/mouse._SL1500_.jpg"></div>
</body></html>`

// TestFetchInlinesMedia 验证图片与视频地址内联进正文。这是"从页面取图再交给生成类
// 工具"链路的起点；如果地址丢失，模型只能根据文字猜测 URL。
func TestFetchInlinesMedia(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(mediaPage))
	}))
	t.Cleanup(srv.Close)

	got := execFetch(t, srv.Client(), `{"url":"`+srv.URL+`"}`)
	hires := "![](https://img.example/mouse._SL1500_.jpg)"

	for _, want := range []string{
		hires,
		"![](https://img.example/side.jpg)",
		"[video](https://vid.example/demo.mp4)",
		// <video> 自带 src（无 <source> 子元素）也要出标记
		"[video](https://vid.example/inline.webm)",
		// 只有 srcset 没有 src 的图：srcset 是兜底路径，不能整张图丢掉
		"![](https://img.example/only-srcset.jpg)",
		// 相对路径按落地地址补全：只有绝对地址才转手交得给下游工具
		"![](" + srv.URL + "/detail/photo.jpg)",
	} {
		if !strings.Contains(got.Content, want) {
			t.Fatalf("content must contain %q:\n%s", want, got.Content)
		}
	}
	for _, noise := range []string{
		"_SX300_QL70_",    // 主图取 data-old-hires 的原图，不取 src 的缩略图
		"tracker.example", // 1x1 追踪像素
		"/logo.png",       // 24x24 站点图标
		"data:image",      // 内联 data URI 不是能转手的地址
		"cart.svg",        // 矢量图标不是素材
		"side.webp",       // <picture> 的候选与其 <img> 兜底是同一张图
	} {
		if strings.Contains(got.Content, noise) {
			t.Fatalf("content must not contain %q:\n%s", noise, got.Content)
		}
	}
	// 同一地址只内联一次：图片在页面上重复出现很常见，重复内联只会占用预算。
	if n := strings.Count(got.Content, hires); n != 1 {
		t.Fatalf("hires marker appeared %d times, want 1:\n%s", n, got.Content)
	}
	// 位置要保留：主图标记落在标题与价格之间。汇总成清单就没有这个信号了，
	// 而它正是模型分辨主图与推荐位的依据。
	title, marker, price := indexOfLine(t, got.Content, "无线鼠标"),
		indexOfLine(t, got.Content, hires), indexOfLine(t, got.Content, "￥199")
	if title >= marker || marker >= price {
		t.Fatalf("marker must sit between the heading and the price:\n%s", got.Content)
	}
}

// indexOfLine 返回首个等于 want 的行的序号（找不到就让用例失败）。
func indexOfLine(t *testing.T, content, want string) int {
	t.Helper()
	for i, line := range strings.Split(content, "\n") {
		if line == want {
			return i
		}
	}
	t.Fatalf("no line equal to %q:\n%s", want, content)
	return -1
}

// longPage 起一个正文为 n 个"字"的测试站点，并统计被请求了几次。
func longPage(t *testing.T, n int) (*httptest.Server, *int) {
	t.Helper()
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><p>" + strings.Repeat("字", n) + "</p></body></html>"))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// TestFetchPaging 长页面按 offset 分段读：单次只回一段，nextOffset 指向后文。
// 这是 web_fetch 的主要路径：抓回的内容会长期占用上下文，全文一次性返回
// 等于给之后每一步都加上这份重量。
func TestFetchPaging(t *testing.T) {
	srv, hits := longPage(t, 700)
	spec := newFetchTool(t, srv.Client())

	first := runFetch(t, spec, `{"url":"`+srv.URL+`","maxChars":500}`)
	if first.Chars != 700 || first.Offset != 0 || !first.HasMore || first.NextOffset != 500 {
		t.Fatalf("first page = %+v", first)
	}
	// 按字符切：700 个中文按字节截会切碎多字节序列，长度也对不上 500
	if n := len([]rune(first.Content)); n != 500 {
		t.Fatalf("first page runes = %d, want 500", n)
	}
	if first.Note == "" {
		t.Fatal("truncated result must tell the model how to continue")
	}

	second := runFetch(t, spec, `{"url":"`+srv.URL+`","offset":500,"maxChars":500}`)
	if second.Offset != 500 || second.HasMore || second.NextOffset != 0 {
		t.Fatalf("second page = %+v", second)
	}
	if n := len([]rune(second.Content)); n != 200 {
		t.Fatalf("second page runes = %d, want 200", n)
	}

	// 翻页复用缓存：整页不该为了取后半段再下载一遍
	if *hits != 1 {
		t.Fatalf("origin hits = %d, want 1 (paging must reuse the cached page)", *hits)
	}
}

// TestFetchDefaultBudget 不给 maxChars 时用缺省预算，且缺省远小于整页——
// 默认行为就该是"先看一段"，而不是把全文倒进上下文。
func TestFetchDefaultBudget(t *testing.T) {
	srv, _ := longPage(t, defaultMaxChars*3)
	got := execFetch(t, srv.Client(), `{"url":"`+srv.URL+`"}`)
	if n := len([]rune(got.Content)); n != defaultMaxChars {
		t.Fatalf("content runes = %d, want the default budget %d", n, defaultMaxChars)
	}
	if !got.HasMore || got.NextOffset != defaultMaxChars {
		t.Fatalf("result = %+v", got)
	}
}

// TestFetchMaxCharsClamped maxChars 超过上限时收敛到上限而不是报错——
// 模型填写过大值时不应让整次抓取失败，但也不能突破实际上限。
func TestFetchMaxCharsClamped(t *testing.T) {
	srv, _ := longPage(t, maxMaxChars+500)
	got := execFetch(t, srv.Client(), `{"url":"`+srv.URL+`","maxChars":999999}`)
	if n := len([]rune(got.Content)); n != maxMaxChars {
		t.Fatalf("content runes = %d, want clamped to %d", n, maxMaxChars)
	}
	if !got.HasMore {
		t.Fatalf("result = %+v", got)
	}
}

// TestFetchOffsetBounds offset 越界要明确报错，而不是回一段空正文——
// 后者会让模型以为页面到此为止。
func TestFetchOffsetBounds(t *testing.T) {
	srv, _ := longPage(t, 100)
	spec := newFetchTool(t, srv.Client())
	for _, input := range []string{
		`{"url":"` + srv.URL + `","offset":100}`,
		`{"url":"` + srv.URL + `","offset":5000}`,
		`{"url":"` + srv.URL + `","offset":-1}`,
	} {
		if _, err := spec.Run(context.Background(), json.RawMessage(input), tool.Context{}); err == nil {
			t.Fatalf("expected error for %s", input)
		}
	}
}

// TestFetchNonHTML JSON 等文本类型原样返回，不过 HTML 解析。
func TestFetchNonHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	got := execFetch(t, srv.Client(), `{"url":"`+srv.URL+`"}`)
	if got.ContentType != "application/json" || got.Content != `{"ok":true}` {
		t.Fatalf("result = %+v", got)
	}
}

// TestFetchGBK 中文站点常见的 GBK 页面按 meta 声明解码成 UTF-8——
// 不转换会导致正文乱码；乱码进入上下文比抓取失败更难发现，因为模型无法判断
// 内容已经损坏。
func TestFetchGBK(t *testing.T) {
	// "中文" 的 GBK 字节
	gbk := []byte{0xd6, 0xd0, 0xce, 0xc4}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=gbk")
		_, _ = w.Write(append([]byte("<html><body><p>"), append(gbk, []byte("</p></body></html>")...)...))
	}))
	t.Cleanup(srv.Close)

	if got := execFetch(t, srv.Client(), `{"url":"`+srv.URL+`"}`); got.Content != "中文" {
		t.Fatalf("content = %q, want 中文", got.Content)
	}
}

// TestFetchRedirect 跟随重定向，报出落地地址而不是入口短链。
func TestFetchRedirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /final", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><p>落地页</p></body></html>"))
	})
	mux.HandleFunc("GET /short", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	got := execFetch(t, srv.Client(), `{"url":"`+srv.URL+`/short"}`)
	if got.URL != srv.URL+"/final" || got.Content != "落地页" {
		t.Fatalf("result = %+v", got)
	}
}

// TestFetchRejects 非 http(s) scheme、非文本内容与非 2xx 都以错误回给模型。
func TestFetchRejects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/binary":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 0x50})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	spec, err := BuildFetchTool(Config{Client: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{
		`{"url":"file:///etc/passwd"}`,
		`{"url":"ftp://example.com/x"}`,
		`{"url":"/relative/path"}`,
		`{"url":""}`,
		`{"url":"` + srv.URL + `/binary"}`,
		`{"url":"` + srv.URL + `/missing"}`,
	} {
		if _, err := spec.Run(context.Background(), json.RawMessage(input), tool.Context{}); err == nil {
			t.Fatalf("expected error for %s", input)
		}
	}
}

// TestFetchSpec 契约：纯读、url 必填、带自己的超时。
func TestFetchSpec(t *testing.T) {
	spec, err := BuildFetchTool(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Name != FetchToolName || spec.Timeout != defaultTimeout {
		t.Fatalf("spec = %+v", spec)
	}
	// 声明历史瘦身：当轮全文可见，之后的 run 回放只留开头一段
	if spec.HistoryTrim == nil ||
		spec.HistoryTrim.Field != "content" || spec.HistoryTrim.MaxChars != historyTrimChars {
		t.Fatalf("history trim = %+v", spec.HistoryTrim)
	}
	if historyTrimChars >= defaultMaxChars {
		t.Fatalf("history budget %d must be well below the live budget %d",
			historyTrimChars, defaultMaxChars)
	}
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(spec.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "url" {
		t.Fatalf("required = %v", schema.Required)
	}
	// 无效代理应在装配期报错，不能延迟到调用时。
	if _, err := BuildFetchTool(Config{ProxyURL: "::: bad"}); err == nil {
		t.Fatal("bad proxy url must fail at build time")
	}
	if _, err := BuildFetchTool(Config{ProxyURL: "no-scheme"}); err == nil {
		t.Fatal("relative proxy url must fail at build time")
	}
}
