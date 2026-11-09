package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// azure 实现：Azure OpenAI 旧版 deployment-scoped 生图协议（经内部网关转发）。
// 特征：deployment 名嵌在路径、api-version 查询参数、api-key 头鉴权；
// 请求体不放 model 字段——deployment 与模型名不一致时 body 里的 model 会导致
// 请求失败（openai-python #2892），路由完全靠 URL。

// DefaultAzureAPIVersion 条目省略 apiVersion 时的缺省。
const DefaultAzureAPIVersion = "2024-02-01"

// azureSizes 画幅 → gpt-image 支持的 size 枚举。
var azureSizes = map[string]string{
	"1:1":  "1024x1024",
	"9:16": "1024x1536",
	"16:9": "1536x1024",
}

type AzureConfig struct {
	// BaseURL 分配地址截到 /openai/deployments 之前的部分
	//（如 http://kigress-gateway.internal/<路由前缀>），必填
	BaseURL string
	// APIKey api-key 头的值，必填
	APIKey string
	// Deployment 部署名（即 profile 条目名），必填
	Deployment string
	// APIVersion 空用 DefaultAzureAPIVersion
	APIVersion string
	// ProxyURL 可选公网出口代理；内网网关通常不需要
	ProxyURL string
	// Client 供测试注入；缺省按 ProxyURL 构造
	Client *http.Client
}

type Azure struct {
	cfg  AzureConfig
	http *http.Client
}

func NewAzure(cfg AzureConfig) (*Azure, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("imagegen: azure baseUrl is required")
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("imagegen: azure api key is required")
	}
	if cfg.Deployment == "" {
		return nil, fmt.Errorf("imagegen: azure deployment name is required")
	}
	baseURL, err := url.Parse(cfg.BaseURL)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" {
		return nil, fmt.Errorf("imagegen: azure baseUrl %q must be an absolute HTTP(S) URL", cfg.BaseURL)
	}
	if cfg.APIVersion == "" {
		cfg.APIVersion = DefaultAzureAPIVersion
	}
	client := cfg.Client
	if client == nil {
		var err error
		if client, err = newHTTPClient(cfg.ProxyURL); err != nil {
			return nil, err
		}
	}
	return &Azure{cfg: cfg, http: client}, nil
}

func (a *Azure) endpoint() string {
	return fmt.Sprintf("%s/openai/deployments/%s/images/generations?api-version=%s",
		strings.TrimSuffix(a.cfg.BaseURL, "/"), url.PathEscape(a.cfg.Deployment), url.QueryEscape(a.cfg.APIVersion))
}

func (a *Azure) Generate(ctx context.Context, req Request) (Image, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return Image{}, fmt.Errorf("imagegen: prompt is required")
	}
	size, err := imageSize(azureSizes, req.AspectRatio, "1:1")
	if err != nil {
		return Image{}, err
	}
	payload, err := json.Marshal(map[string]any{
		"prompt": req.Prompt, "size": size, "n": 1,
	})
	if err != nil {
		return Image{}, err
	}

	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint(), bytes.NewReader(payload))
	if err != nil {
		return Image{}, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("api-key", a.cfg.APIKey)

	resp, err := a.http.Do(hreq)
	if err != nil {
		return Image{}, fmt.Errorf("imagegen: azure generate: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes))
	if err != nil {
		return Image{}, fmt.Errorf("imagegen: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Image{}, fmt.Errorf("imagegen: azure generate: HTTP %d: %s", resp.StatusCode, snippet(raw))
	}

	var body struct {
		Data []struct {
			B64 string `json:"b64_json"`
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return Image{}, fmt.Errorf("imagegen: parse response: %w (%s)", err, snippet(raw))
	}
	if len(body.Data) == 0 {
		return Image{}, fmt.Errorf("imagegen: no image in response: %s", snippet(raw))
	}
	params := map[string]any{"size": size}
	item := body.Data[0]
	switch {
	case item.B64 != "":
		data, err := base64.StdEncoding.DecodeString(item.B64)
		if err != nil {
			return Image{}, fmt.Errorf("imagegen: decode b64_json: %w", err)
		}
		return Image{Data: data, MIME: "image/png", Params: params}, nil
	case item.URL != "":
		data, err := download(ctx, a.http, item.URL)
		if err != nil {
			return Image{}, fmt.Errorf("imagegen: %w", err)
		}
		return Image{Data: data, MIME: "image/png", Params: params}, nil
	default:
		return Image{}, fmt.Errorf("imagegen: no b64_json/url in response: %s", snippet(raw))
	}
}
