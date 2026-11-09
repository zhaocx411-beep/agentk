package imagegen

import (
	"context"
)

// 参考图生图（图像编辑）与 generate_image（纯文生图）分开，使用独立的工具名、
// 入参和供应商协议。edit_image 必须带至少一张参考图，模型按参考图和文字指令
// 重绘或编辑，并返回新图 URL。当前实现为 qwenedit、gptimage 与 gemini。

// EditRequest 是不含供应商协议细节的公共参考图生图参数。
type EditRequest struct {
	// Prompt 编辑指令：描述如何在参考图基础上生成新画面
	Prompt string
	// ReferenceImages 参考图 URL 列表（至少一张）。这些是上下文里流转的图片占位符——
	// 通常是先前 generate_image / edit_image 返回的 artifacts 数组元素的 uri，不内嵌图片字节。
	ReferenceImages []string
	// AspectRatio 可选画幅 "1:1" / "9:16" / "16:9"；空则由供应商实现决定（编辑模型
	// 通常按输入图推断尺寸，故留空时不强制 size）。
	AspectRatio string
}

// EditClient 是一个参考图生图模型端点：同步生成，返回图片字节。
type EditClient interface {
	Edit(ctx context.Context, req EditRequest) (Image, error)
}

// EditEntry 是模型目录里的一个参考图生图客户端条目。
type EditEntry struct {
	Name   string
	Client EditClient
	// MaxReferenceImages 该模型接受的参考图数量上限；0 = 不限。capability 按此值校验。
	MaxReferenceImages int
}
