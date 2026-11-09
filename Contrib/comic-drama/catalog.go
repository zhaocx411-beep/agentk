// Package studio 汇集仓库内所有 Agent 的产品定义。
//
// agents/ 下每个目录就是一个产品：meta.json 描述展示元数据（名称、
// 简介、集市卡片、聊天欢迎语等），profile*.json 是提交给 AgentK 的
// Profile 原文。本包在编译期把它们打进二进制，供 platform/chat 的
// 注册表与 cmd/apiserver 的装配代码使用；除标准库外无任何依赖。
package studio

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

//go:embed agents/*/meta.json agents/*/profile*.json
var catalogFS embed.FS

// agentOrder 决定 /v1/agents 与集市页的展示顺序。在 agents/ 下新增
// Agent 目录后在这里登记，否则 Agents() 会报错提醒。
var agentOrder = []string{
	"comic-drama",
	// assistant 是 converse 的统一对话入口（固定 system prompt + call_agent
	// 路由到各子 agent，含漫剧）。不是一个供用户挑选的「全能助手」产品。
	"assistant",
	"creative-studio",
	"media-creator",
	"web-research",
	"document-research",
	"video-understanding",
	"audio",
	"video-editor",
}

// AgentMeta 对应各 Agent 目录下的 meta.json。
type AgentMeta struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	// Kind 是产品形态：chat（通用聊天页）或 workshop（独立工坊页）。
	Kind   string `json:"kind"`
	Href   string `json:"href"`
	Status string `json:"status"`
	// Visual / VisualLabel 供集市卡片使用。
	Visual      string `json:"visual"`
	VisualLabel string `json:"visualLabel"`
	// 以下字段仅 chat 形态使用。
	Icon           string   `json:"icon"`
	Accent         string   `json:"accent"`
	WelcomeMessage string   `json:"welcomeMessage"`
	StarterPrompts []string `json:"starterPrompts"`
}

// Agent 是一个目录的完整定义：元数据加全部 Profile 原文。
type Agent struct {
	Meta AgentMeta
	// Profiles 键为文件名去掉 .json 后缀（如 profile、profile-agent），
	// 值为校验过的 JSON 对象原文。
	Profiles map[string]json.RawMessage
}

// Agents 按 agentOrder 返回全部 Agent 定义。目录缺 meta.json、meta 的
// id 与目录名不一致、或 profile 不是 JSON 对象都判定为打包错误。
func Agents() ([]Agent, error) {
	result := make([]Agent, 0, len(agentOrder))
	for _, id := range agentOrder {
		rawMeta, err := catalogFS.ReadFile(path.Join("agents", id, "meta.json"))
		if err != nil {
			return nil, fmt.Errorf("agent %s: read meta.json: %w", id, err)
		}
		var meta AgentMeta
		if err := json.Unmarshal(rawMeta, &meta); err != nil {
			return nil, fmt.Errorf("agent %s: decode meta.json: %w", id, err)
		}
		if meta.ID != id {
			return nil, fmt.Errorf("agent %s: meta.json id 为 %q，须与目录名一致", id, meta.ID)
		}
		profilePaths, err := fs.Glob(catalogFS, path.Join("agents", id, "profile*.json"))
		if err != nil {
			return nil, fmt.Errorf("agent %s: glob profiles: %w", id, err)
		}
		profiles := make(map[string]json.RawMessage, len(profilePaths))
		for _, profilePath := range profilePaths {
			raw, err := catalogFS.ReadFile(profilePath)
			if err != nil {
				return nil, fmt.Errorf("agent %s: read %s: %w", id, profilePath, err)
			}
			var object map[string]json.RawMessage
			if err := json.Unmarshal(raw, &object); err != nil || object == nil {
				if err == nil {
					err = fmt.Errorf("profile must be a JSON object")
				}
				return nil, fmt.Errorf("agent %s: decode %s: %w", id, profilePath, err)
			}
			name := strings.TrimSuffix(path.Base(profilePath), ".json")
			profiles[name] = json.RawMessage(raw)
		}
		if len(profiles) == 0 {
			return nil, fmt.Errorf("agent %s: 目录下没有 profile*.json", id)
		}
		result = append(result, Agent{Meta: meta, Profiles: profiles})
	}
	return result, nil
}

// MustAgents 是 Agents 的 panic 版本，供进程启动路径使用：目录内容在
// 编译期固定，出错只能是打包缺陷，快速失败即可。
func MustAgents() []Agent {
	agents, err := Agents()
	if err != nil {
		panic(err)
	}
	return agents
}

// Find 按 ID 查找 Agent 定义。
func Find(id string) (Agent, bool) {
	for _, agent := range MustAgents() {
		if agent.Meta.ID == id {
			return agent, true
		}
	}
	return Agent{}, false
}
