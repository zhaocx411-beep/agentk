package timeline

// 规模上限（后端详细设计 5.4 节）。超限的 Ops 整批拒绝。
const (
	MaxMedia              = 500
	MaxTracks             = 64
	MaxItems              = 5000
	MaxTransitions        = 5000
	MaxTransitionIDLength = 128
	MaxOpsPerBatch        = 200
	// MaxDocBytes 约束序列化后的文档体积，需早于 Temporal payload 限制在应用层拦截。
	MaxDocBytes = 4 << 20 // 4 MiB
)

// AllowedFPS 是文档标准帧率白名单。
var AllowedFPS = map[int]bool{24: true, 25: true, 30: true, 50: true, 60: true}

// DefaultFPS 是未指定时的默认帧率。
const DefaultFPS = 30
