package httpcall

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"go.temporal.io/sdk/activity"

	"agentk/internal/temporalx"
	"agentk/internal/tool"
)

// CallActivityName 和 SubmitActivityName 是供内部 Workflow 调用的稳定 Temporal ABI 名。
const CallActivityName = "agentk.capability.http_call"

const SubmitActivityName = "agentk.capability.http_submit"

// CallConfig 是调用方 workflow 提供的端点配置。
type CallConfig struct {
	URL string `json:"url"`
	// Method 为空时使用 POST。
	Method string `json:"method,omitempty"`
}

// SubmitConfig 是提交半程的 config 段。
type SubmitConfig struct {
	CallConfig
	// TaskIDField 是提交响应中远端任务 ID 的点分路径，例如 "data.task_id"。
	TaskIDField string `json:"taskIdField"`
}

// CallInput 分离声明提供的固定配置和作为 request body 原样发送的 LLM 输入。
type CallInput struct {
	Config CallConfig      `json:"config"`
	Input  json.RawMessage `json:"input"`
}

// SubmitInput 是 http_submit 的两段式入参。
type SubmitInput struct {
	Config SubmitConfig    `json:"config"`
	Input  json.RawMessage `json:"input"`
}

// SubmitOutput 是提交半程返回的远端任务 id。
type SubmitOutput struct {
	RemoteTaskID string `json:"remoteTaskId"`
}

// Activity 执行通用 HTTP 调用，每个 Worker 复用一个实例。
type Activity struct {
	client *http.Client
}

// NewActivity 在 client 为 nil 时使用 http.DefaultClient。
func NewActivity(client *http.Client) *Activity {
	if client == nil {
		client = http.DefaultClient
	}
	return &Activity{client: client}
}

// Call 执行同步 HTTP 请求。2xx JSON 原样返回，其他 2xx body 包装为 raw 字段。
// 非 2xx 按状态码分类：语义 4xx 不重试，408、429 和 5xx 可重试。
func (a *Activity) Call(ctx context.Context, in CallInput) (json.RawMessage, error) {
	const op = "capability http_call"
	method, err := resolveMethod(in.Config.URL, in.Config.Method, http.MethodPost)
	if err != nil {
		return nil, temporalx.ClassifyActivityError(op, err)
	}
	raw, err := doJSONRequest(ctx, a.client, method, in.Config.URL, in.Input, idempotencyKey(ctx))
	if err != nil {
		return nil, temporalx.ClassifyActivityError(op, err)
	}
	if json.Valid(raw) && len(bytes.TrimSpace(raw)) > 0 {
		return json.RawMessage(raw), nil
	}
	out, err := json.Marshal(map[string]string{"raw": string(raw)})
	if err != nil {
		return nil, temporalx.ClassifyActivityError(op, err)
	}
	return out, nil
}

// Submit 调用异步任务的提交端点，并按 TaskIDField 提取远端任务 ID。
// 成功后将任务 ID 写入 heartbeat；Activity 重试时复用该 ID，避免重复提交。
func (a *Activity) Submit(ctx context.Context, in SubmitInput) (SubmitOutput, error) {
	const op = "capability http_submit"
	if in.Config.TaskIDField == "" {
		return SubmitOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("config.taskIdField is required"))
	}
	method, err := resolveMethod(in.Config.URL, in.Config.Method, http.MethodPost)
	if err != nil {
		return SubmitOutput{}, temporalx.ClassifyActivityError(op, err)
	}

	progress := temporalx.LoadRemoteTaskProgress(ctx)
	if progress.TaskID == "" {
		raw, err := doJSONRequest(ctx, a.client, method, in.Config.URL, in.Input, idempotencyKey(ctx))
		if err != nil {
			return SubmitOutput{}, temporalx.ClassifyActivityError(op+": submit", err)
		}
		taskID, ok := tool.JSONFieldString(raw, in.Config.TaskIDField)
		if !ok || taskID == "" {
			// 远端可能已经创建任务；缺少 ID 时无法轮询，也不能安全重试提交。
			return SubmitOutput{}, temporalx.RemoteTaskFailure(op,
				fmt.Sprintf("submit response missing task id field %q: %s",
					in.Config.TaskIDField, bytes.TrimSpace(raw)))
		}
		progress = temporalx.NewRemoteTaskProgress(taskID)
		temporalx.RecordRemoteTaskProgress(ctx, progress)
	}
	return SubmitOutput{RemoteTaskID: progress.TaskID}, nil
}

// resolveMethod 使用默认方法并拒绝白名单外取值。
// 声明侧虽已校验，这里仍需保护直接调用 Temporal ABI 的调用方。
func resolveMethod(url, method, fallback string) (string, error) {
	if url == "" {
		return "", temporalx.InvalidActivityInput("config.url is required")
	}
	if method == "" {
		return fallback, nil
	}
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete:
		return method, nil
	default:
		return "", temporalx.InvalidActivityInput("unsupported method %q", method)
	}
}

// idempotencyKey 从 Activity 调度身份派生：同一调度的重试保持不变，新的调度不同。
// 非 Activity context 返回空，不发送 Idempotency-Key header。
func idempotencyKey(ctx context.Context) string {
	if !activity.IsActivity(ctx) {
		return ""
	}
	info := activity.GetInfo(ctx)
	return info.WorkflowExecution.ID + "/" + info.ActivityID
}
