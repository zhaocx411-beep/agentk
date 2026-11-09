// Package types 是跨层共享的存储侧类型：会话、pipeline 执行读模型与错误哨兵。
//
// 会话历史不在这里——它是事件流（internal/event），由 fold 投影成模型看的形状。
package types

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Session 是一条会话（ak_sessions 一行），事件日志的宿主。
type Session struct {
	ID       int64
	PublicID string
	TenantID string
	Title    string
	// Profile 是创建时已经校验并固定的规范 JSON；永非空且不含凭证。
	Profile   []byte
	CreatedAt time.Time
}

// SessionRef 是 Temporal Workflow/Activity 定位 Session 所需的稳定引用。
type SessionRef struct {
	SessionID       int64  `json:"sessionId"`
	SessionPublicID string `json:"sessionPublicId"`
}

type ExecutionID string

func NewExecutionID() ExecutionID {
	id, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return ExecutionID("exe_" + id.String())
}

type ExecutionRef struct {
	ID      ExecutionID `json:"executionId"`
	Session SessionRef  `json:"session"`
}

type ExecutionKind string

const (
	ExecutionKindAgentLoop ExecutionKind = "agent_loop"
	ExecutionKindPipeline  ExecutionKind = "pipeline"
)

type ExecutionStatus string

const (
	ExecutionStatusRunning   ExecutionStatus = "running"
	ExecutionStatusCompleted ExecutionStatus = "completed"
	ExecutionStatusFailed    ExecutionStatus = "failed"
	ExecutionStatusCancelled ExecutionStatus = "cancelled"
)

type PipelineExecution struct {
	Name         string
	CapabilityID string
	Args         json.RawMessage
	Output       json.RawMessage
}

// Execution 是 Session 中一次 Agent Loop 或 Pipeline 运行的事件投影。
type Execution struct {
	ID              ExecutionID
	SessionID       int64
	SessionPublicID string
	TenantID        string
	Kind            ExecutionKind
	Status          ExecutionStatus
	ProfileVersion  string
	Pipeline        *PipelineExecution
	Error           string
	CreatedAt       time.Time
	FinishedAt      *time.Time
}
