// 资产库的共享投影类型：各 Agent 服务把自己的任务/会话映射成
// "项目 + 资产清单"，platform/app 聚合后经 /v1/assets 提供给前端。
package core

import "time"

// Asset 一条产物。仅展示与下载，无交互语义。
type Asset struct {
	// Kind image / video / audio。
	Kind string `json:"kind"`
	URI  string `json:"uri"`
	// Tool 产出工具名，可空（如 pipeline 清单里的成片）。
	Tool string    `json:"tool,omitempty"`
	At   time.Time `json:"at,omitempty"`
}

// AssetProject 资产库里的一个项目：一个漫剧任务或一个聊天会话。
type AssetProject struct {
	ID string `json:"id"`
	// Kind drama / chat，前端据此显示项目形态。
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// Agent 所属 Agent 的展示名（漫剧工坊 / 图文视频创作 …）。
	Agent     string    `json:"agent"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Assets    []Asset   `json:"assets"`
}
