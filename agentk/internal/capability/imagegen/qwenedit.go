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

	"agentk/internal/artifact"
)

// qwenedit 实现：阿里 qwen-image-edit 系列（qwen-image-edit-plus 等）经快手 kigress
// 内网网关转发的参考图生图协议。协议体沿用 DashScope 的 multimodal-generation
// 三段式（model + input.messages + parameters），content 数组里放 image 段 + text 段；
// 与公网 dashscope 的差别在鉴权与路由：走 x-api-key + x-ks-* 一组网关头（非 Bearer），
// 且 baseUrl 里已含租户/模型路由前缀，代码只在其后拼固定端点路径。
// 响应从 output.choices[].message.content[].image 取图（阿里 OSS 临时 URL，当场转存）。

// qwenEditSizes 画幅 → qwen-image 支持的 size 枚举（仅在显式给了 aspectRatio 时下发；
// 编辑模型不给 size 时通常按输入图推断，故缺省不强制）。
var qwenEditSizes = map[string]string{
	"1:1":  "1328*1328",
	"9:16": "928*1664",
	"16:9": "1664*928",
}

type QwenEditConfig struct {
	// BaseURL 网关基址，含租户/模型路由前缀，截到 /api/v1（含），必填。
	// 形如 http://kigress-gateway.internal/<租户>/<模型id>/api/v1
	BaseURL string
	// Model 请求体 model 字段（即 profile 条目名），必填
	Model string
	// Headers 逐条按原样下发的网关头（x-api-key / x-ks-user-key / x-ks-llm-model /
	// x-ks-biz-scene 等）；至少含鉴权头，必填。值只来自模型目录，不回显也不写日志。
	Headers map[string]string
	// Artifacts 解析受管产物 URL 及其公网可达性。私有产物读取后转 base64 data URI 内嵌到 content
	//（qwen 后端读不到内网地址）；受管公共产物原样透传；不受管 URL 拒绝以避免网关侧 SSRF。
	Artifacts artifact.Storage
	// ProxyURL 可选出口代理；内网网关通常不需要
	ProxyURL string
	// Client 供测试注入；缺省按 ProxyURL 构造
	Client *http.Client
}

type QwenEdit struct {
	cfg  QwenEditConfig
	http *http.Client
}

func NewQwenEdit(cfg QwenEditConfig) (*QwenEdit, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("imagegen: qwenedit baseUrl is required")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("imagegen: qwenedit model name is required")
	}
	if len(cfg.Headers) == 0 {
		return nil, fmt.Errorf("imagegen: qwenedit headers are required (x-api-key / x-ks-*)")
	}
	baseURL, err := url.Parse(cfg.BaseURL)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" {
		return nil, fmt.Errorf("imagegen: qwenedit baseUrl %q must be an absolute HTTP(S) URL", cfg.BaseURL)
	}
	client := cfg.Client
	if client == nil {
		var err error
		if client, err = newHTTPClient(cfg.ProxyURL); err != nil {
			return nil, err
		}
	}
	return &QwenEdit{cfg: cfg, http: client}, nil
}

// qwenEditMaxImageBytes 参考图拉取上限（base64 后约膨胀 1/3）。
const qwenEditMaxImageBytes = 30 << 20

// inlineImage 将自有私有产物读取并转为 data URI（DashScope multimodal 的 image 段接受
// data:image/...;base64,... 格式）；受管公共产物原样透传，不受管 URL 拒绝。
func (q *QwenEdit) inlineImage(ctx context.Context, image string) (string, error) {
	if q.cfg.Artifacts == nil {
		return image, nil
	}
	object, ok := q.cfg.Artifacts.Resolve(image)
	if !ok {
		return "", fmt.Errorf("imagegen: untrusted reference URL; only URLs produced by the configured artifact storage are allowed")
	}
	if object.Visibility == artifact.Public {
		return image, nil
	}
	r, err := q.cfg.Artifacts.Open(ctx, object)
	if err != nil {
		return "", fmt.Errorf("imagegen: open reference image %s: %w", image, err)
	}
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(io.LimitReader(r, qwenEditMaxImageBytes+1))
	if err != nil {
		return "", fmt.Errorf("imagegen: read reference image %s: %w", image, err)
	}
	if len(data) > qwenEditMaxImageBytes {
		return "", fmt.Errorf("imagegen: reference image %s exceeds %d bytes", image, qwenEditMaxImageBytes)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), nil
}

func (q *QwenEdit) endpoint() string {
	return strings.TrimSuffix(q.cfg.BaseURL, "/") + "/services/aigc/multimodal-generation/generation"
}

func (q *QwenEdit) Edit(ctx context.Context, req EditRequest) (Image, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return Image{}, fmt.Errorf("imagegen: prompt is required")
	}
	if len(req.ReferenceImages) == 0 {
		return Image{}, fmt.Errorf("imagegen: qwenedit requires at least one reference image")
	}
	// content：参考图作为 image 段依次排在 text 段之前。自有 blob 的内网 URL 拉取转
	// base64 data URI 内嵌（qwen 后端读不到内网地址）；受管公网 URL 原样透传。
	content := make([]map[string]any, 0, len(req.ReferenceImages)+1)
	for _, img := range req.ReferenceImages {
		if strings.TrimSpace(img) == "" {
			return Image{}, fmt.Errorf("imagegen: reference image URL must not be empty")
		}
		inlined, err := q.inlineImage(ctx, img)
		if err != nil {
			return Image{}, err
		}
		content = append(content, map[string]any{"image": inlined})
	}
	content = append(content, map[string]any{"text": req.Prompt})

	parameters := map[string]any{
		"n":               1,
		"negative_prompt": " ",
		"prompt_extend":   true,
		"watermark":       false,
	}
	if req.AspectRatio != "" {
		size, err := imageSize(qwenEditSizes, req.AspectRatio, "")
		if err != nil {
			return Image{}, err
		}
		parameters["size"] = size
	}
	payload, err := json.Marshal(map[string]any{
		"model": q.cfg.Model,
		"input": map[string]any{
			"messages": []map[string]any{{
				"role":    "user",
				"content": content,
			}},
		},
		"parameters": parameters,
	})
	if err != nil {
		return Image{}, err
	}

	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, q.endpoint(), bytes.NewReader(payload))
	if err != nil {
		return Image{}, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	for k, v := range q.cfg.Headers {
		hreq.Header.Set(k, v)
	}

	resp, err := q.http.Do(hreq)
	if err != nil {
		return Image{}, fmt.Errorf("imagegen: qwenedit generate: %w", err)
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
	if body.Code != "" || resp.StatusCode != http.StatusOK {
		return Image{}, fmt.Errorf("imagegen: qwenedit generate rejected: HTTP %d code=%q message=%q",
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
	data, err := download(ctx, q.http, imageURL)
	if err != nil {
		return Image{}, fmt.Errorf("imagegen: %w", err)
	}
	// size 缺席时不报：那正是"未下发 size、由模型按输入图定尺寸"的情形。
	params := map[string]any{}
	if size, ok := parameters["size"]; ok {
		params["size"] = size
	}
	return Image{Data: data, MIME: "image/png", Params: params}, nil
}
