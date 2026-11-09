package videogen

import (
	"time"

	"go.temporal.io/sdk/workflow"

	"agentk/internal/temporalx"
)

const (
	KlingT2VWorkflowType  = "agentk.capability.kling_text_to_video"
	KlingI2VWorkflowType  = "agentk.capability.kling_image_to_video"
	KlingOmniWorkflowType = "agentk.capability.kling_omni_video"
	SeedanceWorkflowType  = "agentk.capability.seedance_generate_video"

	KlingT2VExecutionActivityName  = "agentk.capability.kling_text_to_video_execute"
	KlingI2VExecutionActivityName  = "agentk.capability.kling_image_to_video_execute"
	KlingOmniExecutionActivityName = "agentk.capability.kling_omni_video_execute"
	SeedanceExecutionActivityName  = "agentk.capability.seedance_generate_video_execute"

	videoWorkflowTimeout = 24 * time.Hour
)

var videoExecutionActivityOptions = workflow.ActivityOptions{
	StartToCloseTimeout:    videoWorkflowTimeout,
	ScheduleToCloseTimeout: videoWorkflowTimeout,
	HeartbeatTimeout:       time.Minute,
	RetryPolicy:            temporalx.ActivityRetryPolicy(10),
	WaitForCancellation:    true,
}

func KlingT2VWorkflow(ctx workflow.Context, in KlingT2VInput) (ActivityOutput, error) {
	return executeVideoActivity(ctx, KlingT2VExecutionActivityName, in)
}

func KlingI2VWorkflow(ctx workflow.Context, in KlingI2VInput) (ActivityOutput, error) {
	return executeVideoActivity(ctx, KlingI2VExecutionActivityName, in)
}

func KlingOmniWorkflow(ctx workflow.Context, in KlingOmniInput) (ActivityOutput, error) {
	return executeVideoActivity(ctx, KlingOmniExecutionActivityName, in)
}

func SeedanceWorkflow(ctx workflow.Context, in SeedanceInput) (ActivityOutput, error) {
	return executeVideoActivity(ctx, SeedanceExecutionActivityName, in)
}

func executeVideoActivity(
	ctx workflow.Context,
	activityName string,
	input any,
) (ActivityOutput, error) {
	activityCtx := workflow.WithActivityOptions(ctx, videoExecutionActivityOptions)
	var out ActivityOutput
	if err := workflow.ExecuteActivity(activityCtx, activityName, input).Get(activityCtx, &out); err != nil {
		return ActivityOutput{}, err
	}
	return out, nil
}
