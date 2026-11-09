// Package pipeline 将 Session Profile 中的公开 Pipeline 名称绑定到 Temporal 实现。
// 所有执行先进入统一 container，由 container 记录 AgentK 生命周期。具体实现的业务
// payload 只包含调用方参数；Session、tenant 和 exec 坐标通过 invocation Meta 单独传播，
// 不混入跨语言业务 ABI。
package pipeline

import (
	"context"
	"encoding/json"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/workflow"

	"agentk/internal/event"
	"agentk/internal/temporalx"
	"agentk/internal/types"
)

const (
	BeginExecutionActivityName  = "agentk.pipeline.begin_execution"
	FinishExecutionActivityName = "agentk.pipeline.finish_execution"
)

// WorkflowInput 包含 container 所需的完整参数；Target 写入 Workflow History 后不再
// 受 Profile 或 capability catalog 后续变化影响。
type WorkflowInput struct {
	Execution    types.ExecutionRef `json:"execution"`
	Name         string             `json:"name"`
	CapabilityID string             `json:"capabilityId"`
	Args         json.RawMessage    `json:"args,omitempty"`
	Target       Target             `json:"target"`
}

type FinishExecutionInput struct {
	Execution types.ExecutionRef    `json:"execution"`
	Status    types.ExecutionStatus `json:"status"`
	Output    json.RawMessage       `json:"output,omitempty"`
	Error     string                `json:"error,omitempty"`
}

// Register 注册 Pipeline container 和生命周期 Activity；第一方和外部实现由各自
// capability worker 注册，container 只依赖稳定 Temporal ABI。
func Register(w temporalx.Registry, a *Activities) {
	w.RegisterWorkflowWithOptions(ExecWorkflow,
		workflow.RegisterOptions{Name: ExecWorkflowType})
	w.RegisterActivityWithOptions(a.BeginExecutionActivity,
		activity.RegisterOptions{Name: BeginExecutionActivityName})
	w.RegisterActivityWithOptions(a.FinishExecutionActivity,
		activity.RegisterOptions{Name: FinishExecutionActivityName})
}

// Store 是 pipeline 对 Session 和事件存储的依赖；执行状态不写入单独的 exec 表。
type Store interface {
	GetSession(ctx context.Context, id int64) (types.Session, error)
	AppendEvents(ctx context.Context, sessionID int64, executionID types.ExecutionID, batchID event.BatchID, drafts []event.Draft) ([]event.Event, error)
}
