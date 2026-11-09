// Package event 定义持久化的 Session 事件及其 JSON 格式。
// Session 事件是业务事实源；模型历史和 Pipeline 快照均由事件投影，Temporal
// 只保存执行状态。存储 payload 与对外 wire 共用同一 codec。
// 已有 kind 的字段和语义属于兼容性契约，由 spec/fixtures/events 中只增不改的
// fixture 校验；新增 kind 可以兼容，修改既有 kind 语义不兼容。
package event

import (
	"time"

	"github.com/google/uuid"

	"agentk/internal/types"
)

type EventID string

func NewEventID() EventID {
	id, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return EventID("evt_" + id.String())
}

// RoundID 标识一次模型调用，并用于关联流式 delta、持久化内容和产出模型。
type RoundID string

// ToolCallID 关联工具调用、执行结果、资源和用户回答，不依赖事件邻接关系。
type ToolCallID string

// BatchID 标识一次幂等追加的事件批；Activity 重试复用该值以读取首次提交结果。
type BatchID string

// BlockIndex 是一个 round 内 assistant 内容块从 0 开始的序号；delta 和最终事件
// 使用同一坐标，使客户端可以归并流式预览与持久化结果。
type BlockIndex int

// Event 是 append-only 的已持久化事件。ID 与 CreatedAt 由存储分配。
type Event struct {
	ID          EventID
	ExecutionID types.ExecutionID
	// RoundID 为空表示事件不属于模型调用。
	RoundID   RoundID
	Payload   Payload
	CreatedAt time.Time
}

// Draft 是尚未分配 ID 和 CreatedAt 的事件；调用方通过 BatchID 将一组 Draft
// 作为同一事务幂等追加。
type Draft struct {
	RoundID RoundID
	Payload Payload
}

// Kind 是由 Payload 具体类型确定的事件类型标签。
type Kind string

const (
	KindUserInput          Kind = "user.input"
	KindRoundStarted       Kind = "round.started"
	KindRoundCompleted     Kind = "round.completed"
	KindAssistantText      Kind = "assistant.text"
	KindAssistantReasoning Kind = "assistant.reasoning"
	KindAssistantToolCall  Kind = "assistant.tool_call"
	KindToolExecStarted    Kind = "tool.exec_started"
	KindToolExecSucceeded  Kind = "tool.exec_succeeded"
	KindToolExecFailed     Kind = "tool.exec_failed"
	KindAskUserRequested   Kind = "ask_user.requested"
	KindAskUserResponded   Kind = "ask_user.responded"
	KindExecutionStarted   Kind = "execution.started"
	KindInvocationProgress Kind = "invocation.progress"
	KindExecutionFinished  Kind = "execution.finished"
)

// AllKinds 包含所有支持的事件类型。
var AllKinds = []Kind{
	KindUserInput,
	KindRoundStarted,
	KindRoundCompleted,
	KindAssistantText,
	KindAssistantReasoning,
	KindAssistantToolCall,
	KindToolExecStarted,
	KindToolExecSucceeded,
	KindToolExecFailed,
	KindAskUserRequested,
	KindAskUserResponded,
	KindExecutionStarted,
	KindInvocationProgress,
	KindExecutionFinished,
}

// Payload 是事件载荷的封闭联合。新增类型必须同时添加解码分支和 fixture。
//
//sumtype:decl
type Payload interface {
	Kind() Kind
	isPayload()
}
