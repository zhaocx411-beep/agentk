package chat

import (
	"encoding/json"
	"fmt"

	"studio"
	"studio/platform/core"
)

// AgentDefinition 是服务端固定的 Agent 产品定义，来自各 Agent 目录的
// meta.json 与 profile.json。Profile 永远不序列化给前端：浏览器只拿
// 展示元数据，并用 ID 请求创建会话。
type AgentDefinition struct {
	ID          string
	Name        string
	Description string
	Category    string
	Tags        []string
	// Kind 产品形态：chat（通用聊天页）或 workshop（独立工坊页，如漫剧）。
	Kind   string
	Href   string
	Status string
	// Visual / VisualLabel 集市卡片视觉。
	Visual      string
	VisualLabel string
	// 以下仅 chat 形态使用。
	Icon           string
	Accent         string
	WelcomeMessage string
	StarterPrompts []string
	ProfileVersion string
	// Profile 仅 chat 形态非空；workshop 的 profile 由对应服务自行管理。
	Profile json.RawMessage
}

type agentDTO struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Category       string   `json:"category"`
	Tags           []string `json:"tags,omitempty"`
	Kind           string   `json:"kind"`
	Href           string   `json:"href"`
	Status         string   `json:"status,omitempty"`
	Visual         string   `json:"visual,omitempty"`
	VisualLabel    string   `json:"visualLabel,omitempty"`
	Icon           string   `json:"icon,omitempty"`
	Accent         string   `json:"accent,omitempty"`
	WelcomeMessage string   `json:"welcomeMessage,omitempty"`
	StarterPrompts []string `json:"starterPrompts,omitempty"`
}

type AgentRegistry struct {
	order []string
	items map[string]AgentDefinition
}

// NewRegistry 从 studio 目录构建注册表。全部条目都进入 PublicList
// （集市页需要 workshop 卡片），但只有 chat 形态能通过 Get 创建会话。
func NewRegistry(agents []studio.Agent) (AgentRegistry, error) {
	registry := AgentRegistry{
		order: make([]string, 0, len(agents)),
		items: make(map[string]AgentDefinition, len(agents)),
	}
	for _, agent := range agents {
		meta := agent.Meta
		if _, duplicate := registry.items[meta.ID]; duplicate {
			return AgentRegistry{}, fmt.Errorf("duplicate agent id %q", meta.ID)
		}
		definition := AgentDefinition{
			ID:             meta.ID,
			Name:           meta.Name,
			Description:    meta.Description,
			Category:       meta.Category,
			Tags:           append([]string(nil), meta.Tags...),
			Kind:           meta.Kind,
			Href:           meta.Href,
			Status:         meta.Status,
			Visual:         meta.Visual,
			VisualLabel:    meta.VisualLabel,
			Icon:           meta.Icon,
			Accent:         meta.Accent,
			WelcomeMessage: meta.WelcomeMessage,
			StarterPrompts: append([]string(nil), meta.StarterPrompts...),
		}
		if meta.Kind == "chat" {
			profile, ok := agent.Profiles["profile"]
			if !ok {
				return AgentRegistry{}, fmt.Errorf(
					"chat agent %s: 缺少 profile.json", meta.ID,
				)
			}
			if _, err := core.DecodeAgentKProfile(profile); err != nil {
				return AgentRegistry{}, fmt.Errorf(
					"chat agent %s: profile.json: %w", meta.ID, err,
				)
			}
			definition.Profile = append(json.RawMessage(nil), profile...)
			definition.ProfileVersion = "agent-k/archive-demo-ui-c121beb-compat-v1/" + meta.ID
		}
		if meta.ID == "comic-drama" {
			// 漫剧工坊页仍走 /v1/dramas；作为 converse 的子 agent 时使用
			// agent 模式 profile，由 call_agent 开子会话驱动。
			profile, ok := agent.Profiles["profile-agent"]
			if !ok {
				return AgentRegistry{}, fmt.Errorf(
					"comic-drama: 缺少 profile-agent.json",
				)
			}
			if _, err := core.DecodeAgentKProfile(profile); err != nil {
				return AgentRegistry{}, fmt.Errorf(
					"comic-drama: profile-agent.json: %w", err,
				)
			}
			definition.Profile = append(json.RawMessage(nil), profile...)
		}
		registry.order = append(registry.order, meta.ID)
		registry.items[meta.ID] = definition
	}
	return registry, nil
}

// DefaultRegistry 从内嵌的 studio 目录构建注册表；目录在编译期固定，
// 出错只能是打包缺陷，直接 panic。
func DefaultRegistry() AgentRegistry {
	registry, err := NewRegistry(studio.MustAgents())
	if err != nil {
		panic(err)
	}
	return registry
}

// SubAgent 返回可被 call_agent 调度的子 agent（含漫剧）。对话入口
// assistant 自己不能作为子 agent，避免递归调度。
func (r AgentRegistry) SubAgent(id string) (AgentDefinition, bool) {
	if id == "" || id == "assistant" {
		return AgentDefinition{}, false
	}
	definition, ok := r.items[id]
	if !ok || len(definition.Profile) == 0 {
		return AgentDefinition{}, false
	}
	definition.Profile = append(json.RawMessage(nil), definition.Profile...)
	definition.StarterPrompts = append([]string(nil), definition.StarterPrompts...)
	return definition, true
}

// Get 返回可创建会话的 chat 形态 Agent；workshop 形态返回 false。
func (r AgentRegistry) Get(id string) (AgentDefinition, bool) {
	definition, ok := r.items[id]
	if !ok || definition.Kind != "chat" {
		return AgentDefinition{}, false
	}
	definition.Profile = append(json.RawMessage(nil), definition.Profile...)
	definition.StarterPrompts = append([]string(nil), definition.StarterPrompts...)
	return definition, true
}

// NameOf 返回 Agent 的展示名；未知 ID 原样返回（资产库标注用）。
func (r AgentRegistry) NameOf(id string) string {
	if definition, ok := r.items[id]; ok {
		return definition.Name
	}
	return id
}

// Has 判断 ID 是否存在（含 workshop 形态；前端路由校验用）。
func (r AgentRegistry) Has(id string) bool {
	_, ok := r.items[id]
	return ok
}

// PublicList 返回全部条目的展示元数据（不含 Profile）。
func (r AgentRegistry) PublicList() []agentDTO {
	result := make([]agentDTO, 0, len(r.order))
	for _, id := range r.order {
		definition := r.items[id]
		result = append(result, agentDTO{
			ID:             definition.ID,
			Name:           definition.Name,
			Description:    definition.Description,
			Category:       definition.Category,
			Tags:           append([]string(nil), definition.Tags...),
			Kind:           definition.Kind,
			Href:           definition.Href,
			Status:         definition.Status,
			Visual:         definition.Visual,
			VisualLabel:    definition.VisualLabel,
			Icon:           definition.Icon,
			Accent:         definition.Accent,
			WelcomeMessage: definition.WelcomeMessage,
			StarterPrompts: append([]string(nil), definition.StarterPrompts...),
		})
	}
	return result
}
