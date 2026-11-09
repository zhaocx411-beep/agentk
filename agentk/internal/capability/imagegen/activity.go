package imagegen

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/heartbeat"

	"agentk/internal/artifact"
	"agentk/internal/temporalx"
	"agentk/internal/tool"
)

const (
	// ActivityName 是生图能力的公开 Temporal Activity 名（对外 ABI：
	// agent 工具派发、pipeline 与第三方 workflow 都按这一个名字直调）。
	ActivityName = "agentk.capability.generate_image"

	// defaultAspectRatio 仅用于文生图。图片编辑保留空值，由供应商实现决定默认画幅。
	defaultAspectRatio = "1:1"
)

// CapabilityInput 是公开 payload：只包含生图业务参数。Model 是 models catalog
// 中的 model entry name，调用方必须显式选择。
type CapabilityInput struct {
	Model string `json:"model"`
	// Prompt 画面内容与风格描述，必填。
	Prompt string `json:"prompt"`
	// AspectRatio 画幅 "1:1" / "9:16" / "16:9"；空用 "1:1"。
	AspectRatio string `json:"aspectRatio,omitempty"`
}

// CapabilityOutput 是公开返回：图片 URL、实际选用的模型条目与实际生效的参数
// （供应商补充的 size、quality 等参数必须在此记录，否则之后无法还原实际请求）。
type CapabilityOutput struct {
	Artifacts []tool.ResultArtifact `json:"artifacts"`
}

// ModelLookup 按显式 model entry name 解析生图客户端。
type ModelLookup interface {
	ImageClient(name string) (Client, bool)
	// EditClient 解析参考图生图客户端，一并给出该条目的参考图数量上限（0 = 不限）。
	EditClient(name string) (EditClient, int, bool)
}

// Activity 实现生图 Activity：同步执行（生成 → 产物存储 → 返回 URL），没有任务 id，
// 不可轮询，也不使用 videogen 的任务续接机制。
type Activity struct {
	models  ModelLookup
	storage artifact.Storage
}

func NewActivity(models ModelLookup, storage artifact.Storage) *Activity {
	return &Activity{models: models, storage: storage}
}

func (a *Activity) Run(ctx context.Context, in CapabilityInput) (CapabilityOutput, error) {
	const op = "capability generate_image"
	if a.models == nil || a.storage == nil {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("imagegen model lookup and storage are required")))
	}
	if in.Model == "" {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("model is required"))
	}
	if strings.TrimSpace(in.Prompt) == "" {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("prompt is required"))
	}
	aspectRatio := in.AspectRatio
	if aspectRatio == "" {
		aspectRatio = defaultAspectRatio
	}
	if _, err := imageSize(azureSizes, aspectRatio, defaultAspectRatio); err != nil {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("%v", err))
	}
	client, ok := a.models.ImageClient(in.Model)
	if !ok {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("image capability %q is not available", in.Model)))
	}

	stop := heartbeat.Start(ctx)
	defer stop()
	img, err := client.Generate(ctx, Request{Prompt: in.Prompt, AspectRatio: aspectRatio})
	if err != nil {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op+": generate", err)
	}
	object, err := a.storage.Put(ctx, capabilityImageKey(ctx), img.Data, img.MIME)
	if err != nil {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op+": store image", err)
	}
	return CapabilityOutput{
		Artifacts: []tool.ResultArtifact{{
			Type:        "image",
			URI:         object.URL,
			Description: fmt.Sprintf("使用 generate_image 按提示词 %q 生成的图片（模型：%s，画幅：%s）", in.Prompt, in.Model, aspectRatio),
			Model:       in.Model,
			Params:      mergeParams(map[string]any{"aspectRatio": aspectRatio}, img.Params),
		}},
	}, nil
}

// capabilityImageKey 对同一个 Temporal activity 调用稳定，activity retry 覆盖
// 同一对象；它不接受调用方提供的租户或路径。
func capabilityImageKey(ctx context.Context) string {
	sum := sha256.Sum256([]byte(temporalx.ActivityIdempotencyKey(ctx)))
	return fmt.Sprintf("capabilities/images/%x.png", sum[:16])
}
