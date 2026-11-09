package invocation

import (
	"context"
	"encoding/json"
	"time"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/internal/workermetrics"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/failure"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const ReportProgressActivityType = "agentk.invocation.progress.v1"

type ReportProgressInput struct {
	SessionID      int64           `json:"sessionId"`
	InvocationID   string          `json:"invocationId"`
	InvocationKind Kind            `json:"invocationKind"`
	InvocationName string          `json:"invocationName"`
	StepID         string          `json:"stepId"`
	Message        string          `json:"message,omitempty"`
	Data           json.RawMessage `json:"data,omitempty"`
}

func ReportProgress(ctx workflow.Context, stepID, message string, data json.RawMessage) error {
	m, ok := MetaFromWorkflow(ctx)
	if !ok || !m.HasSession() {
		return nil
	}
	actCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		TaskQueue:              m.CallbackTaskQueue,
		StartToCloseTimeout:    30 * time.Second,
		ScheduleToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:        time.Second,
			BackoffCoefficient:     2,
			MaximumInterval:        10 * time.Second,
			MaximumAttempts:        3,
			NonRetryableErrorTypes: failure.NonRetryableTypes,
		},
	})
	err := workflow.ExecuteActivity(actCtx, ReportProgressActivityType, ReportProgressInput{
		SessionID:      m.SessionID,
		InvocationID:   m.InvocationID,
		InvocationKind: m.InvocationKind,
		InvocationName: m.InvocationName,
		StepID:         stepID,
		Message:        message,
		Data:           data,
	}).Get(actCtx, nil)
	if !workflow.IsReplaying(ctx) {
		workermetrics.ProgressReport(context.Background(), m.AppID, workermetrics.Status(err))
	}
	return err
}
