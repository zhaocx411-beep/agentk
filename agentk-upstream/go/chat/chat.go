// Package chat 定义对话模型共用的请求、流式增量与结果。
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Dialect string

const (
	DialectOpenAI          Dialect = "openai"
	DialectOpenAIResponses Dialect = "openai-responses"
	DialectGemini          Dialect = "gemini"
	DialectAnthropic       Dialect = "anthropic"
)

type MediaRef struct {
	URI      string
	MIMEType string
	Data     []byte
	// Bytes 是媒体探测得到的大小，仅用于诊断。
	Bytes int64
}

type MediaResolver interface {
	Resolve(ctx context.Context, ref MediaRef) (MediaRef, error)
}

type Message struct {
	Role  Role
	Text  string
	Media []MediaRef
	// Reasoning 仅在目标模型和 Dialect 与产出方一致时设置，由 adapter 决定能否回传。
	Reasoning  string
	Blocks     []Block
	ToolCalls  []ToolCall
	ToolCallID string
}

type ToolCall struct {
	ID         string
	ProviderID string
	Name       string
	// Args 保留原始参数，可能不是合法 JSON。
	Args          json.RawMessage
	ProtocolState string
}

type ToolDef struct {
	Name        string
	Description string
	// InputSchema 为空表示工具没有参数。
	InputSchema json.RawMessage
}

type Request struct {
	Model    string
	System   string
	Messages []Message
	Tools    []ToolDef
	// OutputSchema requests JSON matching this schema; unsupported adapters reject it.
	OutputSchema json.RawMessage
	// IdempotencyKey 在同一次调用的重试中应保持不变。
	IdempotencyKey string
}

type BlockKind string

const (
	BlockText      BlockKind = "text"
	BlockReasoning BlockKind = "reasoning"
)

// Block 按模型产出顺序排列。
type Block struct {
	Kind          BlockKind
	Text          string
	ProtocolState string
}

// Delta.Block 对应 Result.Blocks 的下标。
type Delta struct {
	Block int
	Kind  BlockKind
	Text  string
}

type Usage struct {
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
}

type Result struct {
	// Blocks 按产出顺序排列。
	Blocks    []Block
	ToolCalls []ToolCall
	Usage     Usage
	// FinishReason 保留 provider 返回的原始值。
	FinishReason string
}

type Response struct {
	Stream iter.Seq2[Delta, error]
	// Result 只在 Stream 消费完之后有效；提前调用返回错误。
	Result func() (Result, error)
}

// Stream、返回的迭代器及 Result 可能返回包装 ErrRejected 的错误。
type Client interface {
	Stream(ctx context.Context, req Request) (Response, error)
}

// ErrRejected 表示上游拒绝请求，原样重试无效。
var ErrRejected = errors.New("chat: upstream rejected the request")
