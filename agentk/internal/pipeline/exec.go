package pipeline

import (
	"encoding/json"
	"errors"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"agentk/internal/temporalx"
	"agentk/internal/types"
)

func runExec(
	ctx workflow.Context,
	input WorkflowInput,
	invoke func(workflow.Context) (json.RawMessage, error),
) (output json.RawMessage, workflowErr error) {
	defer func() {
		status := types.ExecutionStatusCompleted
		detail := ""
		if workflowErr != nil {
			status = types.ExecutionStatusFailed
			detail = boundedError(workflowErr.Error())
			if temporal.IsCanceledError(workflowErr) || errors.Is(ctx.Err(), workflow.ErrCanceled) {
				status = types.ExecutionStatusCancelled
			}
		}
		if err := finishExecution(ctx, FinishExecutionInput{
			Execution: input.Execution,
			Status:    status,
			Output:    output,
			Error:     detail,
		}); err != nil && workflowErr == nil {
			output = nil
			workflowErr = err
		}
	}()

	beginCtx, _ := workflow.NewDisconnectedContext(ctx)
	beginCtx = workflow.WithActivityOptions(beginCtx, lifecycleActivityOptions)
	if err := workflow.ExecuteActivity(
		beginCtx, BeginExecutionActivityName, input,
	).Get(beginCtx, nil); err != nil {
		return nil, err
	}

	output, workflowErr = invoke(ctx)
	if temporal.IsCanceledError(workflowErr) || errors.Is(ctx.Err(), workflow.ErrCanceled) {
		return nil, temporal.NewCanceledError("pipeline execution cancelled")
	}
	return output, workflowErr
}

func finishExecution(ctx workflow.Context, input FinishExecutionInput) error {
	disconnected, _ := workflow.NewDisconnectedContext(ctx)
	finishCtx := workflow.WithActivityOptions(disconnected, lifecycleActivityOptions)
	return workflow.ExecuteActivity(
		finishCtx, FinishExecutionActivityName, input,
	).Get(finishCtx, nil)
}

func boundedError(message string) string {
	const maxBytes = 4096
	if len(message) <= maxBytes {
		return message
	}
	return message[:maxBytes]
}

var lifecycleActivityOptions = workflow.ActivityOptions{
	StartToCloseTimeout: 30 * time.Second,
	RetryPolicy:         temporalx.ActivityRetryPolicy(0),
}
