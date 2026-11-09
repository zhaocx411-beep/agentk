package imagegen

import (
	_ "embed"
	"time"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
)

const (
	CapabilityID    = "agentk.generate_image"
	ToolName        = "generate_image"
	toolDescription = "根据文字描述生成一张图片，返回图片的 URL。" +
		"生成的图片可作为生视频工具的首帧图。" +
		"生成有随机性：不满意可调整 prompt 重试；因内容审核被拒时换措辞才有效。"
	EditCapabilityID    = "agentk.edit_image"
	EditToolName        = "edit_image"
	editToolDescription = "基于一张或多张参考图和文字指令生成/编辑图片，返回新图片的 URL。" +
		"参考图取值为上下文里的图片占位符——通常是先前 generate_image 或 edit_image 返回的 artifacts 数组元素的 uri。" +
		"生成的图片可作为生视频工具的首帧图。" +
		"生成有随机性：不满意可调整 prompt 重试；因内容审核被拒时换措辞才有效。"
	toolTimeout          = 5 * time.Minute
	maxAttempts          = 2
	activityStartToClose = toolTimeout + time.Minute
)

//go:embed generate_image.schema.json
var generateImageInputSchema []byte

//go:embed edit_image.schema.json
var editImageInputSchema []byte

// ManifestCapabilities 描述文生图与参考图编辑的稳定 Activity ABI。
func ManifestCapabilities() []sdkcapability.Capability {
	return []sdkcapability.Capability{
		{
			ID:             CapabilityID,
			TemporalType:   ActivityName,
			Description:    "根据文字描述生成图片并返回受管图片 URL。",
			InputSchema:    generateImageInputSchema,
			OutputSchema:   sdkcapability.SchemaOf[CapabilityOutput](),
			TimeoutSeconds: int64(activityStartToClose.Seconds()),
			Tool: &sdkcapability.Tool{
				Description: toolDescription,
				Mode:        sdkcapability.ToolModeBlocking, MaxAttempts: maxAttempts,
			},
		},
		{
			ID:             EditCapabilityID,
			TemporalType:   EditActivityName,
			Description:    "根据参考图和文字指令编辑或生成图片。",
			InputSchema:    editImageInputSchema,
			OutputSchema:   sdkcapability.SchemaOf[CapabilityOutput](),
			TimeoutSeconds: int64(activityStartToClose.Seconds()),
			Tool: &sdkcapability.Tool{
				Description: editToolDescription,
				Mode:        sdkcapability.ToolModeBlocking, MaxAttempts: maxAttempts,
			},
		},
	}
}
