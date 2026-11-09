package videocompose

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"agentk/internal/artifact"
	"agentk/internal/temporalx"
	"agentk/internal/tool"
)

// ActivityName 是视频拼接能力的公开 Temporal Activity 名（对外 ABI：pipeline 与
// 第三方 workflow 按名直调）。
const ActivityName = "agentk.capability.concat_videos"

// ActivityInput 是公开 payload，只包含视频拼接业务参数。
type ActivityInput struct {
	// VideoURLs 片段 URL，按拼接顺序排列，至少一个。
	VideoURLs []string `json:"videoUrls"`
	// Size 输出尺寸策略：auto 跟随首段实际显示尺寸，1080p 保持首段比例并归一到 1080 档。
	Size string `json:"size,omitempty"`
}

// ActivityOutput 是公开返回：成片 URL 与片段数。
type ActivityOutput struct {
	Artifacts []tool.ResultArtifact `json:"artifacts"`
	ClipCount int                   `json:"clipCount"`
}

// Activity 实现视频拼接能力的 Temporal Activity。
type Activity struct {
	storage artifact.Storage
}

func NewActivity(storage artifact.Storage) *Activity {
	return &Activity{storage: storage}
}

func (a *Activity) Run(ctx context.Context, in ActivityInput) (ActivityOutput, error) {
	const op = "capability concat_videos"
	if a.storage == nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("videocompose storage is required")))
	}
	if len(in.VideoURLs) == 0 {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("videoUrls is required"))
	}
	if in.Size == "" {
		in.Size = outputSizeAuto
	}
	if in.Size != outputSizeAuto && in.Size != outputSize1080p {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("size must be auto or 1080p"))
	}
	out, err := concat(ctx, in.VideoURLs, in.Size)
	if err != nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op, err)
	}
	object, err := a.storage.Put(ctx, capabilityVideoKey(ctx), out, "video/mp4")
	if err != nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op+": store video", err)
	}
	return ActivityOutput{
		Artifacts: []tool.ResultArtifact{{
			Type: "video", URI: object.URL,
			Description: fmt.Sprintf("使用 concat_videos 按顺序拼接 %d 段视频（%s）后生成",
				len(in.VideoURLs), strings.Join(in.VideoURLs, "、")),
		}},
		ClipCount: len(in.VideoURLs),
	}, nil
}

func capabilityVideoKey(ctx context.Context) string {
	sum := sha256.Sum256([]byte(temporalx.ActivityIdempotencyKey(ctx)))
	return fmt.Sprintf("capabilities/videos/%x.mp4", sum[:16])
}
