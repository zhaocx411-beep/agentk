package videogen

import (
	_ "embed"
	"time"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
)

const (
	KlingT2VToolName  = "kling_text_to_video"
	KlingI2VToolName  = "kling_image_to_video"
	KlingOmniToolName = "kling_omni_video"
	SeedanceToolName  = "seedance_generate_video"

	generationGuidance = "视频生成可能需要较长时间；" +
		"生成有随机性：不满意可调整 prompt 重试；因内容审核被拒时换措辞才有效，原样重试无用。"
)

//go:embed kling_text_to_video.schema.json
var klingT2VInputSchema []byte

//go:embed kling_image_to_video.schema.json
var klingI2VInputSchema []byte

//go:embed kling_omni_video.schema.json
var klingOmniInputSchema []byte

//go:embed seedance_generate_video.schema.json
var seedanceInputSchema []byte

//go:embed video_output.schema.json
var videoOutputSchema []byte

// ManifestActivityCapability 描述供 pipeline 与编排型能力阻塞直调的通用视频 ABI。
func ManifestActivityCapability() sdkcapability.Capability {
	return sdkcapability.Capability{
		ID:             "agentk.generate_video",
		TemporalType:   ActivityName,
		Description:    "提交视频生成任务并等待完成，返回生成视频。",
		InputSchema:    sdkcapability.SchemaOf[ActivityInput](),
		OutputSchema:   sdkcapability.SchemaOf[ActivityOutput](),
		TimeoutSeconds: int64((30 * time.Minute).Seconds()),
	}
}

// ManifestWorkflowCapabilities 描述四个静态异步视频 Tool ABI。
func ManifestWorkflowCapabilities() []sdkcapability.Capability {
	return []sdkcapability.Capability{
		workflowCapability(
			"agentk.kling_text_to_video", KlingT2VWorkflowType,
			"可灵文生视频：只给一段文字描述，从零生成一段短视频，不接受任何图片或视频素材。"+
				"手上已有首帧图、要把它动起来时用 kling_image_to_video 而不是这个——"+
				"把图的内容改写成文字描述，人物与商品的样子是保不住的。"+
				generationGuidance,
			klingT2VInputSchema,
		),
		workflowCapability(
			"agentk.kling_image_to_video", KlingI2VWorkflowType,
			"可灵图生视频：给定一张首帧图（可选再给尾帧图）和一段画面描述，生成一段短视频。"+
				"适合把已有静态图动起来、或做首尾帧过渡。"+
				"手上没有图、要凭文字从零生成，用 kling_text_to_video；"+
				"要用参考图/参考视频改画面、或编辑已有视频，用 kling_omni_video；"+
				"需要参考音频或更多画幅比例，用 seedance_generate_video。"+
				generationGuidance,
			klingI2VInputSchema,
		),
		workflowCapability(
			"agentk.kling_omni_video", KlingOmniWorkflowType,
			"可灵 Omni 视频生成：一段描述 + 任意组合的素材（首尾帧图、元素参考图、参考视频、待编辑视频）生成视频。"+
				"用它来：按参考图保持角色/物体/风格一致、借鉴另一段视频的运镜与动作、或直接编辑一段已有视频。"+
				"素材在 prompt 里用 @image_n / @video_1 指名。"+
				"一个素材都没有时用 kling_text_to_video；"+
				"只是把一张图动起来（无参考素材）时用 kling_image_to_video，更简单也更便宜。"+
				generationGuidance,
			klingOmniInputSchema,
		),
		workflowCapability(
			"agentk.seedance_generate_video", SeedanceWorkflowType,
			"方舟 Seedance 视频生成：一个接口覆盖文生视频、首帧/首尾帧图生视频与多模态参考"+
				"（参考图 + 参考视频 + 参考音频），用哪种形态取决于你给了哪些素材。"+
				"需要参考音频、480p 低清省钱档或 21:9 / 4:3 这类画幅时用它——这些是它独有的。"+
				generationGuidance,
			seedanceInputSchema,
		),
	}
}

func workflowCapability(
	id string,
	workflowType string,
	description string,
	inputSchema []byte,
) sdkcapability.Capability {
	return sdkcapability.Capability{
		ID:             id,
		TemporalType:   workflowType,
		Description:    description,
		InputSchema:    inputSchema,
		OutputSchema:   videoOutputSchema,
		TimeoutSeconds: int64(videoWorkflowTimeout.Seconds()),
		Tool: &sdkcapability.Tool{
			Mode: sdkcapability.ToolModeBlocking,
		},
	}
}
