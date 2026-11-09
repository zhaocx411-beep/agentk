package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"

	"agentk/internal/artifact"
)

// gptimage 实现：Azure OpenAI gpt-image 系列经快手 kigress 内网网关转发。
// 它与公网 Azure 协议同源，但有两点不同：
//   - 鉴权走 x-api-key + x-ks-* 一组网关头（逐条按原样下发），不是 azure 的 api-key；
//   - 支持参考图编辑：/images/edits（multipart，参考图作为文件段上传），gpt-image 特有。
// 一个 struct 同时实现 Client（文生图 /images/generations，JSON）与 EditClient
// （参考图生图 /images/edits），按模型条目 kind 选用对应接口。
// baseUrl 含租户 + deployment 路由前缀，截到 .../openai/deployments/{deployment}（含）；
// 端点路径与 api-version 由代码/配置拼装。响应取 data[].b64_json（gpt-image 缺省回 b64）。

const (
	// DefaultGptImageGenAPIVersion 文生图 generations 端点缺省 api-version。
	DefaultGptImageGenAPIVersion = "2024-02-01"
	// DefaultGptImageEditAPIVersion 参考图 edits 端点缺省 api-version。
	DefaultGptImageEditAPIVersion = "2025-04-01-preview"
	// DefaultGptImageQuality gpt-image 生成质量缺省。
	DefaultGptImageQuality = "medium"
)

type KSGptImageConfig struct {
	// BaseURL 网关基址，含租户 + deployment 前缀，截到 .../openai/deployments/{deployment}（含），必填。
	BaseURL string
	// APIVersion 该条目端点的 api-version。image → generations 版本（缺省 2024-02-01）；
	// image_edit → edits 版本（缺省 2025-04-01-preview）。一个条目只对应一种 kind，故一个值够用。
	APIVersion string
	// Quality low / medium / high，缺省 medium。
	Quality string
	// Headers 逐条按原样下发的网关头（x-api-key / x-ks-user-key / x-ks-llm-model / x-ks-biz-scene），
	// 至少含鉴权头，必填。Content-Type 由代码按端点设置（JSON / multipart），配置里给了也会被覆盖。
	Headers map[string]string
	// ProxyURL 可选出口代理（走隧道进内网时用）
	ProxyURL string
	// Client 供测试注入；缺省按 ProxyURL 构造
	Client *http.Client
	// Artifacts 是允许作为编辑参考图的受管产物存储。参考图必须能由它识别并读取，
	// 避免服务端按用户输入下载任意 URL 形成 SSRF。
	Artifacts artifact.Storage
}

type KSGptImage struct {
	cfg  KSGptImageConfig
	http *http.Client
}

func NewKSGptImage(cfg KSGptImageConfig) (*KSGptImage, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("imagegen: gptimage baseUrl is required")
	}
	if len(cfg.Headers) == 0 {
		return nil, fmt.Errorf("imagegen: gptimage headers are required (x-api-key / x-ks-*)")
	}
	baseURL, err := url.Parse(cfg.BaseURL)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" {
		return nil, fmt.Errorf("imagegen: gptimage baseUrl %q must be an absolute HTTP(S) URL", cfg.BaseURL)
	}
	if cfg.Quality == "" {
		cfg.Quality = DefaultGptImageQuality
	}
	switch cfg.Quality {
	case "low", "medium", "high":
	default:
		return nil, fmt.Errorf("imagegen: gptimage unsupported quality %q", cfg.Quality)
	}
	client := cfg.Client
	if client == nil {
		var err error
		if client, err = newHTTPClient(cfg.ProxyURL); err != nil {
			return nil, err
		}
	}
	return &KSGptImage{cfg: cfg, http: client}, nil
}

func (k *KSGptImage) endpoint(op, apiVersion string) string {
	return strings.TrimSuffix(k.cfg.BaseURL, "/") + "/images/" + op + "?api-version=" + apiVersion
}

// setGatewayHeaders 下发配置里的网关头（x-api-key / x-ks-*）。调用后再按端点显式设置
// Content-Type，确保 multipart 的 boundary 不被配置里误留的 Content-Type 覆盖。
func (k *KSGptImage) setGatewayHeaders(req *http.Request) {
	for hk, hv := range k.cfg.Headers {
		req.Header.Set(hk, hv)
	}
}

// Generate 文生图（/images/generations，JSON）。
func (k *KSGptImage) Generate(ctx context.Context, req Request) (Image, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return Image{}, fmt.Errorf("imagegen: prompt is required")
	}
	apiVersion := k.cfg.APIVersion
	if apiVersion == "" {
		apiVersion = DefaultGptImageGenAPIVersion
	}
	size, err := imageSize(azureSizes, req.AspectRatio, "1:1")
	if err != nil {
		return Image{}, err
	}
	payload, err := json.Marshal(map[string]any{
		"prompt": req.Prompt, "size": size, "quality": k.cfg.Quality, "n": 1,
	})
	if err != nil {
		return Image{}, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, k.endpoint("generations", apiVersion), bytes.NewReader(payload))
	if err != nil {
		return Image{}, err
	}
	k.setGatewayHeaders(hreq)
	hreq.Header.Set("Content-Type", "application/json")
	return k.do(ctx, hreq, "generations", map[string]any{"size": size, "quality": k.cfg.Quality})
}

// Edit 参考图编辑（/images/edits，multipart）。上下文里的参考图必须是受管 artifact URL，
// 逐张从存储读取为字节后作为 image[] 文件段上传（edits 端点不接受 URL，只接受文件）。
func (k *KSGptImage) Edit(ctx context.Context, req EditRequest) (Image, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return Image{}, fmt.Errorf("imagegen: prompt is required")
	}
	if len(req.ReferenceImages) == 0 {
		return Image{}, fmt.Errorf("imagegen: gptimage edits requires at least one reference image")
	}
	apiVersion := k.cfg.APIVersion
	if apiVersion == "" {
		apiVersion = DefaultGptImageEditAPIVersion
	}
	size, err := imageSize(azureSizes, req.AspectRatio, "1:1")
	if err != nil {
		return Image{}, err
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for i, ref := range req.ReferenceImages {
		if strings.TrimSpace(ref) == "" {
			return Image{}, fmt.Errorf("imagegen: reference image URL must not be empty")
		}
		data, err := k.readReference(ctx, ref)
		if err != nil {
			return Image{}, fmt.Errorf("imagegen: read reference image %d: %w", i, err)
		}
		// CreateFormFile 会把文件段固定标成 application/octet-stream，
		// 网关按段头校验 MIME 会直接 400；按实际字节嗅探声明。
		contentType := http.DetectContentType(data)
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(
			`form-data; name="image[]"; filename="image_%d%s"`, i, imageExt(contentType)))
		header.Set("Content-Type", contentType)
		part, err := mw.CreatePart(header)
		if err != nil {
			return Image{}, err
		}
		if _, err := part.Write(data); err != nil {
			return Image{}, err
		}
	}
	_ = mw.WriteField("prompt", req.Prompt)
	_ = mw.WriteField("size", size)
	_ = mw.WriteField("quality", k.cfg.Quality)
	_ = mw.WriteField("n", "1")
	if err := mw.Close(); err != nil {
		return Image{}, err
	}

	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, k.endpoint("edits", apiVersion), &body)
	if err != nil {
		return Image{}, err
	}
	k.setGatewayHeaders(hreq)
	// multipart 的 Content-Type 带 boundary，必须在网关头之后设置（覆盖配置里误留的值）。
	hreq.Header.Set("Content-Type", mw.FormDataContentType())
	return k.do(ctx, hreq, "edits", map[string]any{"size": size, "quality": k.cfg.Quality})
}

// imageExt 按嗅探出的 MIME 给文件段一个匹配的扩展名（部分网关按扩展名双重校验）。
func imageExt(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}

func (k *KSGptImage) readReference(ctx context.Context, uri string) ([]byte, error) {
	if k.cfg.Artifacts == nil {
		return nil, fmt.Errorf("gptimage edits requires managed artifact storage")
	}
	object, ok := k.cfg.Artifacts.Resolve(uri)
	if !ok {
		return nil, fmt.Errorf("untrusted reference URL; only URLs produced by the configured artifact storage are allowed")
	}
	reader, err := k.cfg.Artifacts.Open(ctx, object)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(io.LimitReader(reader, maxImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxImageBytes {
		return nil, fmt.Errorf("reference image exceeds %d bytes", maxImageBytes)
	}
	return data, nil
}

// do 发请求并解析 gpt-image 响应（data[].b64_json 优先，其次 url）。
// params 是本次实际发送的参数，随结果返回给调用方。
func (k *KSGptImage) do(ctx context.Context, hreq *http.Request, op string, params map[string]any) (Image, error) {
	resp, err := k.http.Do(hreq)
	if err != nil {
		return Image{}, fmt.Errorf("imagegen: gptimage %s: %w", op, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes))
	if err != nil {
		return Image{}, fmt.Errorf("imagegen: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Image{}, fmt.Errorf("imagegen: gptimage %s: HTTP %d: %s", op, resp.StatusCode, snippet(raw))
	}
	var b struct {
		Data []struct {
			B64 string `json:"b64_json"`
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return Image{}, fmt.Errorf("imagegen: parse response: %w (%s)", err, snippet(raw))
	}
	if len(b.Data) == 0 {
		return Image{}, fmt.Errorf("imagegen: no image in response: %s", snippet(raw))
	}
	item := b.Data[0]
	switch {
	case item.B64 != "":
		data, err := base64.StdEncoding.DecodeString(item.B64)
		if err != nil {
			return Image{}, fmt.Errorf("imagegen: decode b64_json: %w", err)
		}
		return Image{Data: data, MIME: "image/png", Params: params}, nil
	case item.URL != "":
		data, err := download(ctx, k.http, item.URL)
		if err != nil {
			return Image{}, fmt.Errorf("imagegen: %w", err)
		}
		return Image{Data: data, MIME: "image/png", Params: params}, nil
	default:
		return Image{}, fmt.Errorf("imagegen: no b64_json/url in response: %s", snippet(raw))
	}
}
