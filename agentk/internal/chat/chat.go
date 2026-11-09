// Package chat 定义模型调用使用的 provider-neutral IR。
// Session 事件由 fold 转换为该 IR，再由具体 provider adapter 转换为 SDK 参数；
// 新增 provider 不应改变 fold 或 Agent 循环。
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message 是对话历史中的一条消息。字段按 Role 取子集：
//
//   - user：Text
//   - assistant：Text / Reasoning / ToolCalls
//   - tool：Text（工具结果的 JSON 文本）+ ToolCallID
type Message struct {
	Role Role
	Text string
	// Reasoning 仅在目标模型和 Dialect 与产出方一致时设置，由 adapter 决定能否回传。
	Reasoning  string
	ToolCalls  []ToolCall
	ToolCallID string
}

// ToolCall 是模型发起的一次工具调用。
type ToolCall struct {
	ID   string
	Name string
	// Args 保留模型返回的原始参数，即使它不符合 schema 或不是合法 JSON；错误由工具层处理。
	Args json.RawMessage
}

// ToolDef 是交给模型的工具声明。
type ToolDef struct {
	Name        string
	Description string
	// InputSchema 为空表示工具没有参数。
	InputSchema json.RawMessage
}

// Request 是一次模型调用；具体模型由 Client 实例确定。
type Request struct {
	System   string
	Messages []Message
	Tools    []ToolDef
	// IdempotencyKey 透传给 provider 或 gateway；同一 Temporal Activity 的重试保持不变。
	IdempotencyKey string
}

// BlockKind 是 assistant 产出块的类型。
type BlockKind string

const (
	BlockText      BlockKind = "text"
	BlockReasoning BlockKind = "reasoning"
)

// Block 是按模型产出顺序排列的一段 assistant 内容。顺序与 BlockIndex 一起用于将
// 流式增量和最终持久化内容对齐。
type Block struct {
	Kind BlockKind
	Text string
}

// Delta 是尽力而为的流式增量；Block 对应 Result.Blocks 中的下标。
// 丢失的增量不重放，完整内容最终由该 round 的持久化事件提供。
type Delta struct {
	Block int
	Kind  BlockKind
	Text  string
}

type Usage struct {
	InputTokens     int
	OutputTokens    int
	CacheReadTokens int
}

// Result 是一次调用的完整产出。
type Result struct {
	// Blocks 按产出顺序排列。
	Blocks    []Block
	ToolCalls []ToolCall
	Usage     Usage
	// FinishReason 保留 provider 返回的原始值。
	FinishReason string
}

// Response 先提供增量 Stream，完整 Result 在 Stream 消费完成后可用。
// usage 和 finish reason 通常只在流末尾出现，而调用方需要即时转发前面的增量。
type Response struct {
	Stream iter.Seq2[Delta, error]
	// Result 只在 Stream 消费完之后有效；提前调用返回错误。
	Result func() (Result, error)
}

// Client 是单个模型的流式调用入口。
type Client interface {
	Stream(ctx context.Context, req Request) (Response, error)
}

// StatusError 统一暴露上游 HTTP 状态码，使 Temporal 重试分类不依赖 provider SDK 类型。
type StatusError struct {
	Code    int
	Message string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("chat: status %d: %s", e.Code, e.Message)
}

func (e *StatusError) StatusCode() int { return e.Code }
