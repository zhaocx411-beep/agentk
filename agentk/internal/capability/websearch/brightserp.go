package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// brightSERPEndpoint Bright Data 的通用抓取端点（条目没写 baseUrl 时用它）。
const brightSERPEndpoint = "https://api.brightdata.com/request"

// googleSearchURL 是检索目标。Bright Data 的入参与火山完全不同：它接收"要抓取
// 哪个 URL"，所有检索语义都必须编码进该地址的查询串。由客户端统一拼接 URL，
// 避免模型自行在 query 中拼接 site: 和 tbs=。
const googleSearchURL = "https://www.google.com/search"

// googleTimeRange 是公共档位到 Google tbs 参数的映射。
// Google 另有 qdr:h（过去一小时），火山没有对应档，故不在能力层暴露。
var googleTimeRange = map[string]string{
	TimeRangeDay:   "qdr:d",
	TimeRangeWeek:  "qdr:w",
	TimeRangeMonth: "qdr:m",
	TimeRangeYear:  "qdr:y",
}

// BrightSERPConfig 配置 Bright Data SERP API。
type BrightSERPConfig struct {
	// BaseURL 完整的请求端点；空用 brightSERPEndpoint
	BaseURL string
	// APIKey 账号 API Key，按 Bearer 下发
	APIKey string
	// Zone Bright Data 控制台里的 zone 名：决定 targeting 规则、输出偏好与计费归属。
	// 由目录条目的 modelName 承载：它是上游标识，与 Kling 条目用 modelName
	// 表示具体模型的作用相同。
	Zone string
	// ProxyURL 可选出网代理
	ProxyURL string
	// Client 供测试注入
	Client *http.Client
}

// BrightSERP 实现 Bright Data 的 SERP 抓取协议：POST 一个目标 URL，
// 由上游代抓并解析，拿回结构化结果。
type BrightSERP struct {
	endpoint string
	apiKey   string
	zone     string
	http     *http.Client
}

var _ Client = (*BrightSERP)(nil)

func NewBrightSERP(cfg BrightSERPConfig) (*BrightSERP, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("websearch: brightserp api key is required")
	}
	if cfg.Zone == "" {
		return nil, fmt.Errorf("websearch: brightserp zone is required (set it as the catalog entry's modelName)")
	}
	endpoint, err := resolveEndpoint(cfg.BaseURL, brightSERPEndpoint)
	if err != nil {
		return nil, err
	}
	client := cfg.Client
	if client == nil {
		if client, err = newHTTPClient(cfg.ProxyURL); err != nil {
			return nil, err
		}
	}
	return &BrightSERP{endpoint: endpoint, apiKey: cfg.APIKey, zone: cfg.Zone, http: client}, nil
}

func (b *BrightSERP) Search(ctx context.Context, req Request) ([]Result, error) {
	if err := validateRequest(req); err != nil {
		return nil, err
	}
	target, err := buildGoogleURL(req)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]any{
		"zone": b.zone,
		"url":  target,
		// format=raw：响应体就是上游解析好的 SERP JSON（解析由 URL 上的
		// brd_json=1 触发，见 buildGoogleURL）。
		// 不能用 format=json——那个"json"指的是把 HTTP 响应本身包成
		// {status_code, headers, body} 的信封，body 仍是 Google 的原始 HTML；
		// 它会正常解码成一个没有 organic 字段的对象，于是静默回零条结果。
		"format": "raw",
	})
	if err != nil {
		return nil, err
	}

	var resp brightSERPResponse
	if err := b.call(ctx, payload, &resp); err != nil {
		return nil, fmt.Errorf("websearch: brightserp search: %w", err)
	}
	out := make([]Result, 0, len(resp.Organic))
	for _, item := range resp.Organic {
		out = append(out, Result{Title: item.Title, URL: item.Link, Summary: item.Description})
	}
	return normalizeResults(out), nil
}

// buildGoogleURL 把公共请求编码成一个 Google 检索地址。
// q 手工拼在最前面而不是用 url.Values.Encode()：后者按键名排序，而上游文档明示
// q 必须是第一个查询参数。
//
// 不带 num：Google 已弃用它，而上游本来就回 10 条 organic（正好是 defaultCount），
// 继续发送已弃用参数只会增加被判定为异常流量的可能。
func buildGoogleURL(req Request) (string, error) {
	params := "q=" + url.QueryEscape(req.Query)
	// 语言与地区固定为中文：上游默认跟随出口 IP（实测返回过 en-ID/印尼），
	// 对中文检索通常不正确。不做成工具入参，因为模型没有依据选择地区，
	// 而检索语言跟着 query 的语言走已经够用。
	params += "&hl=zh-CN&gl=cn"
	if req.TimeRange != "" {
		mapped, ok := googleTimeRange[req.TimeRange]
		if !ok {
			// 与火山相同：缺少映射时直接失败，不能静默退化成"未筛选时间"。
			return "", fmt.Errorf("websearch: brightserp has no mapping for time range %q", req.TimeRange)
		}
		params += "&tbs=" + url.QueryEscape(mapped)
	}
	// brd_json=1 是上游解析 SERP 的开关：带上它响应体才是结构化结果，
	// 否则回来的是 Google 原始 HTML。它是给代理看的指令而不是 Google 的参数，
	// 故排在最后，不与检索语义混在一起。
	params += "&brd_json=1"
	return googleSearchURL + "?" + params, nil
}

func (b *BrightSERP) call(ctx context.Context, payload []byte, out any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, b.endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+b.apiKey)

	response, err := b.http.Do(request)
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
	// Bright Data 的失败会封装在 200 响应中，必须检查响应体，否则会被当成"没搜到"：
	//   空体      —— 本次抓取在上游失败（实测相当常见）
	//   纯文本    —— 上游的说明，例如"这条 query 刚失败过，15 秒内不再受理"
	// 两种都是瞬时故障，继续返回错误交给 Activity 退避重试。
	if len(bytes.TrimSpace(raw)) == 0 {
		return fmt.Errorf("upstream returned an empty body (the fetch failed on their side)")
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w; body: %s", err, snippet(raw))
	}
	return nil
}

// brightSERPResponse 只声明 organic 一段。响应体里还有 people_also_ask、
// perspectives、related、pagination、knowledge、snack_pack 等大量附加段，organic
// 每条还内联 base64 的 icon 与缩略图；全部保留时一次检索就会占用大半个上下文，
// 因此只声明实际使用的三个字段，其余字段在解码时自然丢弃。
type brightSERPResponse struct {
	Organic []struct {
		Title       string `json:"title"`
		Link        string `json:"link"`
		Description string `json:"description"`
	} `json:"organic"`
}
