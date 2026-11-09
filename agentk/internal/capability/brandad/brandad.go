// Package brandad 提供品牌广告视频的编排能力：第一方 workflow 把业务参数提交给
// 外部 HTTP 服务，随后定时轮询到终态。执行体复用 httpcall 的公开 Activity；
// httpcall.Submit 已通过 Idempotency-Key 和记录远端任务身份的 heartbeat 保证重试时
// 不会重复提交，这里不再重复实现。
package brandad

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.temporal.io/sdk/workflow"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/invocation"

	"agentk/internal/capability/httpcall"
	"agentk/internal/temporalx"
	"agentk/internal/tool"
)

// PipelineName 是 profile pipelines 声明与工具面共用的对外名。
const PipelineName = "generate_brand_ad"

// CapabilityID 是 Catalog 中稳定的跨仓能力标识。
const CapabilityID = "agentk.generate_brand_ad"

// WorkflowType 是执行体的稳定注册名。
const WorkflowType = "agentk.capability.generate_brand_ad"

const pipelineDescription = "根据一组商品图片和视频提示词生成品牌广告视频。"

var (
	inputSchema = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["title", "images", "video_prompt"],
		"properties": {
			"title": {"type": "string", "minLength": 1, "description": "广告标题或业务类型，例如“电商”"},
			"images": {"type": "array", "minItems": 1, "items": {"type": "string", "minLength": 1}, "description": "商品图片 URL 列表"},
			"video_prompt": {"type": "string", "minLength": 1, "description": "视频内容、风格和营销诉求"}
		}
	}`)
	outputSchema = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["artifacts"],
		"properties": {
			"artifacts": {
				"type": "array",
				"minItems": 1,
				"items": {
					"type": "object",
					"additionalProperties": false,
					"required": ["type", "uri"],
					"properties": {
						"type": {"type": "string", "const": "video"},
						"uri": {"type": "string", "description": "生成的品牌广告视频 URL"},
						"description": {"type": "string", "description": "产物说明"}
					}
				}
			}
		}
	}`)
)

// 远端服务契约。过渡服务、契约未定，先硬编码；稳定后再考虑配置化。
const (
	submitURL   = "https://kling-aigc-engine.test.gifshow.com/v1/tasks"
	pollURLTmpl = "https://kling-aigc-engine.test.gifshow.com/v1/tasks/{taskId}"

	taskIDField = "task_id"
	statusField = "status"
	errorField  = "error"

	statusSucceeded = "succeeded"
	statusFailed    = "failed"
)

// 远端状态最多轮询 120 分钟；执行预算额外保留 30 分钟给提交和 Activity 重试。
const (
	pollInterval    = 2 * time.Minute
	maxPolls        = 60
	workflowTimeout = time.Duration(maxPolls)*pollInterval + 30*time.Minute
)

// Args 是 workflow 入参，与远端提交接口的 request body 同形。
type Args struct {
	Title       string   `json:"title"`
	Images      []string `json:"images"`
	VideoPrompt string   `json:"video_prompt"`
}

// Workflow 是普通 Temporal workflow：提交远端任务后定时轮询到终态。它只理解
// 自己的 args 与 httpcall 的公开 activity，不知道 agentk pipeline/session/exec。
func Workflow(ctx workflow.Context, args Args) (json.RawMessage, error) {
	if err := validateArgs(args); err != nil {
		return nil, err
	}
	body, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("brandad: marshal args: %w", err)
	}

	submitCtx := workflow.WithActivityOptions(ctx, submitActivityOptions)
	var receipt httpcall.SubmitOutput
	if err := workflow.ExecuteActivity(submitCtx, httpcall.SubmitActivityName,
		httpcall.SubmitInput{
			Config: httpcall.SubmitConfig{
				CallConfig:  httpcall.CallConfig{URL: submitURL},
				TaskIDField: taskIDField,
			},
			Input: body,
		}).Get(submitCtx, &receipt); err != nil {
		return nil, err
	}
	reportProgress(ctx, "submit", "任务已提交: "+receipt.RemoteTaskID)

	pollURL := strings.ReplaceAll(pollURLTmpl, "{taskId}",
		url.PathEscape(receipt.RemoteTaskID))
	for i := 0; i < maxPolls; i++ {
		if err := workflow.Sleep(ctx, pollInterval); err != nil {
			return nil, err
		}
		pollCtx := workflow.WithActivityOptions(ctx, pollActivityOptions)
		var raw json.RawMessage
		if err := workflow.ExecuteActivity(pollCtx, httpcall.CallActivityName,
			httpcall.CallInput{
				Config: httpcall.CallConfig{URL: pollURL, Method: "GET"},
			}).Get(pollCtx, &raw); err != nil {
			// 单轮轮询的瞬态故障即使耗尽重试，也不应终止一个持续几十分钟的任务；
			// 跳过本轮并在下个轮询周期继续。永久性错误（任务不存在、语义 4xx）
			// 再次查询不会改变结果，因此立即失败。
			if temporalx.IsPermanentActivityError(err) {
				return nil, err
			}
			workflow.GetLogger(ctx).Warn("brandad: poll attempt failed",
				"taskId", receipt.RemoteTaskID, "error", err)
			continue
		}
		status, ok := tool.JSONFieldString(raw, statusField)
		if !ok {
			return nil, fmt.Errorf("brandad: poll response missing status field %q: %s",
				statusField, raw)
		}
		switch status {
		case statusSucceeded:
			videoURL, ok := tool.JSONFieldString(raw, "video_url")
			if !ok || videoURL == "" {
				return nil, fmt.Errorf("brandad: completed result missing video_url")
			}
			result, err := json.Marshal(map[string]any{
				"artifacts": []tool.ResultArtifact{{
					Type: "video",
					URI:  videoURL,
					Description: fmt.Sprintf(
						"使用 generate_brand_ad 根据 %d 张商品图片和提示词 %q 生成的品牌广告视频（标题：%s）",
						len(args.Images), args.VideoPrompt, args.Title),
				}},
			})
			if err != nil {
				return nil, fmt.Errorf("brandad: encode completed result: %w", err)
			}
			return result, nil
		case statusFailed:
			reason := status
			if msg, ok := tool.JSONFieldString(raw, errorField); ok && msg != "" {
				reason = msg
			}
			return nil, fmt.Errorf("brandad: remote task %s failed: %s",
				receipt.RemoteTaskID, reason)
		}
		reportProgress(ctx, "poll", "生成中: "+status)
	}
	return nil, fmt.Errorf("brandad: task %s not finished within %s",
		receipt.RemoteTaskID, time.Duration(maxPolls)*pollInterval)
}

// ManifestCapability 返回由核心 Worker 发布的唯一能力声明。
func ManifestCapability() sdkcapability.Capability {
	return sdkcapability.Capability{
		ID:             CapabilityID,
		TemporalType:   WorkflowType,
		Description:    pipelineDescription,
		InputSchema:    append(json.RawMessage(nil), inputSchema...),
		OutputSchema:   append(json.RawMessage(nil), outputSchema...),
		TimeoutSeconds: int64(workflowTimeout / time.Second),
		Tool: &sdkcapability.Tool{
			Description: pipelineDescription,
			Mode:        sdkcapability.ToolModeBlocking,
		},
	}
}

func validateArgs(args Args) error {
	if args.Title == "" || len(args.Images) == 0 || args.VideoPrompt == "" {
		return fmt.Errorf("brandad: title, images and video_prompt are required")
	}
	return nil
}

// reportProgress 上报进度但不向外传播失败：进度记录不属于生成主流程，即使耗尽重试
// 也不应导致生成失败。不在 pipeline 调度下运行（工具调用或直接调用）时，SDK 侧 no-op。
func reportProgress(ctx workflow.Context, stepID, message string) {
	if err := invocation.ReportProgress(ctx, stepID, message, nil); err != nil {
		workflow.GetLogger(ctx).Warn("brandad: report progress failed",
			"stepId", stepID, "error", err)
	}
}

var (
	submitActivityOptions = workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy:         temporalx.ActivityRetryPolicy(3),
		WaitForCancellation: true,
	}
	pollActivityOptions = workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy:         temporalx.ActivityRetryPolicy(3),
		WaitForCancellation: true,
	}
)
