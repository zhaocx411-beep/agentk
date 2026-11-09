package render

import (
	"errors"
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"agentcut/timeline"
)

// WorkflowName 是渲染 Workflow 的注册名。它跑在 AgentCut 自己的 Task Queue
// 上、由 cut-apiserver 直接启动——不经过 agent-k，不占用 Session 的活跃
// Execution 名额（架构不变量：用户可以边导出边和 Agent 对话）。
const WorkflowName = "RenderTimelineWorkflow"

// QueryProgress 返回渲染进度。
const QueryProgress = "getProgress"

// Input 是渲染入参。Doc 是启动时刻的文档快照：导出的是"那一刻"的时间线，
// 之后的编辑不影响进行中的渲染。
type Input struct {
	ProjectID string       `json:"projectId"`
	RenderKey string       `json:"renderKey"`
	Revision  uint64       `json:"revision"`
	Doc       timeline.Doc `json:"doc"`
	Executor  ExecutorKind `json:"executor,omitempty"`
	Profile   string       `json:"profile,omitempty"`
}

// Output 是渲染结果。
type Output struct {
	OutputURI string       `json:"outputUri"`
	RenderKey string       `json:"renderKey"`
	Revision  uint64       `json:"revision"`
	Calls     int          `json:"calls"`
	Warnings  []string     `json:"warnings,omitempty"`
	Executor  ExecutorKind `json:"executor,omitempty"`
	Profile   string       `json:"profile,omitempty"`
	OutputKey string       `json:"outputKey,omitempty"`
}

// Progress 是 QueryProgress 的返回。
type Progress struct {
	Done     int      `json:"done"`
	Total    int      `json:"total"`
	Current  string   `json:"current,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// ffmpegOutput 是 FfmpegToolOutput 的最小解码形状。
type ffmpegOutput struct {
	Artifacts []ffmpegArtifact `json:"artifacts"`
}

type ffmpegArtifact struct {
	Type string `json:"type"`
	URI  string `json:"uri"`
}

// Workflow 编译文档并逐个执行 FfmpegTool 子 Workflow。
//
// MVP 串行执行：调用序列本身是线性依赖为主（base 链），并行化收益留给
// M2（可并行的只有各片段的预处理）。
func Workflow(ctx workflow.Context, in Input) (Output, error) {
	selection := EffectiveSelection(in)
	executor, err := executorFor(selection.Executor)
	if err != nil {
		return Output{}, temporal.NewNonRetryableApplicationError(err.Error(), "RENDER_EXECUTOR_INVALID", nil)
	}

	// Compile 是纯函数，可以在 Workflow 内直接调用（replay 安全）。
	var plan Plan
	if selection.Executor == ExecutorLocal {
		plan, err = CompileManaged(in.Doc)
	} else {
		plan, err = Compile(in.Doc)
	}
	if err != nil {
		var unsupported *UnsupportedRenderFeatureError
		if errors.As(err, &unsupported) {
			return Output{}, temporal.NewNonRetryableApplicationError(
				err.Error(), "UNSUPPORTED_RENDER_FEATURE", nil)
		}
		return Output{}, temporal.NewNonRetryableApplicationError(err.Error(), "RENDER_COMPILE_FAILED", nil)
	}
	if selection.Executor == ExecutorLocal {
		defer cleanupLocalRenderIntermediates(ctx, in, plan)
	}

	progress := Progress{Total: len(plan.Calls), Warnings: plan.Warnings}
	if err := workflow.SetQueryHandler(ctx, QueryProgress, func() (Progress, error) {
		return progress, nil
	}); err != nil {
		return Output{}, err
	}

	outputs := make(map[string]string, len(plan.Calls))
	lastURI := ""
	for i, call := range plan.Calls {
		progress.Done, progress.Current = i, call.ID

		input, ok := ResolveRefs(call.Input, outputs)
		if !ok {
			return Output{}, temporal.NewNonRetryableApplicationError(
				fmt.Sprintf("call %s references an unknown artifact", call.ID), "RENDER_PLAN_INVALID", nil)
		}

		result, err := executor.Execute(ctx, ExecuteRequest{
			ProjectID:       in.ProjectID,
			RenderKey:       in.RenderKey,
			Call:            call,
			Input:           input,
			Media:           in.Doc.Media,
			Width:           in.Doc.Metadata.Width,
			Height:          in.Doc.Metadata.Height,
			FPS:             in.Doc.Metadata.FPS,
			BackgroundColor: in.Doc.Metadata.BackgroundColor,
			Final:           i == len(plan.Calls)-1,
		})
		if err != nil {
			return Output{}, fmt.Errorf("render call %s: %w", call.ID, err)
		}
		if result.Ref == "" {
			return Output{}, temporal.NewNonRetryableApplicationError(
				fmt.Sprintf("call %s returned no artifact", call.ID), "RENDER_NO_ARTIFACT", nil)
		}
		outputs[call.ID] = result.Ref
		lastURI = result.Ref
	}
	progress.Done, progress.Current = len(plan.Calls), ""

	out := Output{
		OutputURI: lastURI,
		RenderKey: in.RenderKey,
		Revision:  in.Revision,
		Calls:     len(plan.Calls),
		Warnings:  plan.Warnings,
		Executor:  selection.Executor,
		Profile:   selection.Profile,
	}
	if selection.Executor == ExecutorLocal {
		out.OutputKey = lastURI
		out.OutputURI = ContentURI(in.ProjectID, in.RenderKey)
	}
	return out, nil
}

func cleanupLocalRenderIntermediates(ctx workflow.Context, in Input, plan Plan) {
	cleanupCtx, _ := workflow.NewDisconnectedContext(ctx)
	cleanupCtx = workflow.WithActivityOptions(cleanupCtx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    10 * time.Second,
			MaximumAttempts:    3,
		},
	})
	callIDs := make([]string, len(plan.Calls))
	for index, call := range plan.Calls {
		callIDs[index] = call.ID
	}
	err := workflow.ExecuteActivity(cleanupCtx, LocalCleanupActivityName, LocalCleanupInput{
		ProjectID: in.ProjectID,
		RenderKey: in.RenderKey,
		CallIDs:   callIDs,
	}).Get(cleanupCtx, nil)
	if err != nil {
		workflow.GetLogger(ctx).Error("render intermediate cleanup failed",
			"projectId", in.ProjectID, "renderKey", in.RenderKey, "error", err)
	}
}

// WorkflowID 是渲染 Workflow 的确定性 ID：同一项目同一文档内容只会有
// 一次渲染（幂等 renderKey）。
func WorkflowID(projectID, renderKey string) string {
	return "render:" + projectID + ":" + renderKey
}

func ContentURI(projectID, renderKey string) string {
	return "/v1/projects/" + projectID + "/renders/" + renderKey + "/content"
}
