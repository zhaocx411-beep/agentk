package textgen

import (
	"time"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
)

// ManifestCapability 描述可由 Pipeline 或外部 Workflow 直接调用的文本生成 ABI。
func ManifestCapability() sdkcapability.Capability {
	return sdkcapability.Capability{
		ID:             "agentk.generate_text",
		TemporalType:   ActivityName,
		Description:    "使用指定模型生成文本或符合 JSON Schema 的结构化结果。",
		InputSchema:    sdkcapability.SchemaOf[Input](),
		OutputSchema:   sdkcapability.SchemaOf[Output](),
		TimeoutSeconds: int64((10 * time.Minute).Seconds()),
	}
}
