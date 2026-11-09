// Package agent 执行单次模型或工具调用，不负责存储和编排。
// 调用方传入 fold 后的 chat IR，本包只返回待持久化事件，使事件顺序、幂等和事务
// 边界集中由 agentloop Activity 管理。
package agent

import (
	"context"
	"fmt"
	"time"

	"agentk/internal/chat"
	"agentk/internal/event"
	"agentk/internal/tool"
)

const (
	// DefaultMaxToolRounds 是新用户输入之间允许的连续工具轮上限，用于终止模型原地循环。
	DefaultMaxToolRounds = 20
	// DefaultToolTimeout 是可由工具配置覆盖的默认超时。
	DefaultToolTimeout = 60 * time.Second
)

// EffectiveMaxToolRounds 将非正值替换为 DefaultMaxToolRounds。
func EffectiveMaxToolRounds(n int) int {
	if n <= 0 {
		return DefaultMaxToolRounds
	}
	return n
}

// Resolved 是从 Session 固定 Profile 解析出的单次推进配置快照。
type Resolved struct {
	Model    chat.Client
	Registry *tool.Registry
	System   string
	// Version 是解析时使用的 Profile 版本。
	Version   string
	ModelName string
	// Dialect 用于模型消息转换和 reasoning 投影。
	Dialect event.Dialect
	// MaxToolRounds 非正时使用 DefaultMaxToolRounds。
	MaxToolRounds int
}

// Runtime 将固定的 Session Profile 解析为执行配置；引用已不存在的运行时能力时返回错误。
type Runtime interface {
	Resolve(ctx context.Context, profileJSON []byte) (Resolved, error)
}

// StaticRuntime 返回固定配置，用于测试和单配置运行。
type StaticRuntime struct {
	Model         chat.Client
	Registry      *tool.Registry
	System        string
	Version       string
	ModelName     string
	Dialect       event.Dialect
	MaxToolRounds int
}

func (s StaticRuntime) Resolve(context.Context, []byte) (Resolved, error) {
	return Resolved(s), nil
}

// DeltaSink 接收非持久化模型增量；完整内容由 round 事件提供。
type DeltaSink interface {
	Delta(sessionPublicID string, roundID event.RoundID, d chat.Delta)
}

// ModelInvocationKey 从稳定 RoundID 生成模型调用幂等键，同一 Activity 重试保持不变。
func ModelInvocationKey(roundID event.RoundID) string {
	return "round:" + string(roundID)
}

// ToolInvocationKey 是一次工具调用的稳定幂等键。
func ToolInvocationKey(roundID event.RoundID, toolCallID event.ToolCallID) string {
	return fmt.Sprintf("round:%s:tool:%s", roundID, toolCallID)
}
