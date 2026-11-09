package discovery

import (
	"time"

	"go.temporal.io/sdk/workflow"

	"agentk/internal/temporalx"
)

type RegisterActivityInput struct {
	Manifest map[string]any `json:"manifest"`
}

func RegisterWorkflow(ctx workflow.Context, manifest map[string]any) error {
	actCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout:    30 * time.Second,
		ScheduleToCloseTimeout: 2 * time.Minute,
		RetryPolicy:            temporalx.ActivityRetryPolicy(3),
	})
	return workflow.ExecuteActivity(actCtx, RegisterActivityType, RegisterActivityInput{
		Manifest: manifest,
	}).Get(actCtx, nil)
}
