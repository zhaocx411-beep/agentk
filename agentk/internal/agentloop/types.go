// Package agentloop 编排 Session 的 Temporal Workflow、Activity 和启动入口。
// Session 事件是业务事实源，Temporal History 只保存执行状态和下一步所需的最小引用。
// AgentLoop 与 Pipeline 共用 Session 级 Temporal Workflow ID，同一 Session 至多一个
// Execution，避免两个 Workflow 并发写入同一事件日志。
package agentloop

import (
	"encoding/json"
	"fmt"

	"agentk/internal/event"
	"agentk/internal/tool"
	"agentk/internal/types"
)

const (
	AgentWorkflowType = "agentk.session.agent"

	BeginExecutionActivityName    = "agentk.session.begin_execution"
	FinishExecutionActivityName   = "agentk.session.finish_execution"
	RoundActivityName             = "agentk.session.round"
	ExecuteInlineToolActivityName = "agentk.session.execute_inline_tool"
	// BeginToolActivityName 和 FinishToolActivityName 包围由 workflow 派发的工具执行。
	BeginToolActivityName  = "agentk.session.begin_tool"
	FinishToolActivityName = "agentk.session.finish_tool"
	AskUserActivityName    = "agentk.session.ask_user"
)

// AgentWorkflowInput 包含本次 Agent Loop 的唯一外部输入。Temporal 接受 Workflow
// 启动时会一并持久化 Trigger，首个 Activity 再将其写入 Session 事件日志。
type AgentWorkflowInput struct {
	Execution types.ExecutionRef `json:"execution"`
	Trigger   AgentTrigger       `json:"trigger"`
}

// AgentTrigger 是启动一次 Agent Loop 的输入联合；必须且只能设置一个字段。
type AgentTrigger struct {
	Message *event.UserInput        `json:"message,omitempty"`
	Answer  *event.AskUserResponded `json:"answer,omitempty"`
}

func (t AgentTrigger) Draft() (event.Draft, error) {
	switch {
	case t.Message != nil && t.Answer == nil:
		return event.Draft{Payload: *t.Message}, nil
	case t.Message == nil && t.Answer != nil:
		return event.Draft{Payload: *t.Answer}, nil
	default:
		return event.Draft{}, fmt.Errorf("exactly one of message or answer is required")
	}
}

type AgentWorkflowOutput struct {
	// Rounds 是本次运行完成的模型调用次数。
	Rounds int `json:"rounds"`
}

type BeginExecutionInput struct {
	Execution types.ExecutionRef `json:"execution"`
	Trigger   AgentTrigger       `json:"trigger"`
}

type FinishExecutionInput struct {
	Execution types.ExecutionRef    `json:"execution"`
	Status    types.ExecutionStatus `json:"status"`
	Error     string                `json:"error,omitempty"`
}

// RoundInput 标识一次模型调用。RoundID 由 ExecutionID 和轮号确定性派生，
// replay 与 Activity 重试保持一致，同时用于事件幂等和 delta 关联。
type RoundInput struct {
	Execution types.ExecutionRef `json:"execution"`
	RoundID   event.RoundID      `json:"roundId"`
}

// ToolCallRef 是通过 Workflow History 传递的工具调用引用。
// 大参数不写入 History；ArgsInEventLog 为 true 时，Activity 按 Session、round 和
// tool call ID 从 AssistantToolCall 事件回载参数。
type ToolCallRef struct {
	ToolCallID event.ToolCallID `json:"toolCallId"`
	ToolName   string           `json:"toolName"`
	Args       json.RawMessage  `json:"args,omitempty"`
	// DefaultArgs 与 binding 一同固定在 Workflow History 中。
	DefaultArgs json.RawMessage `json:"defaultArgs,omitempty"`
	// ArgsInEventLog 表示参数未内联到 Workflow History。
	ArgsInEventLog bool `json:"argsInEventLog,omitempty"`
	// Binding 和派发参数由 RoundActivity 固定，Workflow 不读取可能随 Profile 变化的 Registry。
	// 空 Binding 表示未知工具，交给通用链路产生模型可见错误。
	Binding tool.BindingKind `json:"binding,omitempty"`
	// Activity 在 Binding 为 activity 时设置。
	Activity *ActivityDispatch `json:"activity,omitempty"`
	// Workflow 在 Binding 为 workflow 时设置。
	Workflow *WorkflowDispatch `json:"workflow,omitempty"`
}

// ActivityDispatch 是 ActivityBinding 的派发参数；零值字段沿用默认配置。
type ActivityDispatch struct {
	Name string `json:"name"`
	// TaskQueue 为空时沿用 Session workflow 的 task queue。
	TaskQueue string `json:"taskQueue,omitempty"`
	// StartToCloseSeconds 为 0 时沿用默认值。
	StartToCloseSeconds int64 `json:"startToCloseSeconds,omitempty"`
	// MaxAttempts 为 0 时沿用默认值。
	MaxAttempts int `json:"maxAttempts,omitempty"`
}

// WorkflowDispatch 是 WorkflowBinding 的派发参数。
type WorkflowDispatch struct {
	Type string `json:"type"`
	// TaskQueue 为空时沿用 Session workflow 的 task queue。
	TaskQueue string `json:"taskQueue,omitempty"`
	// ExecutionTimeoutSeconds 为 0 时不设置总超时。
	ExecutionTimeoutSeconds int64 `json:"executionTimeoutSeconds,omitempty"`
}

type RoundOutput struct {
	// Idle 表示本轮未调用模型。
	Idle      bool          `json:"idle,omitempty"`
	ToolCalls []ToolCallRef `json:"toolCalls,omitempty"`
	// MaxToolRounds 是连续工具轮上限。
	MaxToolRounds int `json:"maxToolRounds,omitempty"`
}

type ToolCallActivityInput struct {
	Execution types.ExecutionRef `json:"execution"`
	RoundID   event.RoundID      `json:"roundId"`
	// CallIdx 是调用在 round 内的序号，也是事件批幂等键的一部分。
	CallIdx int         `json:"callIdx"`
	Call    ToolCallRef `json:"call"`
}

type ExecuteInlineToolOutput struct{}

// BeginToolOutput 使用 map，避免派发参数被 Temporal data converter 二次编码。
type BeginToolOutput struct {
	Args map[string]any `json:"args"`
}

// FinishToolInput 提交工具结果或错误，两者只能设置一个。
// capability 错误会记录为 ToolExecFailed 供模型处理，而不是直接使 Session Workflow 失败。
type FinishToolInput struct {
	Execution      types.ExecutionRef `json:"execution"`
	RoundID        event.RoundID      `json:"roundId"`
	CallIdx        int                `json:"callIdx"`
	Call           ToolCallRef        `json:"call"`
	Result         event.RawJSON      `json:"result,omitempty"`
	Error          string             `json:"error,omitempty"`
	DurationMillis int64              `json:"durationMillis,omitempty"`
}

type FinishToolOutput struct{}

// AskUserInput 是 UserBinding 的输入。MustBeAlone 拒绝与其他 tool call 混合的提问；
// 拒绝结果作为工具错误返回给模型，不登记等待用户的 interaction。
type AskUserInput struct {
	Execution   types.ExecutionRef `json:"execution"`
	RoundID     event.RoundID      `json:"roundId"`
	CallIdx     int                `json:"callIdx"`
	Call        ToolCallRef        `json:"call"`
	MustBeAlone bool               `json:"mustBeAlone,omitempty"`
}

type AskUserOutput struct {
	// AwaitingUser 表示提问已登记且 AgentWorkflow 应结束；false 时模型可在下一轮修正输入。
	AwaitingUser bool `json:"awaitingUser"`
}
