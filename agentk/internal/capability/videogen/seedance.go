package videogen

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
	"agentk/internal/tool"
)

// 火山方舟 Seedance 视频生成端点（对齐官方「Doubao Seedance 2.0」文档）：
// 提交 POST /contents/generations/tasks，轮询 GET /contents/generations/tasks/{id}。
// 一个创建接口覆盖文生视频、首帧/首尾帧图生视频与多模态参考（图/视频/音频），
// 用法差异全在 content 数组的 role 上，故对应一个工具、一份参数表。

// SeedanceConfig 配置火山方舟 Contents Generations API（包括 Kigress 网关形式）。
// BaseURL 截到 /api/v3；Headers 携带 x-api-key / x-ks-* 或其它网关鉴权头。
type SeedanceConfig struct {
	BaseURL string
	Model   string
	Headers map[string]string
	// ProxyURL 可选公网出口代理（IDC 环境访问公网 API 用）
	ProxyURL string
	Client   *http.Client
	// Artifacts 用于识别工具链产物 URL。受管私有图片读取后转 base64 data URI，
	// 受管公共产物和外部 HTTP(S) URL 原样透传。
	Artifacts artifact.Storage
}

// Seedance 实现方舟视频生成任务的提交与轮询协议。
type Seedance struct {
	cfg  SeedanceConfig
	http *http.Client
}

const (
	seedanceDefaultResolution = "1080p"
	seedanceDefaultRatio      = "adaptive"
	seedanceDefaultDuration   = 5
	seedanceMinDuration       = 4
	seedanceMaxDuration       = 15
	seedanceMaxResponseBytes  = 4 << 20
	seedanceMaxImageBytes     = 30 << 20
	seedanceMaxRequestBytes   = 64 << 20
)

// 多模态素材数量上限（官方文档：图片 0~9、视频 0~3、音频 0~3）。
const (
	seedanceMaxReferenceImages = 9
	seedanceMaxReferenceVideos = 3
	seedanceMaxReferenceAudios = 3
)

func NewSeedance(cfg SeedanceConfig) (*Seedance, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("videogen: seedance baseUrl is required")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("videogen: seedance model identifier is required")
	}
	if len(cfg.Headers) == 0 {
		return nil, fmt.Errorf("videogen: seedance headers are required")
	}
	baseURL, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("videogen: parse seedance base url %q: %w", cfg.BaseURL, err)
	}
	if (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" {
		return nil, fmt.Errorf("videogen: seedance base url %q must be an absolute HTTP(S) URL", cfg.BaseURL)
	}
	client := cfg.Client
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if cfg.ProxyURL != "" {
			proxyURL, err := url.Parse(cfg.ProxyURL)
			if err != nil {
				return nil, fmt.Errorf("videogen: parse proxy url %q: %w", cfg.ProxyURL, err)
			}
			if proxyURL.Scheme == "" || proxyURL.Host == "" {
				return nil, fmt.Errorf("videogen: proxy url %q must be absolute", cfg.ProxyURL)
			}
			transport.Proxy = http.ProxyURL(proxyURL)
		}
		client = &http.Client{Transport: transport}
	}
	return &Seedance{cfg: cfg, http: client}, nil
}

// SeedanceRequest 是方舟 Seedance 创建任务接口的入参。素材各按角色分字段给 URL，
// 客户端装进 content 数组；prompt 里按「图片1 / 视频1 / 音频1」引用同类素材的次序。
type SeedanceRequest struct {
	// Prompt 文本提示词，必填；无任何素材时即为文生视频
	Prompt string `json:"prompt"`
	// FirstFrame 首帧图，可选
	FirstFrame string `json:"firstFrame,omitempty"`
	// LastFrame 尾帧图，可选；必须与首帧同时给
	LastFrame string `json:"lastFrame,omitempty"`
	// ReferenceImages 参考图，最多 9 张，依次对应 prompt 里的「图片1..图片9」
	ReferenceImages []string `json:"referenceImages,omitempty"`
	// ReferenceVideos 参考视频，最多 3 个，依次对应「视频1..视频3」
	ReferenceVideos []string `json:"referenceVideos,omitempty"`
	// ReferenceAudios 参考音频，最多 3 个，依次对应「音频1..音频3」
	ReferenceAudios []string `json:"referenceAudios,omitempty"`
	// Resolution 480p / 720p / 1080p / 4k；空取 1080p（各档位由所选模型决定是否支持）
	Resolution string `json:"resolution,omitempty"`
	// Duration 秒数 "4"~"15"；空取 "5"
	Duration string `json:"duration,omitempty"`
	// Ratio 画面比例 21:9 / 16:9 / 4:3 / 1:1 / 3:4 / 9:16 / adaptive；空取 adaptive
	Ratio string `json:"ratio,omitempty"`
	// GenerateAudio 是否生成与画面适配的声音；空取 false
	GenerateAudio *bool `json:"generateAudio,omitempty"`
	// Watermark 是否在成片上加水印；空取 false
	Watermark *bool `json:"watermark,omitempty"`
}

// Normalize 校验入参并落实缺省，幂等。
func (r SeedanceRequest) Normalize() (SeedanceRequest, error) {
	if strings.TrimSpace(r.Prompt) == "" {
		return r, fmt.Errorf("videogen: prompt is required")
	}
	r.FirstFrame = strings.TrimSpace(r.FirstFrame)
	r.LastFrame = strings.TrimSpace(r.LastFrame)
	if r.LastFrame != "" && r.FirstFrame == "" {
		return r, fmt.Errorf("videogen: seedance supports first frame or first+last frame, not last frame alone")
	}
	var err error
	if r.ReferenceImages, err = trimmedList("seedance", "referenceImages",
		r.ReferenceImages, seedanceMaxReferenceImages); err != nil {
		return r, err
	}
	if r.ReferenceVideos, err = trimmedList("seedance", "referenceVideos",
		r.ReferenceVideos, seedanceMaxReferenceVideos); err != nil {
		return r, err
	}
	if r.ReferenceAudios, err = trimmedList("seedance", "referenceAudios",
		r.ReferenceAudios, seedanceMaxReferenceAudios); err != nil {
		return r, err
	}
	// 文档明示不支持"纯音频"与"文本 + 音频"输入：音频只能给已有画面配声。
	if len(r.ReferenceAudios) > 0 &&
		r.FirstFrame == "" && len(r.ReferenceImages) == 0 && len(r.ReferenceVideos) == 0 {
		return r, fmt.Errorf("videogen: seedance reference audio requires an image or video input")
	}
	seconds, err := parseDuration("seedance", r.Duration, seedanceMinDuration, seedanceMaxDuration, seedanceDefaultDuration)
	if err != nil {
		return r, err
	}
	r.Duration = strconv.Itoa(seconds)
	if r.Resolution, err = pickEnum("seedance", "resolution", r.Resolution,
		seedanceDefaultResolution, "480p", "720p", "1080p", "4k"); err != nil {
		return r, err
	}
	if r.Ratio, err = pickEnum("seedance", "ratio", r.Ratio, seedanceDefaultRatio,
		"21:9", "16:9", "4:3", "1:1", "3:4", "9:16", "adaptive"); err != nil {
		return r, err
	}
	if r.GenerateAudio == nil {
		generateAudio := false
		r.GenerateAudio = &generateAudio
	}
	if r.Watermark == nil {
		watermark := false
		r.Watermark = &watermark
	}
	return r, nil
}

// Params 归一后的实际生效参数。
func (r SeedanceRequest) Params() map[string]any {
	return map[string]any{
		"resolution":    r.Resolution,
		"duration":      r.Duration,
		"ratio":         r.Ratio,
		"generateAudio": r.GenerateAudio != nil && *r.GenerateAudio,
		"watermark":     r.Watermark != nil && *r.Watermark,
	}
}

func (s *Seedance) Submit(ctx context.Context, req SeedanceRequest) (string, error) {
	req, err := req.Normalize()
	if err != nil {
		return "", err
	}
	content := []map[string]any{{"type": "text", "text": req.Prompt}}
	appendImage := func(role, image string) error {
		prepared, err := s.prepareImage(ctx, image)
		if err != nil {
			return fmt.Errorf("videogen: seedance %s: %w", role, err)
		}
		content = append(content, map[string]any{
			"type": "image_url", "image_url": map[string]string{"url": prepared}, "role": role,
		})
		return nil
	}
	if req.FirstFrame != "" {
		if err := appendImage("first_frame", req.FirstFrame); err != nil {
			return "", err
		}
	}
	if req.LastFrame != "" {
		if err := appendImage("last_frame", req.LastFrame); err != nil {
			return "", err
		}
	}
	for _, image := range req.ReferenceImages {
		if err := appendImage("reference_image", image); err != nil {
			return "", err
		}
	}
	// 视频与音频素材只能以链接形式提供（上游不收内联字节）。
	for _, media := range req.ReferenceVideos {
		prepared, err := s.prepareRemoteMedia("reference_video", media)
		if err != nil {
			return "", err
		}
		content = append(content, map[string]any{
			"type": "video_url", "video_url": map[string]string{"url": prepared}, "role": "reference_video",
		})
	}
	for _, media := range req.ReferenceAudios {
		prepared, err := s.prepareRemoteMedia("reference_audio", media)
		if err != nil {
			return "", err
		}
		content = append(content, map[string]any{
			"type": "audio_url", "audio_url": map[string]string{"url": prepared}, "role": "reference_audio",
		})
	}

	duration, _ := strconv.Atoi(req.Duration)
	payload, err := json.Marshal(map[string]any{
		"model":          s.cfg.Model,
		"content":        content,
		"generate_audio": *req.GenerateAudio,
		"ratio":          req.Ratio,
		"resolution":     req.Resolution,
		"duration":       duration,
		"watermark":      *req.Watermark,
	})
	if err != nil {
		return "", err
	}
	if len(payload) > seedanceMaxRequestBytes {
		return "", fmt.Errorf("videogen: seedance request exceeds %d bytes", seedanceMaxRequestBytes)
	}

	var response seedanceTaskResponse
	if err := s.call(ctx, http.MethodPost, s.tasksEndpoint(), payload, &response); err != nil {
		return "", fmt.Errorf("videogen: create seedance task: %w", err)
	}
	if response.ID == "" {
		return "", fmt.Errorf("videogen: create seedance task returned empty task id")
	}
	return response.ID, nil
}

// prepareImage 受管私有产物读取后转 base64 data URI；受管公共产物与外部 URL 原样透传。
func (s *Seedance) prepareImage(ctx context.Context, uri string) (string, error) {
	if s.cfg.Artifacts != nil {
		if object, ok := s.cfg.Artifacts.Resolve(uri); ok {
			if object.Visibility == artifact.Public {
				return object.URL, nil
			}
			reader, err := s.cfg.Artifacts.Open(ctx, object)
			if err != nil {
				return "", fmt.Errorf("open image %s: %w", uri, err)
			}
			defer func() { _ = reader.Close() }()
			data, err := io.ReadAll(io.LimitReader(reader, seedanceMaxImageBytes+1))
			if err != nil {
				return "", fmt.Errorf("read image %s: %w", uri, err)
			}
			if len(data) > seedanceMaxImageBytes {
				return "", fmt.Errorf("image %s exceeds %d bytes", uri, seedanceMaxImageBytes)
			}
			return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), nil
		}
	}
	parsed, err := url.Parse(uri)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("image URL %q must be an absolute HTTP(S) URL", uri)
	}
	return uri, nil
}

// prepareRemoteMedia 视频/音频素材必须是上游可直接取用的链接：受管公共产物取其
// 公网 URL，受管私有产物没有这条路（整段视频内联不现实），直接报错说清楚。
func (s *Seedance) prepareRemoteMedia(role, uri string) (string, error) {
	if s.cfg.Artifacts != nil {
		if object, ok := s.cfg.Artifacts.Resolve(uri); ok {
			if object.Visibility != artifact.Public {
				return "", fmt.Errorf(
					"videogen: seedance %s %q is not publicly reachable; provide a public URL", role, uri)
			}
			return object.URL, nil
		}
	}
	parsed, err := url.Parse(uri)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("videogen: seedance %s URL %q must be an absolute HTTP(S) URL", role, uri)
	}
	return uri, nil
}

func (s *Seedance) Poll(ctx context.Context, remoteTaskID string) (tool.PollResult, error) {
	if strings.TrimSpace(remoteTaskID) == "" {
		return tool.PollResult{}, fmt.Errorf("videogen: remote task id is required")
	}
	var response seedanceTaskResponse
	if err := s.call(ctx, http.MethodGet, s.taskEndpoint(remoteTaskID), nil, &response); err != nil {
		return tool.PollResult{}, fmt.Errorf("videogen: poll seedance task %s: %w", remoteTaskID, err)
	}
	switch response.Status {
	case "succeeded":
		if response.Content.VideoURL == "" {
			return tool.PollResult{Failed: true, FailureReason: "任务成功但未返回视频链接"}, nil
		}
		result, err := json.Marshal(map[string]any{
			"artifacts": []tool.ResultArtifact{{Type: "video", URI: response.Content.VideoURL}},
		})
		if err != nil {
			return tool.PollResult{}, err
		}
		return tool.PollResult{Done: true, Result: result}, nil
	case "failed", "expired", "cancelled", "canceled":
		reason := response.Error.Message
		if reason == "" {
			reason = response.Status
		}
		return tool.PollResult{Failed: true, FailureReason: reason}, nil
	default:
		return tool.PollResult{StatusDetail: response.Status}, nil
	}
}

func (s *Seedance) tasksEndpoint() string {
	return strings.TrimSuffix(s.cfg.BaseURL, "/") + "/contents/generations/tasks"
}

func (s *Seedance) taskEndpoint(taskID string) string {
	return s.tasksEndpoint() + "/" + url.PathEscape(taskID)
}

func (s *Seedance) call(ctx context.Context, method, endpoint string, body []byte, out any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	for key, value := range s.cfg.Headers {
		request.Header.Set(key, value)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := s.http.Do(request)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, seedanceMaxResponseBytes))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		failure := struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}{}
		providerErr := &providerError{
			status:  response.StatusCode,
			message: truncate(raw, 300),
		}
		if err := json.Unmarshal(raw, &failure); err == nil {
			providerErr.code = failure.Error.Code
			if failure.Error.Message != "" {
				providerErr.message = failure.Error.Message
			}
		}
		return providerErr
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

type seedanceTaskResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Status  string `json:"status"`
	Content struct {
		VideoURL string `json:"video_url"`
	} `json:"content"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Basic 返回本端点的公共最小接口（见 KlingI2V.Basic）。
func (s *Seedance) Basic() Client { return seedanceBasic{s} }

// seedanceBasic 把 Seedance 端点适配成公共最小接口。
type seedanceBasic struct{ *Seedance }

func (c seedanceBasic) Submit(ctx context.Context, req Request) (string, error) {
	generateAudio := req.Audio == "native"
	return c.Seedance.Submit(ctx, SeedanceRequest{
		Prompt:        req.Prompt,
		FirstFrame:    req.FirstFrame,
		LastFrame:     req.LastFrame,
		Resolution:    req.Resolution,
		Duration:      req.Duration,
		GenerateAudio: &generateAudio,
	})
}
