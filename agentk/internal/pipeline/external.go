package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/workflow"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
)

// ExecWorkflowType 是所有 Pipeline 执行共用的 container workflow type。
const ExecWorkflowType = "agentk.pipeline.exec"

const maxTimeoutSeconds = 24 * 60 * 60

var canonicalNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,127}$`)

// Kind 标识 Pipeline 使用 Temporal activity 或 workflow 实现。
type Kind string

const (
	KindWorkflow Kind = "workflow"
	KindActivity Kind = "activity"
)

// ProfileEntry 是 Session Profile 中的 Pipeline capability 引用和公开名称。
// Temporal 路由和参数契约在 List/Start 时从 catalog 解析，不固化进 Profile。
type ProfileEntry struct {
	Capability  string `json:"capability"`
	Ref         string `json:"ref,omitempty"` // 兼容旧 Profile；解析后清空
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
}

func (e ProfileEntry) Normalize() (ProfileEntry, error) {
	if e.Capability != "" && e.Ref != "" {
		return e, fmt.Errorf("capability and ref cannot both be set")
	}
	if e.Ref != "" {
		if !strings.HasPrefix(e.Ref, "pipeline:") {
			return e, fmt.Errorf("legacy ref must start with %q", "pipeline:")
		}
		e.Capability = strings.TrimPrefix(e.Ref, "pipeline:")
		e.Ref = ""
	}
	return e, e.Validate()
}

func (e ProfileEntry) Validate() error {
	if !canonicalNamePattern.MatchString(e.Name) {
		return fmt.Errorf("name must match %s", canonicalNamePattern)
	}
	if err := sdkcapability.ValidateID(e.Capability); err != nil {
		return fmt.Errorf("invalid capability %q", e.Capability)
	}
	if len(e.DisplayName) > 256 {
		return fmt.Errorf("displayName must not exceed 256 characters")
	}
	return nil
}

// Target 是从 Catalog 解析后写入 Workflow History 的执行路由。
type Target struct {
	Kind           Kind   `json:"kind"`
	Type           string `json:"type"`
	TaskQueue      string `json:"taskQueue"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

func (t Target) Validate() error {
	if t.Kind != KindWorkflow && t.Kind != KindActivity {
		return fmt.Errorf("kind must be %q or %q", KindWorkflow, KindActivity)
	}
	if strings.TrimSpace(t.Type) == "" {
		return fmt.Errorf("type is required")
	}
	if strings.TrimSpace(t.TaskQueue) == "" {
		return fmt.Errorf("taskQueue is required")
	}
	if t.TimeoutSeconds <= 0 || t.TimeoutSeconds > maxTimeoutSeconds {
		return fmt.Errorf("timeoutSeconds must be in (0, %d]", maxTimeoutSeconds)
	}
	return nil
}

// ListItem 是不包含 Temporal 路由的 Pipeline API 视图。
type ListItem struct {
	CapabilityID string          `json:"capabilityId"`
	Name         string          `json:"name"`
	DisplayName  string          `json:"displayName,omitempty"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema"`
}

// Entries 从已校验的 Session Profile 读取 Pipeline 引用。
func Entries(profileJSON []byte) []ProfileEntry {
	var doc struct {
		Pipelines []ProfileEntry `json:"pipelines"`
	}
	if err := json.Unmarshal(profileJSON, &doc); err != nil {
		return nil
	}
	for i := range doc.Pipelines {
		entry, err := doc.Pipelines[i].Normalize()
		if err != nil {
			return nil
		}
		doc.Pipelines[i] = entry
	}
	return doc.Pipelines
}

// ExecWorkflow 在统一 container 中记录生命周期，并只向实现传递调用参数。
// Session、tenant 和 exec 坐标通过 invocation Meta 传播，不污染跨语言业务 ABI。
func ExecWorkflow(ctx workflow.Context, input WorkflowInput) (json.RawMessage, error) {
	return runExec(ctx, input, func(ctx workflow.Context) (json.RawMessage, error) {
		args, err := decodeInvocationArgs(input.Args)
		if err != nil {
			return nil, err
		}
		timeout := time.Duration(input.Target.TimeoutSeconds) * time.Second
		switch input.Target.Kind {
		case KindWorkflow:
			childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
				WorkflowID:               string(input.Execution.ID) + "/child",
				TaskQueue:                input.Target.TaskQueue,
				WorkflowExecutionTimeout: timeout,
				ParentClosePolicy:        enumspb.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
			})
			var out json.RawMessage
			err := workflow.ExecuteChildWorkflow(childCtx, input.Target.Type, args).
				Get(childCtx, &out)
			return out, err

		case KindActivity:
			activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
				TaskQueue:              input.Target.TaskQueue,
				ScheduleToCloseTimeout: timeout,
				WaitForCancellation:    true,
			})
			var out json.RawMessage
			err := workflow.ExecuteActivity(activityCtx, input.Target.Type, args).
				Get(activityCtx, &out)
			return out, err

		default:
			return nil, fmt.Errorf("pipeline: unsupported implementation kind %q", input.Target.Kind)
		}
	})
}

// decodeInvocationArgs 使用 UseNumber 解码参数，避免整数先转换为 float64。
func decodeInvocationArgs(raw json.RawMessage) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var args map[string]any
	if err := decoder.Decode(&args); err != nil {
		return nil, fmt.Errorf("pipeline: decode args: %w", err)
	}
	return args, nil
}
