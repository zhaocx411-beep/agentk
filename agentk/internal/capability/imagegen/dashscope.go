package imagegen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// dashscope 实现：阿里云 DashScope 的 multimodal-generation 同步生图协议
//（z-image-turbo 等）。请求体是 model + input.messages + parameters 三段式，
// Bearer 鉴权；响应从 output.choices[].message.content[] 里取图。
// 注意：返回的是阿里 OSS 的临时 URL（24 小时有效），必须当场下载转存，
// 所以该实现必须执行 download，这不是可选优化。
// 公网服务：IDC 环境需配 proxyUrl。

// DefaultDashScopeBaseURL 官网端点；条目省略 baseUrl 时用它。
const DefaultDashScopeBaseURL = "https://dashscope.aliyuncs.com"

// dashscopeSizes 画幅 → 推荐分辨率（官方 1280*1280 像素档）。
var dashscopeSizes = map[string]string{
	"1:1":  "1280*1280",
	"9:16": "864*1536",
	"16:9": "1536*864",
}

type DashScopeConfig struct {
	// BaseURL 空用 DefaultDashScopeBaseURL（官网）
	BaseURL string
	// APIKey Bearer 鉴权（sk-xxx），必填
	APIKey string
	// Model API 请求的 model 字段（即 profile 条目名，如 z-image-turbo），必填
	Model string
	// ProxyURL 可选公网出口代理
	ProxyURL string
	// Client 供测试注入；缺省按 ProxyURL 构造
	Client *http.Client
}

type DashScope struct {
	cfg  DashScopeConfig
	http *http.Client
}

func NewDashScope(cfg DashScopeConfig) (*DashScope, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("imagegen: dashscope api key is required")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("imagegen: dashscope model name is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultDashScopeBaseURL
	}
	baseURL, err := url.Parse(cfg.BaseURL)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" {
		return nil, fmt.Errorf("imagegen: dashscope baseUrl %q must be an absolute HTTP(S) URL", cfg.BaseURL)
	}
	client := cfg.Client
	if client == nil {
		var err error
		if client, err = newHTTPClient(cfg.ProxyURL); err != nil {
			return nil, err
		}
	}
	return &DashScope{cfg: cfg, http: client}, nil
}

func (d *DashScope) endpoint() string {
	return strings.TrimSuffix(d.cfg.BaseURL, "/") + "/api/v1/services/aigc/multimodal-generation/generation"
}

func (d *DashScope) Generate(ctx context.Context, req Request) (Image, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return Image{}, fmt.Errorf("imagegen: prompt is required")
	}
	size, err := imageSize(dashscopeSizes, req.AspectRatio, "1:1")
	if err != nil {
		return Image{}, err
	}
	payload, err := json.Marshal(map[string]any{
		"model": d.cfg.Model,
		"input": map[string]any{
			"messages": []map[string]any{{
				"role":    "user",
				"content": []map[string]any{{"text": req.Prompt}},
			}},
		},
		"parameters": map[string]any{"size": size, "prompt_extend": false},
	})
	if err != nil {
		return Image{}, err
	}

	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpoint(), bytes.NewReader(payload))
	if err != nil {
		return Image{}, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Authorization", "Bearer "+d.cfg.APIKey)

	resp, err := d.http.Do(hreq)
	if err != nil {
		return Image{}, fmt.Errorf("imagegen: dashscope generate: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes))
	if err != nil {
		return Image{}, fmt.Errorf("imagegen: read response: %w", err)
	}

	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Output  struct {
			Choices []struct {
				Message struct {
					Content []struct {
						Image string `json:"image"`
					} `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		} `json:"output"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return Image{}, fmt.Errorf("imagegen: parse response: %w (%s)", err, snippet(raw))
	}
	// 失败时 code/message 有值（HTTP 状态码也非 200，两个信号都认）
	if body.Code != "" || resp.StatusCode != http.StatusOK {
		return Image{}, fmt.Errorf("imagegen: dashscope generate rejected: HTTP %d code=%q message=%q",
			resp.StatusCode, body.Code, body.Message)
	}
	var imageURL string
	for _, c := range body.Output.Choices {
		for _, part := range c.Message.Content {
			if part.Image != "" {
				imageURL = part.Image
				break
			}
		}
	}
	if imageURL == "" {
		return Image{}, fmt.Errorf("imagegen: no image in response: %s", snippet(raw))
	}
	data, err := download(ctx, d.http, imageURL)
	if err != nil {
		return Image{}, fmt.Errorf("imagegen: %w", err)
	}
	return Image{Data: data, MIME: "image/png", Params: map[string]any{"size": size}}, nil
}
