// Package invocation reports progress from AgentK-invoked Temporal Workflows.
package invocation

import (
	"context"

	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/workflow"
)

// Kind 标识 agent-k 侧发起 invocation 的入口种类。
type Kind string

const (
	KindPipeline Kind = "pipeline"
	KindTool     Kind = "tool"
	// KindDirect 是不经会话、由 API 直接发起的能力调用。它没有事件日志，
	// 因此没有 session 字段，也不能汇报进度。
	KindDirect Kind = "direct"
)

// Meta 是一次 agent-k invocation 的坐标，由 agent-k 在启动时注入 context，
// 经 Temporal header 透传到被调 workflow/activity，业务签名不感知。
// AppID、InvocationID、InvocationKind、InvocationName 始终存在；SessionID、
// Session、CallbackTaskQueue 只在调用运行于会话内、可以向会话汇报进度时
// 同时出现。跨语言契约由 protocol/agentk/invocation/v1/meta.schema.json 固定。
type Meta struct {
	SessionID         int64  `json:"sessionId,omitempty"`
	Session           string `json:"session,omitempty"` // 会话公开 ID，也是推送主题
	AppID             string `json:"appId"`
	InvocationID      string `json:"invocationId"`
	InvocationKind    Kind   `json:"invocationKind"`
	InvocationName    string `json:"invocationName"`
	CallbackTaskQueue string `json:"callbackTaskQueue,omitempty"`
}

// HasSession 报告该 invocation 是否运行于会话内，即是否有进度落点。
func (m Meta) HasSession() bool {
	return m.SessionID > 0 && m.Session != "" && m.CallbackTaskQueue != ""
}

type metaKey struct{}

// MetaHeaderKey 是 header 键，本身就是跨语言契约：非 Go 实现只需按同名键
// 取 JSON payload。
const MetaHeaderKey = "agentk-invocation-meta-v1"

// WithMeta 把 invocation 坐标放进 client 侧 context；MetaPropagator 负责注入 header。
func WithMeta(ctx context.Context, m Meta) context.Context {
	return context.WithValue(ctx, metaKey{}, m)
}

// WithMetaWorkflow 把 invocation 坐标放进 workflow 侧 context，供 workflow 内
// 按每次派发设置不同坐标；MetaPropagator 在派发 child workflow/activity 时注入 header。
func WithMetaWorkflow(ctx workflow.Context, m Meta) workflow.Context {
	return workflow.WithValue(ctx, metaKey{}, m)
}

// MetaFromWorkflow 读取 header 透传进来的 invocation 坐标（App、会话等）。
// 不在 agent-k 调度下运行（直调、测试）时返回 false。
func MetaFromWorkflow(ctx workflow.Context) (Meta, bool) {
	m, ok := ctx.Value(metaKey{}).(Meta)
	return m, ok
}

// MetaFromActivity 读取 header 透传进 activity 的 invocation 坐标。
// 不在 agent-k 调度下运行时返回 false。
func MetaFromActivity(ctx context.Context) (Meta, bool) {
	m, ok := ctx.Value(metaKey{}).(Meta)
	return m, ok
}

// MetaPropagator 把 Meta 沿 Client → Workflow → Child Workflow/Activity
// 全链路透传。context 里没有 meta 时静默跳过，不影响其他 workflow。
type MetaPropagator struct{}

var _ workflow.ContextPropagator = MetaPropagator{}

func (MetaPropagator) Inject(ctx context.Context, w workflow.HeaderWriter) error {
	m, ok := ctx.Value(metaKey{}).(Meta)
	if !ok {
		return nil
	}
	return setMetaHeader(w, m)
}

func (MetaPropagator) InjectFromWorkflow(ctx workflow.Context, w workflow.HeaderWriter) error {
	m, ok := ctx.Value(metaKey{}).(Meta)
	if !ok {
		return nil
	}
	return setMetaHeader(w, m)
}

func (MetaPropagator) Extract(ctx context.Context, r workflow.HeaderReader) (context.Context, error) {
	payload, ok := r.Get(MetaHeaderKey)
	if !ok {
		return ctx, nil
	}
	var m Meta
	if err := converter.GetDefaultDataConverter().FromPayload(payload, &m); err != nil {
		// header 损坏不阻断执行：meta 是记账旁路，业务照常跑。
		return ctx, nil
	}
	return context.WithValue(ctx, metaKey{}, m), nil
}

func (MetaPropagator) ExtractToWorkflow(ctx workflow.Context, r workflow.HeaderReader) (workflow.Context, error) {
	payload, ok := r.Get(MetaHeaderKey)
	if !ok {
		return ctx, nil
	}
	var m Meta
	if err := converter.GetDefaultDataConverter().FromPayload(payload, &m); err != nil {
		return ctx, nil
	}
	return workflow.WithValue(ctx, metaKey{}, m), nil
}

func setMetaHeader(w workflow.HeaderWriter, m Meta) error {
	payload, err := converter.GetDefaultDataConverter().ToPayload(m)
	if err != nil {
		return err
	}
	w.Set(MetaHeaderKey, payload)
	return nil
}
