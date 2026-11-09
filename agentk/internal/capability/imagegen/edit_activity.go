package imagegen

import (
	"context"
	"fmt"
	"strings"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/heartbeat"

	"agentk/internal/temporalx"
	"agentk/internal/tool"
)

// EditActivityName 是参考图生图能力的公开 Temporal Activity 名（对外
// ABI：agent 工具派发、pipeline 与第三方 workflow 按名直调）。
const EditActivityName = "agentk.capability.edit_image"

// EditCapabilityInput 是公开 payload：只包含参考图生图业务参数。Model 是 models
// catalog 中的 model entry name，调用方必须显式选择。
type EditCapabilityInput struct {
	Model string `json:"model"`
	// Prompt 编辑指令，必填。
	Prompt string `json:"prompt"`
	// ReferenceImages 参考图 URL（至少一张），必须是受管产物存储产出的 URL。
	ReferenceImages []string `json:"referenceImages"`
	// AspectRatio 可选画幅；空则由供应商实现决定（编辑模型通常按输入图推断）。
	AspectRatio string `json:"aspectRatio,omitempty"`
}

// RunEdit 执行参考图生图：校验 → 生成 → 产物存储 → 返回 URL。与文生图共用
// Activity 实例共用同一份 catalog 与 storage，并按 EditActivityName 注册。
func (a *Activity) RunEdit(ctx context.Context, in EditCapabilityInput) (CapabilityOutput, error) {
	const op = "capability edit_image"
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
	if in.AspectRatio != "" {
		if _, err := imageSize(azureSizes, in.AspectRatio, ""); err != nil {
			return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
				temporalx.InvalidActivityInput("%v", err))
		}
	}
	if len(in.ReferenceImages) == 0 {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("at least one reference image is required"))
	}
	for _, ref := range in.ReferenceImages {
		if strings.TrimSpace(ref) == "" {
			return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
				temporalx.InvalidActivityInput("reference image URL must not be empty"))
		}
		if _, ok := a.storage.Resolve(ref); !ok {
			return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
				temporalx.InvalidActivityInput("reference image must be a URL produced by the configured artifact storage"))
		}
	}
	client, maxRef, ok := a.models.EditClient(in.Model)
	if !ok {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("image_edit capability %q is not available", in.Model)))
	}
	if maxRef > 0 && len(in.ReferenceImages) > maxRef {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("image_edit model %q accepts at most %d reference image(s), got %d",
				in.Model, maxRef, len(in.ReferenceImages)))
	}

	stop := heartbeat.Start(ctx)
	defer stop()
	img, err := client.Edit(ctx, EditRequest{
		Prompt: in.Prompt, ReferenceImages: in.ReferenceImages, AspectRatio: in.AspectRatio,
	})
	if err != nil {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op+": edit", err)
	}
	object, err := a.storage.Put(ctx, capabilityImageKey(ctx), img.Data, img.MIME)
	if err != nil {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op+": store image", err)
	}
	// aspectRatio 原样透传而不统一默认值：两个供应商的默认行为不同，qwenedit 留空由模型按
	// 输入图推断，gptimage 用 1:1——统一成任一个都会改变行为。未指定时报 auto。
	effectiveRatio := in.AspectRatio
	if effectiveRatio == "" {
		effectiveRatio = "auto"
	}
	return CapabilityOutput{
		Artifacts: []tool.ResultArtifact{{
			Type: "image", URI: object.URL,
			Description: fmt.Sprintf(
				"使用 edit_image 根据 %d 张参考图和指令 %q 生成的图片（模型：%s）",
				len(in.ReferenceImages), in.Prompt, in.Model),
			Model: in.Model,
			Params: mergeParams(map[string]any{
				"aspectRatio":     effectiveRatio,
				"referenceImages": len(in.ReferenceImages),
			}, img.Params),
		}},
	}, nil
}
