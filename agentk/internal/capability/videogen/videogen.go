// Package videogen 是生视频能力的供应商适配层。
//
// 分层：一个厂商接口对应一个工具。agent 面按接口拆开——kling_text_to_video
// （可灵文生视频）、kling_image_to_video（可灵图生视频）、kling_omni_video
// （可灵 Omni）、seedance_generate_video（方舟 Seedance）。每个工具的入参表
// 就是那个接口自己的参数表，描述文案各自维护。模型选工具即选接口，不必在一份
// "能力交集" schema 里猜哪个字段对当次厂商有效；反过来，某家独有的素材类型
// （Omni 的参考视频、Seedance 的参考音频）也不必为了兼容另一家而被砍掉。
//
// 另有一套公共最小接口（Request + Client + ActivityName）：只包含首帧图和描述
// 参数。它不进 agent 工具面，只给 pipeline 与编排型能力（storyboard 等）按
// 模型条目名直调——那些编排只要"把这张图动起来"，不该被迫先挑一家厂商。
// 不接收首帧的端点（可灵文生视频）不实现该接口，因为适配时只能丢弃首帧。
package videogen

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"agentk/internal/tool"
)

// Request 是实现公共最小接口的生视频端点共同支持的参数，不含供应商协议细节。
// 它只服务 Pipeline 直调；agent 工具各自使用对应供应商的请求类型。
type Request struct {
	// FirstFrame 首帧图：公网可读 URL（或供应商支持的内联形式），必填
	FirstFrame string `json:"firstFrame"`
	// LastFrame 尾帧图，可选；仅首帧或首尾帧，不支持仅尾帧
	LastFrame string `json:"lastFrame,omitempty"`
	// Prompt 画面运动与内容描述
	Prompt string `json:"prompt"`
	// Resolution 输出清晰度 720p / 1080p / 4k；空取 1080p
	Resolution string `json:"resolution,omitempty"`
	// Duration 片段秒数 "4"~"15"（三家共同支持的区间）；空取 "5"
	Duration string `json:"duration,omitempty"`
	// Audio 是否生成原生声音 native / off；空取 off
	Audio string `json:"audio,omitempty"`
}

// Client 是公共最小接口对应的生视频端点：提交任务 + 查询状态。
// 提交与轮询是同一异步任务的两半（与 tool 包的 async 约定一致），成对实现。
type Client interface {
	// Submit 提交生成任务，返回远端任务 id。
	Submit(ctx context.Context, req Request) (remoteTaskID string, err error)
	// Poller 查询远端任务状态；error 保留 HTTP/业务语义，由 Activity 统一分级。
	tool.Poller
}

// 各供应商客户端都提供提交和轮询，只有请求类型不同。这些类型对应各工具的独立
// 入参表；在类型上分开，可以在装配期避免把模型条目挂到错误工具。
type (
	// KlingT2VClient 可灵文生视频端点
	KlingT2VClient interface {
		Submit(ctx context.Context, req KlingT2VRequest) (remoteTaskID string, err error)
		tool.Poller
	}
	// KlingI2VClient 可灵图生视频端点
	KlingI2VClient interface {
		Submit(ctx context.Context, req KlingI2VRequest) (remoteTaskID string, err error)
		tool.Poller
	}
	// KlingOmniClient 可灵 Omni 端点
	KlingOmniClient interface {
		Submit(ctx context.Context, req KlingOmniRequest) (remoteTaskID string, err error)
		tool.Poller
	}
	// SeedanceClient 方舟 Seedance 端点
	SeedanceClient interface {
		Submit(ctx context.Context, req SeedanceRequest) (remoteTaskID string, err error)
		tool.Poller
	}
)

// providerError 保留厂商 HTTP/业务错误的结构化语义，供 Activity 统一决定是否重试。
type providerError struct {
	status    int
	code      string
	message   string
	permanent bool
}

func (e *providerError) Error() string {
	prefix := "provider error"
	if e.status > 0 {
		prefix = fmt.Sprintf("HTTP %d", e.status)
	}
	if e.code != "" {
		prefix += " code=" + e.code
	}
	if e.message != "" {
		prefix += ": " + e.message
	}
	return prefix
}

func (e *providerError) StatusCode() int { return e.status }
func (e *providerError) Permanent() bool { return e.permanent }

// 公共最小接口的默认值。各供应商把这些值转换成自己的协议参数；在这里统一默认值不改变行为，
// 但让"实际用了什么参数"在提交那一刻就是确定的。
const (
	defaultResolution = "1080p"
	defaultDuration   = "5"
	defaultAudio      = "off"
)

// normalizeRequest 把缺省落实成具体值，使请求不再带"空 = 由下游决定"的歧义。
// 须在 validateRequest 之后调用。
func normalizeRequest(req Request) Request {
	if req.Resolution == "" {
		req.Resolution = defaultResolution
	}
	if req.Duration == "" {
		req.Duration = defaultDuration
	}
	if req.Audio == "" {
		req.Audio = defaultAudio
	}
	return req
}

func validateRequest(req Request) error {
	if strings.TrimSpace(req.FirstFrame) == "" {
		return fmt.Errorf("videogen: first frame is required")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return fmt.Errorf("videogen: prompt is required")
	}
	switch req.Resolution {
	case "", "720p", "1080p", "4k":
	default:
		return fmt.Errorf("videogen: unsupported resolution %q", req.Resolution)
	}
	if req.Duration != "" {
		duration, err := strconv.Atoi(req.Duration)
		if err != nil || duration < 4 || duration > 15 {
			return fmt.Errorf("videogen: duration must be an integer between 4 and 15 seconds")
		}
	}
	switch req.Audio {
	case "", "native", "off":
	default:
		return fmt.Errorf("videogen: unsupported audio mode %q", req.Audio)
	}
	return nil
}

// parseDuration 解析秒数入参：空取 fallback，否则须是 [min, max] 内的整数。
func parseDuration(dialect, value string, minSeconds, maxSeconds, fallback int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	seconds, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("videogen: invalid duration %q: %w", value, err)
	}
	if seconds < minSeconds || seconds > maxSeconds {
		return 0, fmt.Errorf("videogen: %s duration must be between %d and %d seconds, got %d",
			dialect, minSeconds, maxSeconds, seconds)
	}
	return seconds, nil
}

// pickEnum 校验枚举入参：空取 fallback，否则须命中 allowed。
func pickEnum(dialect, field, value, fallback string, allowed ...string) (string, error) {
	if value == "" {
		value = fallback
	}
	for _, a := range allowed {
		if value == a {
			return value, nil
		}
	}
	return "", fmt.Errorf("videogen: %s unsupported %s %q", dialect, field, value)
}

// trimmedList 整理素材列表：去掉首尾空白，拒绝空串，并限制数量。
// 数量上限由厂商文档明确规定，越界时在提交前报错，不用计费调用试探上游。
func trimmedList(dialect, field string, values []string, limit int) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if limit > 0 && len(values) > limit {
		return nil, fmt.Errorf("videogen: %s accepts at most %d %s, got %d",
			dialect, limit, field, len(values))
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return nil, fmt.Errorf("videogen: %s %s must not contain blank entries", dialect, field)
		}
		out = append(out, trimmed)
	}
	return out, nil
}

func truncate(raw []byte, n int) string {
	s := string(bytes.TrimSpace(raw))
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
