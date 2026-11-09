package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/invocation"

	"agentk/internal/capcatalog"
	"agentk/internal/execution"
	"agentk/internal/types"
)

// StarterConfig 是 Starter 的依赖。
type StarterConfig struct {
	Temporal  execution.StartClient
	TaskQueue string
	Catalog   capcatalog.Catalog
}

// Starter 是 Pipeline 发现和启动的统一入口，始终启动 AgentK container 而非直接
// 调用 capability 实现。
type Starter struct {
	temporal  execution.StartClient
	taskQueue string
	catalog   capcatalog.Catalog
}

func NewStarter(cfg StarterConfig) *Starter {
	return &Starter{
		temporal: cfg.Temporal, taskQueue: cfg.TaskQueue,
		catalog: cfg.Catalog,
	}
}

// List 惰性解析 Profile 中的 Pipeline；Catalog 中已不存在的单项被省略，Catalog
// 整体读取失败则返回错误。
func (s *Starter) List(ctx context.Context, sess types.Session) ([]ListItem, error) {
	entries := append([]ProfileEntry(nil), Entries(sess.Profile)...)
	slices.SortFunc(entries, func(a, b ProfileEntry) int {
		return strings.Compare(a.Name, b.Name)
	})
	out := make([]ListItem, 0, len(entries))
	for _, entry := range entries {
		resolved, err := resolveEntry(ctx, s.catalog, entry)
		if errors.Is(err, capcatalog.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%w: resolve %q: %v", ErrUnavailable, entry.Name, err)
		}
		out = append(out, resolved.item)
	}
	return out, nil
}

// Start 解析 Pipeline 路由和参数契约，并在共享 Session execution slot 中启动 container。
func (s *Starter) Start(
	ctx context.Context, sess types.Session, name string, args json.RawMessage,
) (types.Execution, error) {
	entry, ok := findEntry(sess.Profile, name)
	if !ok {
		return types.Execution{}, fmt.Errorf("pipeline: %w: %q not exposed by session profile", types.ErrNotFound, name)
	}
	resolved, err := resolveEntry(ctx, s.catalog, entry)
	if err != nil {
		return types.Execution{}, fmt.Errorf(
			"pipeline %q: %w: %v", name, ErrUnavailable, err)
	}
	normalized, err := validateArgs(resolved.item.InputSchema, args)
	if err != nil {
		return types.Execution{}, fmt.Errorf("pipeline %q: %w", name, err)
	}
	if s.temporal == nil || s.taskQueue == "" {
		return types.Execution{}, fmt.Errorf("start pipeline: temporal client and task queue are required")
	}
	executionID := types.NewExecutionID()
	// MetaPropagator 通过 header 传递执行坐标，不修改业务参数。
	ctx = invocation.WithMeta(ctx, invocation.Meta{
		SessionID:         sess.ID,
		Session:           sess.PublicID,
		TenantID:          sess.TenantID,
		InvocationID:      string(executionID),
		InvocationKind:    invocation.KindPipeline,
		InvocationName:    name,
		CallbackTaskQueue: s.taskQueue,
	})
	input := WorkflowInput{
		Execution: types.ExecutionRef{
			ID: executionID,
			Session: types.SessionRef{
				SessionID: sess.ID, SessionPublicID: sess.PublicID,
			},
		},
		Name:         name,
		CapabilityID: entry.Capability,
		Args:         normalized,
		Target:       resolved.target,
	}
	_, err = s.temporal.ExecuteWorkflow(ctx,
		execution.StartOptions(sess.PublicID, s.taskQueue), ExecWorkflowType, input)
	if err != nil {
		return types.Execution{}, fmt.Errorf("start pipeline %s (%s): %w",
			name, executionID, execution.ClassifyStartError(err))
	}
	return types.Execution{
		ID:              executionID,
		SessionID:       sess.ID,
		SessionPublicID: sess.PublicID,
		TenantID:        sess.TenantID,
		Kind:            types.ExecutionKindPipeline,
		Status:          types.ExecutionStatusRunning,
		ProfileVersion:  execution.ProfileVersion(sess.Profile),
		Pipeline: &types.PipelineExecution{
			Name: name, CapabilityID: entry.Capability, Args: normalized,
		},
		CreatedAt: time.Now().UTC(),
	}, nil
}

func findEntry(profileJSON []byte, name string) (ProfileEntry, bool) {
	for _, entry := range Entries(profileJSON) {
		if entry.Name == name {
			return entry, true
		}
	}
	return ProfileEntry{}, false
}
