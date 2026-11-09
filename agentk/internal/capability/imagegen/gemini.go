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

// gemini 实现：Google Gemini 图像模型（gemini-3-pro-image、gemini-3.1-flash-image 等）
// 经快手 kigress 内网网关转发，鉴权走 x-api-key + x-ks-* 一组网关头。
//
// 与其他生图协议不同：它没有专门的 images 端点，生图和对话共用
// generateContent——图片作为响应里的 inlineData 段回来。因此画幅不换算成像素
// size（azure/dashscope 使用这种参数），而是 generationConfig.imageConfig 中的
// aspectRatio + imageSize 两个独立参数：前者决定比例，后者决定清晰度档。
//
// 文生图与参考图生图是同一个端点的两种入参，因此该实现同时提供 Client 与
// EditClient：Generate 只发一段 text；Edit 在 text 之前先摆参考图的 inlineData
// 段（官方示例 contents=[image, prompt] 即此顺序）。两者的响应处理完全一样。
//
// baseUrl 含租户与项目路由前缀，截到 .../publishers/google（含），
// 代码再拼 /models/{模型id}:generateContent——与 mediaunderstand 的 gemini
// 实现使用同一个基址，只是模型不同。

// DefaultGeminiImageSize 条目省略 imageSize 时的清晰度档。
const DefaultGeminiImageSize = "1K"

// DefaultGeminiMaxReferenceImages 条目省略 maxReferenceImages 时 edit_image 接受的
// 参考图张数上限。上游单请求最多 14 张，这里按 10 收窄：参考图是整张内联进请求的，
// 张数直接乘上传字节与输入 token（每张按 560~1120 input tokens 计费）。
const DefaultGeminiMaxReferenceImages = 10

// geminiMaxReferenceBytes 单张参考图的字节上限，对齐官方文档的内联数据限制：
// inline data 单文件上限为 7 MB，更大文件必须使用 Cloud Storage，本实现不涉及。
const geminiMaxReferenceBytes = 7 << 20

// geminiReferenceMIMEs 参考图可用的 MIME 类型（官方文档列举）。heic/heif 上游也收，
// 但 http.DetectContentType 无法识别；无法探测时直接拒绝，不伪造 MIME 再等待上游失败。
var geminiReferenceMIMEs = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/webp": true,
}

// geminiAspectRatios gemini 支持的画幅取值。这里列全而不是只列工具层当前开放的
// 三档。这样工具 schema 放宽时供应商实现无需同步修改，收窄仍由工具层负责。
var geminiAspectRatios = map[string]bool{
	"1:1": true, "2:3": true, "3:2": true, "3:4": true, "4:3": true,
	"4:5": true, "5:4": true, "9:16": true, "16:9": true, "21:9": true,
}

// geminiImageSizes 清晰度档。大小写敏感——小写的 "2k" 会被上游静默忽略、
// 返回一张 1K 图片，因此在构建期严格校验取值。
var geminiImageSizes = map[string]bool{"512": true, "1K": true, "2K": true, "4K": true}

type GeminiConfig struct {
	// BaseURL 网关基址，截到 .../publishers/google（含），必填
	BaseURL string
	// Model 模型 id，进路径的 /models/{model}:generateContent，必填
	Model string
	// ImageSize 清晰度档 512 / 1K / 2K / 4K，缺省 1K
	ImageSize string
	// Headers 逐条按原样下发的网关头（x-api-key / x-ks-user-key / x-ks-llm-model /
	// x-ks-biz-scene），至少含鉴权头，必填。Content-Type 由代码设置。
	Headers map[string]string
	// ProxyURL 可选出口代理
	ProxyURL string
	// Client 供测试注入；缺省按 ProxyURL 构造
	Client *http.Client
	// Artifacts 受管产物存储，Edit（参考图生图）用它读参考图字节转 inlineData。
	// 只做 Generate 时可为 nil；Edit 缺它即报错——不受管 URL 一律
	// 拒绝，否则模型将获得让服务端读取任意地址的能力。
	Artifacts artifact.Storage
}

type Gemini struct {
	cfg  GeminiConfig
	http *http.Client
}

func NewGemini(cfg GeminiConfig) (*Gemini, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("imagegen: gemini baseUrl is required")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("imagegen: gemini model identifier is required")
	}
	if len(cfg.Headers) == 0 {
		return nil, fmt.Errorf("imagegen: gemini headers are required (x-api-key / x-ks-*)")
	}
	baseURL, err := url.Parse(cfg.BaseURL)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" {
		return nil, fmt.Errorf("imagegen: gemini baseUrl %q must be an absolute HTTP(S) URL", cfg.BaseURL)
	}
	if cfg.ImageSize == "" {
		cfg.ImageSize = DefaultGeminiImageSize
	}
	if !geminiImageSizes[cfg.ImageSize] {
		return nil, fmt.Errorf("imagegen: gemini unsupported imageSize %q (want 512, 1K, 2K or 4K)", cfg.ImageSize)
	}
	client := cfg.Client
	if client == nil {
		if client, err = newHTTPClient(cfg.ProxyURL); err != nil {
			return nil, err
		}
	}
	return &Gemini{cfg: cfg, http: client}, nil
}

func (g *Gemini) endpoint() string {
	return strings.TrimSuffix(g.cfg.BaseURL, "/") + "/models/" + g.cfg.Model + ":generateContent"
}

func (g *Gemini) Generate(ctx context.Context, req Request) (Image, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return Image{}, fmt.Errorf("imagegen: prompt is required")
	}
	aspectRatio := req.AspectRatio
	if aspectRatio == "" {
		aspectRatio = "1:1"
	}
	if !geminiAspectRatios[aspectRatio] {
		return Image{}, fmt.Errorf("imagegen: gemini unsupported aspect ratio %q", aspectRatio)
	}
	return g.generateContent(ctx, []any{map[string]any{"text": req.Prompt}}, map[string]any{
		"aspectRatio": aspectRatio,
		"imageSize":   g.cfg.ImageSize,
	})
}

// Edit 参考图生图：参考图作为 inlineData 段摆在指令之前。
// 画幅留空时不下发 aspectRatio——编辑场景下由模型按输入图推断更合理，
// 强制使用 1:1 会裁剪非方形参考图。
func (g *Gemini) Edit(ctx context.Context, req EditRequest) (Image, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return Image{}, fmt.Errorf("imagegen: prompt is required")
	}
	if len(req.ReferenceImages) == 0 {
		return Image{}, fmt.Errorf("imagegen: gemini edit requires at least one reference image")
	}
	if req.AspectRatio != "" && !geminiAspectRatios[req.AspectRatio] {
		return Image{}, fmt.Errorf("imagegen: gemini unsupported aspect ratio %q", req.AspectRatio)
	}
	parts := make([]any, 0, len(req.ReferenceImages)+1)
	for _, ref := range req.ReferenceImages {
		part, err := g.inlineReference(ctx, ref)
		if err != nil {
			return Image{}, err
		}
		parts = append(parts, part)
	}
	parts = append(parts, map[string]any{"text": req.Prompt})

	imageConfig := map[string]any{"imageSize": g.cfg.ImageSize}
	if req.AspectRatio != "" {
		imageConfig["aspectRatio"] = req.AspectRatio
	}
	return g.generateContent(ctx, parts, imageConfig)
}

// inlineReference 把一张受管参考图读成 inlineData 段。
// 不受管 URL 一律拒绝：这一步是服务端代取字节，放开就等于把"取任意地址"的
// 能力交给模型（同 qwenedit / gptimage 的处置）。受管公共产物也照样读字节内联——
// 上游是从公网回来抓 URL 的，我们的产物多在内网/staging 域名上，它解析不了。
func (g *Gemini) inlineReference(ctx context.Context, ref string) (map[string]any, error) {
	if strings.TrimSpace(ref) == "" {
		return nil, fmt.Errorf("imagegen: gemini reference image URL must not be empty")
	}
	if g.cfg.Artifacts == nil {
		return nil, fmt.Errorf("imagegen: gemini edit requires artifact storage")
	}
	object, ok := g.cfg.Artifacts.Resolve(ref)
	if !ok {
		return nil, fmt.Errorf("imagegen: untrusted reference URL; only URLs produced by the configured artifact storage are allowed")
	}
	r, err := g.cfg.Artifacts.Open(ctx, object)
	if err != nil {
		return nil, fmt.Errorf("imagegen: open reference image %s: %w", ref, err)
	}
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(io.LimitReader(r, geminiMaxReferenceBytes+1))
	if err != nil {
		return nil, fmt.Errorf("imagegen: read reference image %s: %w", ref, err)
	}
	if len(data) > geminiMaxReferenceBytes {
		return nil, fmt.Errorf("imagegen: reference image %s exceeds %d bytes", ref, geminiMaxReferenceBytes)
	}
	// MIME 按字节探测而不是按扩展名：产物 key 上的后缀是生成时写的，不保证与
	// 真实字节一致，而 inlineData 的 mimeType 报错了上游只会回一个含糊的 400。
	mimeType := http.DetectContentType(data)
	if !geminiReferenceMIMEs[mimeType] {
		return nil, fmt.Errorf("imagegen: reference image %s has unsupported type %q (want png, jpeg or webp)",
			ref, mimeType)
	}
	return map[string]any{"inlineData": map[string]any{
		"mimeType": mimeType,
		"data":     base64.StdEncoding.EncodeToString(data),
	}}, nil
}

// generateContent 发一次 generateContent 并把响应里的第一段图片取回。
// 文生图与参考图生图只在 parts 与 imageConfig 上不同，其余完全共用。
func (g *Gemini) generateContent(ctx context.Context, parts []any, imageConfig map[string]any) (Image, error) {
	payload, err := json.Marshal(map[string]any{
		"contents": []any{map[string]any{"role": "user", "parts": parts}},
		"generationConfig": map[string]any{
			// TEXT 必须和 IMAGE 一起给：只声明 IMAGE 时上游回 200 但 parts 为空，
			// 是一种不报错的失败。
			"responseModalities": []string{"TEXT", "IMAGE"},
			"imageConfig":        imageConfig,
		},
	})
	if err != nil {
		return Image{}, err
	}
	img, err := g.call(ctx, payload)
	if err != nil {
		return Image{}, err
	}
	// 回报实际发送的参数；未下发画幅时报 auto，表示"由模型按输入图决定"。
	ratio, ok := imageConfig["aspectRatio"]
	if !ok {
		ratio = "auto"
	}
	img.Params = map[string]any{"aspectRatio": ratio, "imageSize": imageConfig["imageSize"]}
	return img, nil
}

func (g *Gemini) call(ctx context.Context, payload []byte) (Image, error) {
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.endpoint(), bytes.NewReader(payload))
	if err != nil {
		return Image{}, err
	}
	for hk, hv := range g.cfg.Headers {
		hreq.Header.Set(hk, hv)
	}
	hreq.Header.Set("Content-Type", "application/json")

	resp, err := g.http.Do(hreq)
	if err != nil {
		return Image{}, fmt.Errorf("imagegen: gemini generateContent %s: %w", g.endpoint(), err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes))
	if err != nil {
		return Image{}, fmt.Errorf("imagegen: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// 带上 endpoint：网关路由不存在时响应体是空的，只报状态码等于什么都没说
		return Image{}, fmt.Errorf("imagegen: gemini generateContent %s: HTTP %d: %s",
			g.endpoint(), resp.StatusCode, snippet(raw))
	}

	var body geminiImageResponse
	if err := json.Unmarshal(raw, &body); err != nil {
		return Image{}, fmt.Errorf("imagegen: parse response: %w (%s)", err, snippet(raw))
	}
	if body.Error.Message != "" {
		return Image{}, fmt.Errorf("imagegen: gemini: %s", body.Error.Message)
	}
	inline, ok := body.image()
	if !ok {
		// 没有图片段时把终止原因带出去：这一类失败几乎都是内容审核或长度上限，
		// 原样重试无用，模型得知道该换措辞。
		return Image{}, fmt.Errorf("imagegen: gemini returned no image (finishReason=%q blockReason=%q): %s",
			body.finishReason(), body.PromptFeedback.BlockReason, snippet(raw))
	}
	data, err := base64.StdEncoding.DecodeString(inline.Data)
	if err != nil {
		return Image{}, fmt.Errorf("imagegen: decode inlineData: %w", err)
	}
	mimeType := inline.MIMEType
	if mimeType == "" {
		mimeType = "image/png"
	}
	// Params 由调用方按实际发送的参数补齐；这一层只处理图片字节。
	return Image{Data: data, MIME: mimeType}, nil
}

type geminiInlineData struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}

type geminiImageResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				InlineData *geminiInlineData `json:"inlineData"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	PromptFeedback struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// image 取首个候选里的第一段图片。响应是图文交错的（模型常在图前后各说一句），
// 生图工具只要那张图，文字段直接跳过。
func (r geminiImageResponse) image() (geminiInlineData, bool) {
	if len(r.Candidates) == 0 {
		return geminiInlineData{}, false
	}
	for _, part := range r.Candidates[0].Content.Parts {
		if part.InlineData != nil && part.InlineData.Data != "" {
			return *part.InlineData, true
		}
	}
	return geminiInlineData{}, false
}

func (r geminiImageResponse) finishReason() string {
	if len(r.Candidates) == 0 {
		return ""
	}
	return r.Candidates[0].FinishReason
}
