package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// volcEndpoint 火山联网搜索的官方端点（条目没写 baseUrl 时用它）。
const volcEndpoint = "https://open.feedcoopapi.com/search_api/web_search"

// volcMaxConcurrency 上游的并发配额（官方口径 5，超限回 429）。
//
// 写在供应商实现中而不是做成目录条目字段：这是火山的固有属性，不随部署变化，
// 应由客户端内部处理；暴露到目录只会增加一个部署方无需配置的参数。
const volcMaxConcurrency = 5

// volcTimeRange 是公共档位到火山取值的映射。
// 火山另接受 YYYY-MM-DD..YYYY-MM-DD 的自定义区间，能力层暂不暴露——
// 两家的自定义区间写法完全不同，且要处理日期合法性与先后顺序，收益不抵复杂度。
var volcTimeRange = map[string]string{
	TimeRangeDay:   "OneDay",
	TimeRangeWeek:  "OneWeek",
	TimeRangeMonth: "OneMonth",
	TimeRangeYear:  "OneYear",
}

// VolcConfig 配置火山联网搜索端点。
type VolcConfig struct {
	// BaseURL 完整的检索端点；空用 volcEndpoint
	BaseURL string
	// APIKey 控制台创建的 API Key，按 Bearer 下发
	APIKey string
	// ProxyURL 可选出网代理（IDC 环境访问公网用）
	ProxyURL string
	// Client 供测试注入；缺省按 ProxyURL 构造
	Client *http.Client
}

// Volc 实现火山联网搜索协议：POST 检索端点，一次请求拿回一页结果。
type Volc struct {
	endpoint string
	apiKey   string
	http     *http.Client
	// sem 在请求发出前限制在途数量，避免 429。它只在本进程内有效；多 Worker
	// 部署仍可能超过全局并发，因此超限错误按瞬时故障交给 Activity 退避重试。
	sem chan struct{}
}

var _ Client = (*Volc)(nil)

func NewVolc(cfg VolcConfig) (*Volc, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("websearch: volc api key is required")
	}
	endpoint, err := resolveEndpoint(cfg.BaseURL, volcEndpoint)
	if err != nil {
		return nil, err
	}
	client := cfg.Client
	if client == nil {
		if client, err = newHTTPClient(cfg.ProxyURL); err != nil {
			return nil, err
		}
	}
	return &Volc{
		endpoint: endpoint,
		apiKey:   cfg.APIKey,
		http:     client,
		sem:      make(chan struct{}, volcMaxConcurrency),
	}, nil
}

func (v *Volc) Search(ctx context.Context, req Request) ([]Result, error) {
	if err := validateRequest(req); err != nil {
		return nil, err
	}
	body := map[string]any{
		"Query":      req.Query,
		"SearchType": "web",
		"Count":      defaultCount,
		// 要摘要而不要正文：十条正文会占用大量上下文，模型先按摘要选出值得读的结果，
		// 再用 web_fetch 抓。NeedUrl 必须开——没有链接的结果既不能引用也不能再抓。
		"NeedSummary": true,
		"Filter":      map[string]any{"NeedUrl": true, "NeedContent": false},
	}
	if req.TimeRange != "" {
		mapped, ok := volcTimeRange[req.TimeRange]
		if !ok {
			// 缺少映射时不能静默放行，否则模型和用户都会以为结果已经按时间筛选，
			// 实际拿到的却是全时段结果；这种静默偏差比直接失败更难发现。
			return nil, fmt.Errorf("websearch: volc has no mapping for time range %q", req.TimeRange)
		}
		body["TimeRange"] = mapped
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	if err := v.acquire(ctx); err != nil {
		return nil, err
	}
	defer v.release()

	var resp volcResponse
	if err := v.call(ctx, payload, &resp); err != nil {
		return nil, fmt.Errorf("websearch: volc search: %w", err)
	}
	// HTTP 200 也可能带业务错误（额度耗尽、限流、参数非法都走这里）。
	// 不检查该字段会把失败当成"没搜到"，模型随后只会更换检索词再次尝试。
	if e := resp.ResponseMetadata.Error; e != nil && (e.Code != "" || e.Message != "") {
		return nil, fmt.Errorf("websearch: volc rejected the request [%s]: %s", e.Code, e.Message)
	}
	out := make([]Result, 0, len(resp.Result.WebResults))
	for _, item := range resp.Result.WebResults {
		summary := item.Summary
		if summary == "" {
			summary = item.Snippet // 没开摘要或摘要缺失时退回片段
		}
		out = append(out, Result{Title: item.Title, URL: item.URL, Summary: summary})
	}
	return normalizeResults(out), nil
}

// acquire 占用一个在途配额。ctx 取消或超时时立即返回，不无限排队：排队到工具
// 超时和立即失败对模型结果相同，但前者会持续占用 Activity 槽位。
func (v *Volc) acquire(ctx context.Context) error {
	select {
	case v.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (v *Volc) release() { <-v.sem }

func (v *Volc) call(ctx context.Context, payload []byte, out any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, v.endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+v.apiKey)

	response, err := v.http.Do(request)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("HTTP %d: %s", response.StatusCode, snippet(raw))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// volcResponse 只声明用得上的字段。响应里还有 CardResults（天气/股票等结构化
// 卡片）、SearchContext、TimeCost 等，当前一律不取——卡片各类型结构互不相同，
// 如需支持，必须先定义公共表示，不能直接透传给模型。
type volcResponse struct {
	ResponseMetadata struct {
		Error *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error"`
	} `json:"ResponseMetadata"`
	Result struct {
		WebResults []struct {
			Title   string `json:"Title"`
			URL     string `json:"Url"`
			Summary string `json:"Summary"`
			Snippet string `json:"Snippet"`
		} `json:"WebResults"`
	} `json:"Result"`
}
