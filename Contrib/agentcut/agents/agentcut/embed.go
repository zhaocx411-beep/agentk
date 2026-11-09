// Package agentcutprofile 编译期内嵌 AgentCut 的 Agent 定义。
// cut-apiserver 在创建项目会话时以 ProfileJSON 为模板，注入项目级 toolDefaults。
package agentcutprofile

import _ "embed"

const (
	ProfileID          = "agentcut"
	ProfileDisplayName = "剪辑 Agent"
	ProfileDescription = "对话式视频剪辑：直接编辑项目时间线，支持文本剪视频、自动字幕、AI 配音与多音轨混音。"
)

//go:embed profile.json
var ProfileJSON []byte

//go:embed meta.json
var MetaJSON []byte
