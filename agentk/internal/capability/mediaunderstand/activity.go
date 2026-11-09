package mediaunderstand

import (
	"context"
	"fmt"
	"math"
	"strings"

	"agentk/internal/temporalx"
)

// ActivityName 是媒体理解能力的公开 Temporal Activity 名（对外 ABI：
// agent 工具派发、pipeline 与第三方 workflow 按名直调）。
const ActivityName = "agentk.capability.understand_media"

// SegmentActivityName 是按原视频时间区间理解单个视频的公开 Activity 名。
const SegmentActivityName = "agentk.capability.understand_video_segment"

// CapabilityInput 是公开 payload：只包含媒体理解业务参数。Model 是 models
// catalog 中的 model entry name，调用方必须显式选择。
type CapabilityInput struct {
	Model string `json:"model"`
	// MediaURLs 要看的视频/图片 URL，按送进模型的顺序排列，至少一个。
	MediaURLs []string `json:"mediaUrls"`
	// Prompt 要模型回答的问题或分析要求，必填。
	Prompt string `json:"prompt"`
}

// CapabilityOutput 是公开返回：文字回答、实际选用的模型条目与实际生效的参数
// （每段媒体的判定类型与字节数等）。
type CapabilityOutput struct {
	Text   string         `json:"text"`
	Model  string         `json:"model"`
	Params map[string]any `json:"params,omitempty"`
}

// SegmentInput 是按时间区间理解单个视频的公开 payload。FPS 为 0 时使用模型缺省值。
type SegmentInput struct {
	Model        string  `json:"model"`
	VideoURL     string  `json:"videoUrl"`
	StartSeconds float64 `json:"startSeconds"`
	EndSeconds   float64 `json:"endSeconds"`
	FPS          float64 `json:"fps,omitempty"`
	Prompt       string  `json:"prompt"`
}

// ModelLookup 按显式 model entry name 解析媒体理解客户端。
type ModelLookup interface {
	UnderstandClient(name string) (Client, bool)
}

// Activity 实现媒体理解能力的 Temporal Activity：同步执行（下载素材 + 上游按帧解码，
// 以分钟计），没有任务 id，不能轮询。
type Activity struct {
	models ModelLookup
}

func NewActivity(models ModelLookup) *Activity {
	return &Activity{models: models}
}

func (a *Activity) Run(ctx context.Context, in CapabilityInput) (CapabilityOutput, error) {
	const op = "capability understand_media"
	if a.models == nil {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("mediaunderstand model lookup is required")))
	}
	if in.Model == "" {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("model is required"))
	}
	if len(in.MediaURLs) == 0 {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("mediaUrls is required"))
	}
	if strings.TrimSpace(in.Prompt) == "" {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("prompt is required"))
	}
	client, ok := a.models.UnderstandClient(in.Model)
	if !ok {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("media_understand capability %q is not available", in.Model)))
	}
	result, err := client.Understand(ctx, Request{MediaURLs: in.MediaURLs, Prompt: in.Prompt})
	if err != nil {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op+": understand", err)
	}
	return CapabilityOutput{Text: result.Text, Model: in.Model, Params: result.Params}, nil
}

// RunSegment 使用 provider 原生视频裁剪参数理解指定区间，不创建临时视频。
func (a *Activity) RunSegment(ctx context.Context, in SegmentInput) (CapabilityOutput, error) {
	const op = "capability understand_video_segment"
	if a.models == nil {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("mediaunderstand model lookup is required")))
	}
	if in.Model == "" {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("model is required"))
	}
	if strings.TrimSpace(in.VideoURL) == "" {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("videoUrl is required"))
	}
	if !validNumber(in.StartSeconds) || in.StartSeconds < 0 {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("startSeconds must be a finite number greater than or equal to 0"))
	}
	if !validNumber(in.EndSeconds) || in.EndSeconds <= in.StartSeconds {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("endSeconds must be finite and greater than startSeconds"))
	}
	if !validNumber(in.FPS) || in.FPS < 0 || in.FPS > 24 {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("fps must be 0 (provider default) or in (0, 24]"))
	}
	if strings.TrimSpace(in.Prompt) == "" {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("prompt is required"))
	}
	client, ok := a.models.UnderstandClient(in.Model)
	if !ok {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("media_understand capability %q is not available", in.Model)))
	}
	result, err := client.Understand(ctx, Request{
		MediaURLs: []string{in.VideoURL},
		Prompt:    in.Prompt,
		VideoSegment: &VideoSegment{
			StartSeconds: in.StartSeconds,
			EndSeconds:   in.EndSeconds,
			FPS:          in.FPS,
		},
	})
	if err != nil {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op+": understand", err)
	}
	return CapabilityOutput{Text: result.Text, Model: in.Model, Params: result.Params}, nil
}

func validNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
