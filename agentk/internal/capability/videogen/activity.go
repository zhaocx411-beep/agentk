package videogen

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"agentk/internal/temporalx"
	"agentk/internal/tool"
)

// ActivityName 是公共最小接口的公开 Temporal Activity 名：提交 + 等待一体，
// 供 pipeline 与编排型能力（storyboard 等）按模型条目名阻塞直调，不关心背后
// 是哪家厂商（对外 ABI：进 Workflow History，改名即断掉在飞调用）。
const ActivityName = "agentk.capability.generate_video"

// ActivityInput 是公共最小接口的公开 payload。Model 是 models catalog 中的
// model entry name，调用方必须显式选择。
type ActivityInput struct {
	Model string `json:"model"`
	// Request 通用业务参数（firstFrame / prompt / resolution / duration / audio），
	// 直接嵌入 payload 顶层。
	Request
}

// 各接口自己的执行 payload：供应商请求直接嵌入顶层，与对应工具的 schema 一致
// （LLM 入参原样即派发入参）。
type (
	KlingT2VInput struct {
		Model string `json:"model"`
		KlingT2VRequest
	}
	KlingI2VInput struct {
		Model string `json:"model"`
		KlingI2VRequest
	}
	KlingOmniInput struct {
		Model string `json:"model"`
		KlingOmniRequest
	}
	SeedanceInput struct {
		Model string `json:"model"`
		SeedanceRequest
	}
)

// ActivityOutput 是公共最小接口的公开返回：成片 URL 与实际选用的模型条目（溯源）。
type ActivityOutput struct {
	Artifacts []tool.ResultArtifact `json:"artifacts"`
}

// ModelLookup 按显式 model entry name 解析生视频客户端。VideoClient 服务公共最小
// 接口和 Pipeline，其余查询分别服务对应工具。
type ModelLookup interface {
	VideoClient(name string) (Client, bool)
	KlingT2VClient(name string) (KlingT2VClient, bool)
	KlingI2VClient(name string) (KlingI2VClient, bool)
	KlingOmniClient(name string) (KlingOmniClient, bool)
	SeedanceClient(name string) (SeedanceClient, bool)
}

// Activity 实现生视频 Activity，每个 Worker 使用一个实例。
type Activity struct {
	models ModelLookup
	// pollDelay 按任务年龄给出下次轮询间隔；nil 用缺省退避（测试注入）。
	pollDelay func(age time.Duration) time.Duration
}

func NewActivity(models ModelLookup) *Activity {
	return &Activity{models: models}
}

// Run 通过公共最小接口执行一次生视频：提交远端任务并在本 Activity 内轮询到终态。
// 等待期不写 History；提交后的任务身份记进 heartbeat，重试续轮而不重新提交
// （见 temporalx 的远端任务机制）。
func (a *Activity) Run(ctx context.Context, in ActivityInput) (ActivityOutput, error) {
	const op = "capability generate_video"
	if err := a.ready(op, in.Model); err != nil {
		return ActivityOutput{}, err
	}
	client, ok := a.models.VideoClient(in.Model)
	if !ok {
		return ActivityOutput{}, unavailable(op, in.Model)
	}

	progress := temporalx.LoadRemoteTaskProgress(ctx)
	if progress.TaskID == "" {
		if err := validateRequest(in.Request); err != nil {
			return ActivityOutput{}, temporalx.ClassifyActivityError(op,
				temporalx.InvalidActivityInput("%v", err))
		}
		// 归一后再提交：真正发出去的参数就是确定的那一组。
		taskID, err := client.Submit(ctx, normalizeRequest(in.Request))
		if err != nil {
			return ActivityOutput{}, temporalx.ClassifyActivityError(op+": submit", err)
		}
		progress = temporalx.NewRemoteTaskProgress(taskID)
		temporalx.RecordRemoteTaskProgress(ctx, progress)
	}

	result, err := temporalx.AwaitRemoteTask(ctx, progress, op, pollAdapter(client), a.pollDelay)
	if err != nil {
		return ActivityOutput{}, err
	}
	var completed struct {
		Artifacts []tool.ResultArtifact `json:"artifacts"`
	}
	if err := json.Unmarshal(result, &completed); err != nil {
		return ActivityOutput{}, temporalx.RemoteTaskFailure(op, "remote task returned invalid artifacts")
	}
	artifacts := make([]tool.ResultArtifact, 0, len(completed.Artifacts))
	for _, artifact := range completed.Artifacts {
		if strings.TrimSpace(artifact.URI) == "" {
			continue
		}
		artifact.Type = "video"
		if artifact.Description == "" {
			artifact.Description = fmt.Sprintf(
				"使用视频生成模型 %s 将首帧 %s 按提示词 %q 生成的视频",
				in.Model, in.FirstFrame, in.Prompt,
			)
		}
		if artifact.Model == "" {
			artifact.Model = in.Model
		}
		artifacts = append(artifacts, artifact)
	}
	if len(artifacts) == 0 {
		return ActivityOutput{}, temporalx.RemoteTaskFailure(op, "remote task completed without artifacts")
	}
	return ActivityOutput{Artifacts: artifacts}, nil
}

func (a *Activity) ExecuteKlingT2V(ctx context.Context, in KlingT2VInput) (ActivityOutput, error) {
	const op = "capability " + KlingT2VToolName
	if err := a.ready(op, in.Model); err != nil {
		return ActivityOutput{}, err
	}
	client, ok := a.models.KlingT2VClient(in.Model)
	if !ok {
		return ActivityOutput{}, unavailable(op, in.Model)
	}
	return executeDialect(ctx, op, in.Model, client, in.KlingT2VRequest, a.pollDelay)
}

func (a *Activity) ExecuteKlingI2V(ctx context.Context, in KlingI2VInput) (ActivityOutput, error) {
	const op = "capability " + KlingI2VToolName
	if err := a.ready(op, in.Model); err != nil {
		return ActivityOutput{}, err
	}
	client, ok := a.models.KlingI2VClient(in.Model)
	if !ok {
		return ActivityOutput{}, unavailable(op, in.Model)
	}
	return executeDialect(ctx, op, in.Model, client, in.KlingI2VRequest, a.pollDelay)
}

func (a *Activity) ExecuteKlingOmni(ctx context.Context, in KlingOmniInput) (ActivityOutput, error) {
	const op = "capability " + KlingOmniToolName
	if err := a.ready(op, in.Model); err != nil {
		return ActivityOutput{}, err
	}
	client, ok := a.models.KlingOmniClient(in.Model)
	if !ok {
		return ActivityOutput{}, unavailable(op, in.Model)
	}
	return executeDialect(ctx, op, in.Model, client, in.KlingOmniRequest, a.pollDelay)
}

func (a *Activity) ExecuteSeedance(ctx context.Context, in SeedanceInput) (ActivityOutput, error) {
	const op = "capability " + SeedanceToolName
	if err := a.ready(op, in.Model); err != nil {
		return ActivityOutput{}, err
	}
	client, ok := a.models.SeedanceClient(in.Model)
	if !ok {
		return ActivityOutput{}, unavailable(op, in.Model)
	}
	return executeDialect(ctx, op, in.Model, client, in.SeedanceRequest, a.pollDelay)
}

// dialectRequest 约束各供应商请求：自校验、填充默认值并回报实际生效参数。
// 各接口的参数表各不相同，但"提交前先把这三件事做完"是同一条纪律。
type dialectRequest[R any] interface {
	Normalize() (R, error)
	Params() map[string]any
}

// dialectClient 是供应商端点的提交与轮询接口。
type dialectClient[R any] interface {
	Submit(ctx context.Context, req R) (string, error)
	tool.Poller
}

func executeDialect[R dialectRequest[R]](
	ctx context.Context, op, model string, client dialectClient[R], req R,
	pollDelay func(time.Duration) time.Duration,
) (ActivityOutput, error) {
	normalized, err := req.Normalize()
	if err != nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("%v", err))
	}
	progress := temporalx.LoadRemoteTaskProgress(ctx)
	if progress.TaskID == "" {
		taskID, err := client.Submit(ctx, normalized)
		if err != nil {
			return ActivityOutput{}, temporalx.ClassifyActivityError(op+": submit", err)
		}
		progress = temporalx.NewRemoteTaskProgress(taskID)
		temporalx.RecordRemoteTaskProgress(ctx, progress)
	}
	result, err := temporalx.AwaitRemoteTask(
		ctx, progress, op, pollAdapter(client), pollDelay)
	if err != nil {
		return ActivityOutput{}, err
	}
	var completed struct {
		Artifacts []tool.ResultArtifact `json:"artifacts"`
	}
	if err := json.Unmarshal(result, &completed); err != nil {
		return ActivityOutput{}, temporalx.RemoteTaskFailure(
			op, "remote task returned invalid artifacts")
	}
	description := dialectDescription(op, model, normalized)
	params := normalized.Params()
	artifacts := make([]tool.ResultArtifact, 0, len(completed.Artifacts))
	for _, artifact := range completed.Artifacts {
		if strings.TrimSpace(artifact.URI) == "" {
			continue
		}
		artifact.Type = "video"
		if artifact.Description == "" {
			artifact.Description = description
		}
		if artifact.Model == "" {
			artifact.Model = model
		}
		if artifact.Params == nil {
			artifact.Params = params
		}
		artifacts = append(artifacts, artifact)
	}
	if len(artifacts) == 0 {
		return ActivityOutput{}, temporalx.RemoteTaskFailure(
			op, "remote task completed without artifacts")
	}
	return ActivityOutput{Artifacts: artifacts}, nil
}

func dialectDescription(op, model string, req any) string {
	toolName := op
	const prefix = "capability "
	if len(toolName) > len(prefix) && toolName[:len(prefix)] == prefix {
		toolName = toolName[len(prefix):]
	}
	switch r := req.(type) {
	case KlingT2VRequest:
		return fmt.Sprintf("使用 %s 按提示词 %q 生成的视频（模型：%s）", toolName, r.Prompt, model)
	case KlingI2VRequest:
		source := "首帧 " + r.FirstFrame
		if r.LastFrame != "" {
			source += " 和尾帧 " + r.LastFrame
		}
		return fmt.Sprintf("使用 %s 根据%s和提示词 %q 生成的视频（模型：%s）",
			toolName, source, r.Prompt, model)
	case KlingOmniRequest:
		sources := make([]string, 0, 4)
		if r.FirstFrame != "" {
			sources = append(sources, "首帧 "+r.FirstFrame)
		}
		if r.LastFrame != "" {
			sources = append(sources, "尾帧 "+r.LastFrame)
		}
		if len(r.ReferImages) > 0 {
			sources = append(sources, fmt.Sprintf("%d 张参考图", len(r.ReferImages)))
		}
		if r.FeatureVideo != "" {
			sources = append(sources, "参考视频 "+r.FeatureVideo)
		}
		if r.BaseVideo != "" {
			sources = append(sources, "待编辑视频 "+r.BaseVideo)
		}
		if len(sources) == 0 {
			sources = append(sources, "纯文本输入")
		}
		return fmt.Sprintf(
			"使用 %s 根据%s，按提示词 %q 生成的视频（模型：%s）",
			toolName, strings.Join(sources, "、"), r.Prompt, model)
	case SeedanceRequest:
		sources := make([]string, 0, 5)
		if r.FirstFrame != "" {
			sources = append(sources, "首帧 "+r.FirstFrame)
		}
		if r.LastFrame != "" {
			sources = append(sources, "尾帧 "+r.LastFrame)
		}
		if len(r.ReferenceImages) > 0 {
			sources = append(sources, fmt.Sprintf("%d 张参考图", len(r.ReferenceImages)))
		}
		if len(r.ReferenceVideos) > 0 {
			sources = append(sources, fmt.Sprintf("%d 段参考视频", len(r.ReferenceVideos)))
		}
		if len(r.ReferenceAudios) > 0 {
			sources = append(sources, fmt.Sprintf("%d 段参考音频", len(r.ReferenceAudios)))
		}
		if len(sources) == 0 {
			sources = append(sources, "纯文本输入")
		}
		return fmt.Sprintf(
			"使用 %s 根据%s，按提示词 %q 生成的视频（模型：%s）",
			toolName, strings.Join(sources, "、"), r.Prompt, model)
	default:
		return fmt.Sprintf("使用 %s 生成的视频（模型：%s）", toolName, model)
	}
}

// ready 校验 Activity 依赖和必填的模型选择。
func (a *Activity) ready(op, model string) error {
	if a.models == nil {
		return temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("videogen model lookup is required")))
	}
	if model == "" {
		return temporalx.ClassifyActivityError(op, temporalx.InvalidActivityInput("model is required"))
	}
	return nil
}

// unavailable 目录里没有这个条目：配置问题，重试无用。
func unavailable(op, model string) error {
	return temporalx.ClassifyActivityError(op,
		temporalx.ConfigurationError(fmt.Errorf("video capability %q is not available", model)))
}

// pollAdapter 把能力层 Poller 适配成 temporalx 的轮询回调。
func pollAdapter(poller tool.Poller) func(context.Context, string) (temporalx.RemoteTaskStatus, error) {
	return func(ctx context.Context, taskID string) (temporalx.RemoteTaskStatus, error) {
		res, err := poller.Poll(ctx, taskID)
		if err != nil {
			return temporalx.RemoteTaskStatus{}, err
		}
		return temporalx.RemoteTaskStatus{
			Done:          res.Done,
			Failed:        res.Failed,
			FailureReason: res.FailureReason,
			Result:        res.Result,
		}, nil
	}
}
