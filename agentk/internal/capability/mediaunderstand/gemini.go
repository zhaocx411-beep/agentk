package mediaunderstand

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"agentk/internal/artifact"
)

// Gemini provider 使用 Google Gemini 的 generateContent 协议（Vertex 形式），
// 经快手 kigress 内网网关转发，鉴权走 x-api-key + x-ks-* 一组网关头。
//
// 媒体有两种发送方式，由能力层的 fetchMedia 逐段决定（判定条件见该函数）：
// 公网可达的走 parts[].fileData（只给 URL 与 MIME，Gemini 自己去抓），
// 其余走 parts[].inlineData（base64 字节）。
//
// baseUrl 含租户与项目路由前缀，截到 .../publishers/google（含），
// 代码再拼 /models/{模型id}:generateContent。

// geminiMaxResponseBytes 响应体读取上限。回来的是一段文字，几 MB 已经绰绰有余，
// 这一层只防网关异常时把一个巨大的错误页读进内存。
const geminiMaxResponseBytes = 8 << 20

// GeminiConfig 配置一个 Gemini 理解端点。
type GeminiConfig struct {
	// BaseURL 网关基址，截到 .../publishers/google（含），必填
	BaseURL string
	// Model 模型 id，进路径的 /models/{model}:generateContent，必填
	Model string
	// Headers 逐条按原样下发的网关头（x-api-key / x-ks-user-key / x-ks-llm-model /
	// x-ks-biz-scene），至少含鉴权头，必填。Content-Type 由代码设置。
	Headers map[string]string
	// ProxyURL 可选出口代理
	ProxyURL string
	// Client 供测试注入；缺省按 ProxyURL 构造
	Client *http.Client
	// Artifacts 受管产物存储，用于直接读取本地/内网不可达的产物字节；
	// 可为 nil——那时全部媒体按 http(s) 下载。
	Artifacts artifact.Storage
	// RemoteFetch 允许把公网可达的素材 URL 直接交给 Gemini 自取（fileData），
	// 省掉一次下载与 base64 往返。缺省关闭：Gemini 是从公网回来抓这个 URL 的，
	// 内网对象存储与 staging 域名它解析不了（回 URL_ERROR-ERROR_DNS）。
	// 产物 URL 确实公网可达（配了公网 CDN）时才打开。
	// 打开后若上游仍取不到，会自动改内联重试一次，不会把错误直接甩给模型。
	RemoteFetch bool
}

// Gemini 实现 generateContent 协议的媒体理解调用。
type Gemini struct {
	cfg  GeminiConfig
	http *http.Client
}

func NewGemini(cfg GeminiConfig) (*Gemini, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("mediaunderstand: gemini baseUrl is required")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("mediaunderstand: gemini model identifier is required")
	}
	if len(cfg.Headers) == 0 {
		return nil, fmt.Errorf("mediaunderstand: gemini headers are required (x-api-key / x-ks-*)")
	}
	baseURL, err := url.Parse(cfg.BaseURL)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" {
		return nil, fmt.Errorf("mediaunderstand: gemini baseUrl %q must be an absolute HTTP(S) URL", cfg.BaseURL)
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

// Understand 决定素材发送方式后调用一次 generateContent，返回模型的文字回答。
//
// 交给上游自取的素材万一它取不到（内网地址、robots.txt 拦截），会自动改内联重来
// 一次：这类失败对模型没有可操作性——URL 是它从上下文里拿的，重试或换措辞都不解决，
// 而内联这条路本来就走得通，没有理由把它变成一次用户可见的失败。
func (g *Gemini) Understand(ctx context.Context, req Request) (Result, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return Result{}, fmt.Errorf("mediaunderstand: prompt is required")
	}
	var media []Media
	var err error
	if len(req.PreparedMedia) > 0 {
		if len(req.MediaURLs) > 0 {
			return Result{}, fmt.Errorf("mediaunderstand: mediaUrls and preparedMedia are mutually exclusive")
		}
		if media, err = validatePreparedMedia(req.PreparedMedia); err != nil {
			return Result{}, fmt.Errorf("mediaunderstand: %w", err)
		}
	} else {
		media, err = fetchMedia(ctx, g.http, g.cfg.Artifacts, req.MediaURLs, g.cfg.RemoteFetch)
		if err != nil {
			return Result{}, fmt.Errorf("mediaunderstand: %w", err)
		}
	}
	if req.VideoSegment != nil && (len(media) != 1 || media[0].Kind != "video") {
		return Result{}, fmt.Errorf("mediaunderstand: video segment requires exactly one video")
	}
	result, err := g.generate(ctx, media, req.Prompt, req.VideoSegment)
	if err == nil || !hasRemote(media) || !isRemoteFetchFailure(err) {
		return result, err
	}
	inlined, ferr := inlineRemote(ctx, g.http, g.cfg.Artifacts, media)
	if ferr != nil {
		// 两个原因都要报：起因是上游取不到那个 URL，但真正卡住的是内联这条退路也
		// 走不通（多半是加起来超了字节预算）。只报前者会把人引去查 URL 可达性，
		// 而那边根本没有问题可查。
		return Result{}, fmt.Errorf("%w; inline fallback also failed: %v", err, ferr)
	}
	return g.generate(ctx, inlined, req.Prompt, req.VideoSegment)
}

func validatePreparedMedia(items []Media) ([]Media, error) {
	if len(items) == 0 || len(items) > maxMediaItems {
		return nil, fmt.Errorf("prepared media must contain between 1 and %d items", maxMediaItems)
	}
	total := 0
	out := make([]Media, len(items))
	for i, item := range items {
		if item.Remote || len(item.Data) == 0 {
			return nil, fmt.Errorf("prepared media %d must contain inline data", i)
		}
		if !supported(item.MIME) && item.MIME != "application/pdf" {
			return nil, fmt.Errorf("prepared media %d has unsupported MIME %q", i, item.MIME)
		}
		if item.MIME == "application/pdf" {
			item.Kind = "document"
		} else {
			item.Kind = mediaKind(item.MIME)
		}
		item.Bytes = len(item.Data)
		total += len(item.Data)
		if total > maxTotalBytes {
			return nil, fmt.Errorf("prepared media exceeds the %d-byte inline budget", maxTotalBytes)
		}
		out[i] = item
	}
	return out, nil
}

// hasRemote 本次是否真有素材是交给上游自取的（决定回落重试是否有意义）。
func hasRemote(media []Media) bool {
	for _, m := range media {
		if m.Remote {
			return true
		}
	}
	return false
}

// isRemoteFetchFailure 判断这次失败是不是"上游取不到我们给的 URL"。
// 按错误文案匹配较脆弱，但误判最多增加一次内联请求；漏判则维持原行为并返回错误，
// 两种结果都不会改变成功请求的语义。
func isRemoteFetchFailure(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "Cannot fetch content from the provided URL") ||
		strings.Contains(msg, "URL_ERROR")
}

func (g *Gemini) generate(
	ctx context.Context, media []Media, prompt string, segment *VideoSegment,
) (Result, error) {
	// 媒体在前、问题在后：Gemini 文档对视频理解的推荐顺序，
	// 问题跟在素材后面时模型更少答非所问。视频与图片各自带 MIME——协议层不区分
	// 两者，混着传即可；交上游自取的走 fileData，其余内联。
	parts := make([]any, 0, len(media)+1)
	for _, m := range media {
		part := make(map[string]any, 2)
		if m.Remote {
			part["fileData"] = map[string]any{
				"mimeType": m.MIME,
				"fileUri":  m.URL,
			}
		} else {
			part["inlineData"] = map[string]any{
				"mimeType": m.MIME,
				"data":     base64.StdEncoding.EncodeToString(m.Data),
			}
		}
		if segment != nil {
			part["videoMetadata"] = geminiVideoMetadata(*segment)
		}
		parts = append(parts, part)
	}
	parts = append(parts, map[string]any{"text": prompt})
	payload, err := json.Marshal(map[string]any{
		"contents": []any{map[string]any{"role": "user", "parts": parts}},
	})
	if err != nil {
		return Result{}, err
	}

	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.endpoint(), bytes.NewReader(payload))
	if err != nil {
		return Result{}, err
	}
	for hk, hv := range g.cfg.Headers {
		hreq.Header.Set(hk, hv)
	}
	hreq.Header.Set("Content-Type", "application/json")

	resp, err := g.http.Do(hreq)
	if err != nil {
		return Result{}, fmt.Errorf("mediaunderstand: gemini generateContent %s: %w", g.endpoint(), err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, geminiMaxResponseBytes))
	if err != nil {
		return Result{}, fmt.Errorf("mediaunderstand: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// 带上 endpoint：网关路由不存在时响应体是空的，只报状态码等于什么都没说
		return Result{}, fmt.Errorf("mediaunderstand: gemini generateContent %s: HTTP %d: %s",
			g.endpoint(), resp.StatusCode, snippet(raw))
	}

	var body geminiResponse
	if err := json.Unmarshal(raw, &body); err != nil {
		return Result{}, fmt.Errorf("mediaunderstand: parse response: %w (%s)", err, snippet(raw))
	}
	if body.Error.Message != "" {
		return Result{}, fmt.Errorf("mediaunderstand: gemini: %s", body.Error.Message)
	}
	text := body.text()
	if text == "" {
		// 空回答基本都是被安全策略或长度上限截掉的，而这两种都不是"重试一次就好"：
		// 把 finishReason / blockReason 原样带出去，模型才知道该换措辞还是换素材。
		return Result{}, fmt.Errorf("mediaunderstand: gemini returned no text (finishReason=%q blockReason=%q): %s",
			body.finishReason(), body.PromptFeedback.BlockReason, snippet(raw))
	}
	params := map[string]any{"media": describe(media)}
	if segment != nil {
		params["videoSegment"] = segmentParams(*segment)
	}
	return Result{Text: text, Params: params}, nil
}

func geminiVideoMetadata(segment VideoSegment) map[string]any {
	metadata := map[string]any{
		"startOffset": geminiDuration(segment.StartSeconds),
		"endOffset":   geminiDuration(segment.EndSeconds),
	}
	if segment.FPS > 0 {
		metadata["fps"] = segment.FPS
	}
	return metadata
}

func geminiDuration(seconds float64) string {
	return strconv.FormatFloat(seconds, 'f', -1, 64) + "s"
}

func segmentParams(segment VideoSegment) map[string]any {
	params := map[string]any{
		"startSeconds": segment.StartSeconds,
		"endSeconds":   segment.EndSeconds,
	}
	if segment.FPS > 0 {
		params["fps"] = segment.FPS
	}
	return params
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
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

// text 拼接首个候选的全部文本段。分段是模型侧的输出细节（思考块、分段落），
// 对调用方没有意义，拼成一段即可。
func (r geminiResponse) text() string {
	if len(r.Candidates) == 0 {
		return ""
	}
	var b strings.Builder
	for _, part := range r.Candidates[0].Content.Parts {
		b.WriteString(part.Text)
	}
	return strings.TrimSpace(b.String())
}

func (r geminiResponse) finishReason() string {
	if len(r.Candidates) == 0 {
		return ""
	}
	return r.Candidates[0].FinishReason
}
