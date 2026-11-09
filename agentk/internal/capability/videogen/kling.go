package videogen

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"agentk/internal/artifact"
	"agentk/internal/tool"
)

// 可灵开放平台（可灵 3.0 / 3.0 Omni）的接入层：三个生成端点共用同一套基址、
// 鉴权与任务查询，只有提交路径与请求体不同——
//   - 图生视频 POST /image-to-video/{model}（本文件 KlingI2V）
//   - Omni     POST /omni-video/{model}（klingomni.go 的 KlingOmni）
//   - 文生视频 POST /text-to-video/{model}（klingt2v.go 的 KlingT2V）
//
// 前两个走 contents（素材）+ settings（输出配置）的请求体；文生视频不收素材，
// prompt 直接在顶层。三者轮询同一个 GET /tasks?task_ids={id}。
// 认证二选一：新版控制台静态 API Key 直接作 Bearer；
// 或老 AK/SK 现签 JWT（HS256, exp=now+30min, nbf=now-5s）放 Authorization。

// DefaultKlingBaseURL 官网端点；profile 条目省略 baseUrl 时用它。
const DefaultKlingBaseURL = "https://api-beijing.klingai.com"

// 可灵各端点共用的缺省清晰度 / 时长 / 声音。声音由产品层默认开启；
// Omni 的参考视频模式会按上游能力改为保留原声或关闭。
// 画幅缺省只有文生视频用得上——另两个端点有素材可跟随。
const (
	klingDefaultResolution  = "1080p"
	klingDefaultDuration    = 5
	klingDefaultAudio       = "native"
	klingDefaultAspectRatio = "16:9"
)

// 可灵文档给出的时长区间（三个端点一致：3~15 秒）。
const (
	klingMinDuration = 3
	klingMaxDuration = 15
)

// 响应体读取上限（状态查询响应远小于此）
const klingMaxResponseBytes = 4 << 20

// klingMaxImageBytes 图片素材拉取上限（base64 后约膨胀 1/3，可灵收得下）。
const klingMaxImageBytes = 30 << 20

// klingMaxVideoBytes 可灵成片落 artifact storage 时的单文件上限。
// 生成片段最长 15 秒，正常远低于此值；上限只防异常响应拖垮进程内存。
const klingMaxVideoBytes = 512 << 20

// 可灵 1102 表示请求被账户余额或风控策略永久拒绝，原请求重试不会恢复。
const klingPermanentRejectionCode = 1102

// KlingConfig 是可灵三个端点共用的接入配置：差异只在 Model 指向哪个模型标识
// （图生视频 / 文生视频 kling-3.0、Omni kling-3.0-omni），
// 由各自的构造函数落到提交路径上。
type KlingConfig struct {
	// BaseURL 空用 DefaultKlingBaseURL（官网）
	BaseURL string
	// APIKey 新版可灵控制台创建的静态 API Key（api-key-kling-...）：直接作 Bearer 下发，
	// 不签 JWT。给了它就优先用；为空才回落 AccessKey/SecretKey 签 JWT（老鉴权）。
	APIKey string
	// AccessKey / SecretKey 老 AK/SK 签 JWT 用；无 APIKey 时必填
	AccessKey string
	SecretKey string
	// Model 模型标识：进提交路径的模型段，须与可灵开放平台的模型标识一致，必填
	Model string
	// ProxyURL 可选公网出口代理（IDC 环境访问公网 API 用）；空则直连
	ProxyURL string
	// Artifacts 解析自有产物 URL 及其公网可达性。私有产物读取后转 base64，
	// 公共产物 URL 与外部 URL/base64 原样透传。
	Artifacts artifact.Storage
	// Client 供测试注入；缺省按 ProxyURL 构造
	Client *http.Client
}

// klingTransport 是可灵各端点共享的传输层：鉴权、提交、轮询、素材内联。
// 无全局超时：单次请求由调用方 ctx 约束，任务全程时限由 job 机制掌握。
type klingTransport struct {
	cfg  KlingConfig
	http *http.Client
}

func newKlingTransport(cfg KlingConfig) (*klingTransport, error) {
	// 二选一：新版静态 API Key，或老 AK/SK 对。
	if cfg.APIKey == "" && (cfg.AccessKey == "" || cfg.SecretKey == "") {
		return nil, fmt.Errorf("videogen: kling requires apiKey (新版) or accessKey+secretKey (老 AK/SK)")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("videogen: kling model identifier is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultKlingBaseURL
	}
	baseURL, err := url.Parse(cfg.BaseURL)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" {
		return nil, fmt.Errorf("videogen: kling baseUrl %q must be an absolute HTTP(S) URL", cfg.BaseURL)
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
	return &klingTransport{cfg: cfg, http: client}, nil
}

// submit 提交任务：POST {baseUrl}/{route}/{model}，返回系统生成的任务 id。
func (k *klingTransport) submit(ctx context.Context, route string, body map[string]any) (string, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimSuffix(k.cfg.BaseURL, "/") + "/" + route + "/" + url.PathEscape(k.cfg.Model)
	var resp klingCreateResponse
	if err := k.call(ctx, http.MethodPost, endpoint, payload, &resp); err != nil {
		return "", fmt.Errorf("videogen: create task: %w", err)
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("videogen: create task rejected: %w",
			newKlingProviderError(0, resp.Code, resp.Message))
	}
	if resp.Data.ID == "" {
		return "", fmt.Errorf("videogen: create task returned empty task id")
	}
	return resp.Data.ID, nil
}

// Poll 状态映射：succeeded → Done（result 归一为标准 artifact），
// failed → Failed（带 message），submitted/processing → 进行中。
// HTTP/网络/业务码错误返回结构化 error，由能力 Activity 按状态码与业务码分级。
func (k *klingTransport) Poll(ctx context.Context, remoteTaskID string) (tool.PollResult, error) {
	if strings.TrimSpace(remoteTaskID) == "" {
		return tool.PollResult{}, fmt.Errorf("videogen: remote task id is required")
	}
	var resp klingQueryResponse
	if err := k.call(ctx, http.MethodGet, k.queryEndpoint(remoteTaskID), nil, &resp); err != nil {
		return tool.PollResult{}, fmt.Errorf("videogen: poll task %s: %w", remoteTaskID, err)
	}
	if resp.Code != 0 {
		return tool.PollResult{}, fmt.Errorf("videogen: poll task %s: %w",
			remoteTaskID, newKlingProviderError(0, resp.Code, resp.Message))
	}
	if len(resp.Data) == 0 {
		// 任务应存在却查无：按瞬时故障重试（避免刚提交时的可见性延迟误判为终态）。
		return tool.PollResult{}, fmt.Errorf("videogen: poll task %s: empty task list", remoteTaskID)
	}
	task := resp.Data[0]
	switch task.Status {
	case "succeeded":
		videoURL := firstVideoURL(task.Outputs)
		if videoURL == "" {
			return tool.PollResult{Failed: true, FailureReason: "任务成功但未返回视频链接"}, nil
		}
		// 可灵返回带 cacheKey/pkey 的临时签名 URL。若原样交给 LLM，它很容易在
		// 下游工具调用时只保留 .mp4 路径、删掉查询签名，CDN 随即返回 403；
		// URL 本身也会过期。任务完成时立即把成片落到受管产物存储，后续只传
		// 稳定 URL。未注入存储的纯客户端用法仍保持原行为。
		if k.cfg.Artifacts != nil {
			storedURL, err := k.persistVideo(ctx, remoteTaskID, videoURL)
			if err != nil {
				return tool.PollResult{}, fmt.Errorf("videogen: persist completed video: %w", err)
			}
			videoURL = storedURL
		}
		result, err := json.Marshal(map[string]any{
			"artifacts": []tool.ResultArtifact{{Type: "video", URI: videoURL}},
		})
		if err != nil {
			return tool.PollResult{}, err
		}
		return tool.PollResult{Done: true, Result: result}, nil
	case "failed":
		reason := task.Message
		if reason == "" {
			reason = "failed"
		}
		return tool.PollResult{Failed: true, FailureReason: reason}, nil
	default:
		// submitted / processing：透传远端状态原值供等待方展示
		return tool.PollResult{StatusDetail: task.Status}, nil
	}
}

func (k *klingTransport) persistVideo(ctx context.Context, remoteTaskID, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := k.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("download completed video: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download completed video: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, klingMaxVideoBytes+1))
	if err != nil {
		return "", fmt.Errorf("read completed video: %w", err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("completed video is empty")
	}
	if len(data) > klingMaxVideoBytes {
		return "", fmt.Errorf("completed video exceeds %d bytes", klingMaxVideoBytes)
	}
	contentType := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = "video/mp4"
	}
	sum := sha256.Sum256([]byte(k.cfg.Model + "\x00" + remoteTaskID))
	key := fmt.Sprintf("capabilities/videos/kling/%x.mp4", sum[:16])
	object, err := k.cfg.Artifacts.Put(ctx, key, data, contentType)
	if err != nil {
		return "", fmt.Errorf("store completed video: %w", err)
	}
	return object.URL, nil
}

// firstVideoURL 取首个视频产物链接（outputs 可能混有 image/audio 等类型）。
func firstVideoURL(outputs []klingOutput) string {
	for _, o := range outputs {
		if o.Type == "video" && o.URL != "" {
			return o.URL
		}
	}
	return ""
}

// inlineImage 将自有私有产物读取并转为纯 base64；可灵协议不带 data: 前缀。
// 自有公共产物、公网 URL 和调用方已给的 base64 原样透传。
func (k *klingTransport) inlineImage(ctx context.Context, image string) (string, error) {
	if k.cfg.Artifacts == nil {
		return image, nil
	}
	object, ok := k.cfg.Artifacts.Resolve(image)
	if !ok || object.Visibility == artifact.Public {
		return image, nil
	}
	r, err := k.cfg.Artifacts.Open(ctx, object)
	if err != nil {
		return "", fmt.Errorf("videogen: open image %s: %w", image, err)
	}
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(io.LimitReader(r, klingMaxImageBytes+1))
	if err != nil {
		return "", fmt.Errorf("videogen: read image %s: %w", image, err)
	}
	if len(data) > klingMaxImageBytes {
		return "", fmt.Errorf("videogen: image %s exceeds %d bytes", image, klingMaxImageBytes)
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// queryEndpoint 查询任务：GET /tasks?task_ids={id}（支持逗号分隔批量，此处单任务）。
func (k *klingTransport) queryEndpoint(taskID string) string {
	return strings.TrimSuffix(k.cfg.BaseURL, "/") + "/tasks?task_ids=" + url.QueryEscape(taskID)
}

func (k *klingTransport) call(ctx context.Context, method, endpoint string, body []byte, out any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	// 新版静态 API Key 直接 Bearer；否则用 AK/SK 现签 JWT（老鉴权）。
	if k.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+k.cfg.APIKey)
	} else {
		req.Header.Set("Authorization", "Bearer "+signJWT(k.cfg.AccessKey, k.cfg.SecretKey, time.Now()))
	}

	resp, err := k.http.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, klingMaxResponseBytes))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var failure struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(raw, &failure); err == nil {
			return newKlingProviderError(resp.StatusCode, failure.Code, failure.Message)
		}
		return &providerError{status: resp.StatusCode, message: truncate(raw, 300)}
	}
	return json.Unmarshal(raw, out)
}

func newKlingProviderError(status, code int, message string) *providerError {
	codeText := ""
	if code != 0 {
		codeText = strconv.Itoa(code)
	}
	return &providerError{
		status:    status,
		code:      codeText,
		message:   message,
		permanent: code == klingPermanentRejectionCode,
	}
}

// klingCreateResponse 提交任务响应：data 为对象，携带系统生成的任务 id 与初始状态。
type klingCreateResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"data"`
}

// klingQueryResponse 查询任务响应：data 为任务数组（本客户端按单任务查询取首元素）。
type klingQueryResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    []struct {
		ID      string        `json:"id"`
		Status  string        `json:"status"`
		Message string        `json:"message"`
		Outputs []klingOutput `json:"outputs"`
	} `json:"data"`
}

// klingOutput 任务产物：视频类型携带下载 url（同一任务的 outputs 可能混有其它类型）。
type klingOutput struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// signJWT HS256 手签（30 行逻辑不值得引 jwt 依赖）：
// header {alg: HS256, typ: JWT}, payload {iss: ak, exp: now+1800, nbf: now-5}。
func signJWT(accessKey, secretKey string, now time.Time) string {
	enc := func(v any) string {
		raw, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	head := enc(map[string]string{"alg": "HS256", "typ": "JWT"})
	payload := enc(map[string]any{
		"iss": accessKey,
		"exp": now.Unix() + 1800,
		"nbf": now.Unix() - 5,
	})
	msg := head + "." + payload
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(msg))
	return msg + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

/* ---------- 图生视频端点：POST /image-to-video/{model} ---------- */

// klingI2VRoute 图生视频的提交路径段。
const klingI2VRoute = "image-to-video"

// KlingI2VRequest 是可灵图生视频接口的入参（对齐官方「图生视频」文档）：
// 首帧图必填、尾帧图可选，其余是输出配置。素材只有首尾帧——参考图、参考视频
// 与主体是 Omni 那个端点的事，不在这里表达。
type KlingI2VRequest struct {
	// FirstFrame 首帧图 URL（或受管产物 URI / base64），必填
	FirstFrame string `json:"firstFrame"`
	// LastFrame 尾帧图，可选；仅首帧或首尾帧，不支持仅尾帧
	LastFrame string `json:"lastFrame,omitempty"`
	// Prompt 画面运动与内容描述，必填
	Prompt string `json:"prompt"`
	// Resolution 720p / 1080p / 4k；空取 1080p
	Resolution string `json:"resolution,omitempty"`
	// Duration 秒数 "3"~"15"；空取 "5"
	Duration string `json:"duration,omitempty"`
	// Audio native（画面适配声音）/ off；空取 native
	Audio string `json:"audio,omitempty"`
	// MultiShot 是否生成多镜头视频；空取 true
	MultiShot *bool `json:"multiShot,omitempty"`
}

// Normalize 校验入参并落实缺省，幂等：activity 归一一次用于回执，客户端再归一
// 一次以便被直调时同样安全。
func (r KlingI2VRequest) Normalize() (KlingI2VRequest, error) {
	r.FirstFrame = strings.TrimSpace(r.FirstFrame)
	r.LastFrame = strings.TrimSpace(r.LastFrame)
	if r.FirstFrame == "" {
		return r, fmt.Errorf("videogen: kling image-to-video requires a first frame")
	}
	if strings.TrimSpace(r.Prompt) == "" {
		return r, fmt.Errorf("videogen: prompt is required")
	}
	seconds, err := parseDuration("kling", r.Duration, klingMinDuration, klingMaxDuration, klingDefaultDuration)
	if err != nil {
		return r, err
	}
	r.Duration = strconv.Itoa(seconds)
	if r.Resolution, err = pickEnum("kling", "resolution", r.Resolution,
		klingDefaultResolution, "720p", "1080p", "4k"); err != nil {
		return r, err
	}
	if r.Audio, err = pickEnum("kling", "audio mode", r.Audio,
		klingDefaultAudio, "native", "off"); err != nil {
		return r, err
	}
	if r.MultiShot == nil {
		multiShot := true
		r.MultiShot = &multiShot
	}
	return r, nil
}

// Params 归一后的实际生效参数，随提交回执与产物事实一起落账。
func (r KlingI2VRequest) Params() map[string]any {
	return map[string]any{
		"resolution": r.Resolution,
		"duration":   r.Duration,
		"audio":      r.Audio,
		"multiShot":  r.MultiShot != nil && *r.MultiShot,
	}
}

// KlingI2V 是可灵图生视频端点的客户端。
type KlingI2V struct{ *klingTransport }

func NewKlingI2V(cfg KlingConfig) (*KlingI2V, error) {
	transport, err := newKlingTransport(cfg)
	if err != nil {
		return nil, err
	}
	return &KlingI2V{klingTransport: transport}, nil
}

func (k *KlingI2V) Submit(ctx context.Context, req KlingI2VRequest) (string, error) {
	req, err := req.Normalize()
	if err != nil {
		return "", err
	}
	// contents 用 type 区分素材：prompt 文本、first_frame 首帧、last_frame 尾帧。
	// 命中内网前缀的图转 base64 直传，其余原样透传。
	firstFrame, err := k.inlineImage(ctx, req.FirstFrame)
	if err != nil {
		return "", err
	}
	contents := []map[string]any{
		{"type": "prompt", "text": req.Prompt},
		{"type": "first_frame", "url": firstFrame},
	}
	if req.LastFrame != "" {
		lastFrame, err := k.inlineImage(ctx, req.LastFrame)
		if err != nil {
			return "", err
		}
		contents = append(contents, map[string]any{"type": "last_frame", "url": lastFrame})
	}
	duration, _ := strconv.Atoi(req.Duration)
	return k.submit(ctx, klingI2VRoute, map[string]any{
		"contents": contents,
		"settings": map[string]any{
			"resolution": req.Resolution,
			"duration":   duration,
			"audio":      req.Audio,
			"multi_shot": *req.MultiShot,
		},
	})
}

// Basic 返回本端点的公共最小接口，供 Pipeline 与编排型能力按模型名直接调用。
// （它们只要"把这张图动起来"，不该被迫先挑一家厂商）。
func (k *KlingI2V) Basic() Client { return klingI2VBasic{k} }

// klingI2VBasic 把图生视频端点适配成公共最小接口。
type klingI2VBasic struct{ *KlingI2V }

func (c klingI2VBasic) Submit(ctx context.Context, req Request) (string, error) {
	return c.KlingI2V.Submit(ctx, KlingI2VRequest{
		FirstFrame: req.FirstFrame,
		LastFrame:  req.LastFrame,
		Prompt:     req.Prompt,
		Resolution: req.Resolution,
		Duration:   req.Duration,
		Audio:      req.Audio,
	})
}
