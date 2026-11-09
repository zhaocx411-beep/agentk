package webfetch

import (
	_ "embed"
	"time"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
)

const (
	ProductDetailCapabilityID = "agentk.fetch_product_detail"
	ProductDetailActivityName = "agentk.capability.fetch_product_detail"
	productActivityTimeout    = 24 * time.Hour
)

//go:embed fetch_product_detail.schema.json
var productDetailInputSchema []byte

//go:embed fetch_product_detail_output.schema.json
var productDetailOutputSchema []byte

func ManifestCapability() sdkcapability.Capability {
	return sdkcapability.Capability{
		ID:             ProductDetailCapabilityID,
		TemporalType:   ProductDetailActivityName,
		Description:    "从淘宝、天猫或 Amazon 商品详情页提取结构化商品信息并导入商品图片。",
		InputSchema:    productDetailInputSchema,
		OutputSchema:   productDetailOutputSchema,
		TimeoutSeconds: int64(productActivityTimeout.Seconds()),
		Tool: &sdkcapability.Tool{
			Description: "从公网商品详情页提取结构化商品信息。" +
				"仅用于用户已提供明确商品详情页 URL 的场景，支持淘宝、天猫和 Amazon；" +
				"返回页面可见的商品字段与受管商品图片。" +
				"不支持商品搜索、列表页、店铺页、登录后页面、购物车或下单。",
			Mode:        sdkcapability.ToolModeBlocking,
			MaxAttempts: 1,
		},
	}
}
