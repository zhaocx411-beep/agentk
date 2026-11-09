// Package tool 定义工具契约与注册表，并提供内置工具的构造。
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"agentk/internal/event"
)

// ArtifactInfo 是 list_artifacts 返回给模型的资源视图。URL 是可跨工具传递的资源句柄。
type ArtifactInfo struct {
	// Type 是 image、video 或 audio 等资源类型。
	Type string `json:"type"`
	// URL 是可传给其他工具的资源位置。
	URL string `json:"url"`
	// Description 是产出工具提供的资源说明。
	Description string `json:"description,omitempty"`
	// Tool 是产出资源的工具名。
	Tool string `json:"tool,omitempty"`
	// Model 是产出时绑定的模型目录条目名，用于解释和复现生成结果。
	Model     string `json:"model,omitempty"`
	CreatedAt string `json:"createdAt"`
	// Input 是原始 tool call 参数，用于辨认或修改后重新生成资源。
	Input json.RawMessage `json:"input,omitempty"`
	// Params 是工具实际生效的参数，与 Input 分开保存。
	Params json.RawMessage `json:"params,omitempty"`
}

// Context 是一次工具执行的上下文。
type Context struct {
	SessionID  string
	ToolCallID string
	// IdempotencyKey 是逻辑工具调用的稳定键。
	IdempotencyKey string
	// ListArtifacts 按产出顺序返回 Session 资源；无 Session context 时为 nil。
	ListArtifacts func() ([]ArtifactInfo, error)
}

// ResultArtifact 是产物类工具固定 result.artifacts envelope 的元素。
type ResultArtifact struct {
	Type        string         `json:"type"`
	URI         string         `json:"uri"`
	Description string         `json:"description,omitempty"`
	Model       string         `json:"model,omitempty"`
	Params      map[string]any `json:"params,omitempty"`
}

// ExtractResultArtifacts 从固定的 result.artifacts envelope 读取有效资源。
func ExtractResultArtifacts(result json.RawMessage) []event.Artifact {
	var envelope struct {
		Artifacts []struct {
			Type        string          `json:"type"`
			URI         string          `json:"uri"`
			Description string          `json:"description,omitempty"`
			Model       string          `json:"model,omitempty"`
			Params      json.RawMessage `json:"params,omitempty"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(result, &envelope); err != nil {
		return nil
	}
	artifacts := make([]event.Artifact, 0, len(envelope.Artifacts))
	for _, artifact := range envelope.Artifacts {
		if strings.TrimSpace(artifact.Type) == "" || strings.TrimSpace(artifact.URI) == "" {
			continue
		}
		artifacts = append(artifacts, event.Artifact{
			Type:        artifact.Type,
			URI:         artifact.URI,
			Description: artifact.Description,
			Model:       artifact.Model,
			Params:      event.RawJSON(artifact.Params),
		})
	}
	if len(artifacts) == 0 {
		return nil
	}
	return artifacts
}

// HistoryTrim 定义工具结果在后续模型调用中的截断规则。
// 产生结果的当前调用仍看到全文；截断只作用于之后的历史投影，不修改持久化事件。
type HistoryTrim struct {
	// Field 是需要截断的顶层字符串字段；其他定位字段保持不变。
	Field string
	// MaxChars 是投影时保留的字符数。
	MaxChars int
}

// Spec 由两部分组成：模型可见的工具契约，以及决定运行方式的 Binding。
// Binding 使用封闭联合，因为进程内执行、Temporal Activity、child Workflow 和
// 用户应答具有不同的生命周期，不能由一个统一行为接口准确表达。
type Spec struct {
	Name        string
	Description string
	// InputSchema 是提供给模型和配置校验使用的 JSON Schema。
	InputSchema json.RawMessage
	// DefaultArgs 是 Profile 配置的顶层默认参数，模型参数优先。
	DefaultArgs json.RawMessage
	// HistoryTrim 为 nil 时不截断历史中的工具结果。
	HistoryTrim *HistoryTrim
	// Timeout 为零时使用执行器默认值。
	Timeout time.Duration
	// Binding 是必填的执行方式。
	Binding Binding
}

// Binding 是工具执行方式的封闭联合，不提供统一执行方法。
type Binding interface{ isBinding() }

// InlineBinding 在当前 Activity 进程内执行并立即返回结果。
type InlineBinding struct {
	Execute func(ctx context.Context, input json.RawMessage, tc Context) (any, error)
}

// UserBinding 的结果由用户输入提供。AgentWorkflow 登记问题后结束，不保持等待中的
// Workflow；后续用户回答事件完成该次调用并启动新的推进。
type UserBinding struct{}

// ActivityBinding 将原始 LLM 参数派发到指定 Temporal Activity。
// Activity 是可由 Pipeline 或其他 Workflow 直接调用的稳定 capability ABI；工具事件的
// Begin/Finish 由 Agent 编排层写入事件日志，执行体本身不重复记录。
type ActivityBinding struct {
	// ActivityName 写入 Workflow History，不能在已有运行完成前改名。
	ActivityName string
	// TaskQueue 为空时沿用 Session workflow 的 task queue。
	TaskQueue string
	// StartToCloseTimeout 为零时使用默认值。
	StartToCloseTimeout time.Duration
	// MaxAttempts 为零时使用默认值；有外部成本的执行可显式降低重试次数。
	MaxAttempts int
}

// WorkflowBinding 将原始 LLM 参数派发到指定 Temporal child Workflow，
// AgentWorkflow 等待 child 到终态后闭合原 tool call。
type WorkflowBinding struct {
	// WorkflowType 写入 Workflow History，不能在已有运行完成前改名。
	WorkflowType string
	// TaskQueue 为空时沿用 Session workflow 的 task queue。
	TaskQueue string
	// ExecutionTimeout 为零时不设置总超时。
	ExecutionTimeout time.Duration
}

func (InlineBinding) isBinding()   {}
func (UserBinding) isBinding()     {}
func (ActivityBinding) isBinding() {}
func (WorkflowBinding) isBinding() {}

// BindingKind 是写入 Workflow History 的路由标签。
// Workflow 不持有 Registry，因此装配期的 Binding 必须先转换为可序列化决策。
type BindingKind string

const (
	BindInline   BindingKind = "inline"
	BindUser     BindingKind = "user"
	BindActivity BindingKind = "activity"
	BindWorkflow BindingKind = "workflow"
)

// BindingKind 返回绑定的类型标签；未绑定返回空（registry 校验保证不会出现）。
func (s Spec) BindingKind() BindingKind {
	switch s.Binding.(type) {
	case InlineBinding:
		return BindInline
	case UserBinding:
		return BindUser
	case ActivityBinding:
		return BindActivity
	case WorkflowBinding:
		return BindWorkflow
	}
	return ""
}

// Run 只执行 InlineBinding；其他 Binding 到达这里表示 Workflow 路由错误。
func (s Spec) Run(ctx context.Context, input json.RawMessage, tc Context) (any, error) {
	switch b := s.Binding.(type) {
	case InlineBinding:
		return b.Execute(ctx, input, tc)
	case ActivityBinding:
		return nil, fmt.Errorf("tool %q: executes as capability activity %q, not in-process (workflow routing bug)", s.Name, b.ActivityName)
	case WorkflowBinding:
		return nil, fmt.Errorf("tool %q: executes as workflow %q, not in-process (workflow routing bug)", s.Name, b.WorkflowType)
	case UserBinding:
		return nil, fmt.Errorf("tool %q: result comes from the user, not executable (workflow routing bug)", s.Name)
	}
	return nil, fmt.Errorf("tool %q: no execution binding", s.Name)
}
