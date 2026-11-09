package agentloop

import (
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/workflow"

	"agentk/internal/temporalx"
)

// Register 注册 Session workflow 及其 activity。
func Register(w temporalx.Registry, cfg Config) {
	activities := NewActivities(cfg)

	w.RegisterWorkflowWithOptions(AgentWorkflow, workflow.RegisterOptions{Name: AgentWorkflowType})
	w.RegisterActivityWithOptions(activities.BeginExecutionActivity,
		activity.RegisterOptions{Name: BeginExecutionActivityName})
	w.RegisterActivityWithOptions(activities.FinishExecutionActivity,
		activity.RegisterOptions{Name: FinishExecutionActivityName})
	w.RegisterActivityWithOptions(activities.RoundActivity,
		activity.RegisterOptions{Name: RoundActivityName})
	w.RegisterActivityWithOptions(activities.ExecuteInlineToolActivity,
		activity.RegisterOptions{Name: ExecuteInlineToolActivityName})
	// capability activity/workflow 由装配方另行注册。
	w.RegisterActivityWithOptions(activities.BeginToolActivity,
		activity.RegisterOptions{Name: BeginToolActivityName})
	w.RegisterActivityWithOptions(activities.FinishToolActivity,
		activity.RegisterOptions{Name: FinishToolActivityName})
	w.RegisterActivityWithOptions(activities.AskUserActivity,
		activity.RegisterOptions{Name: AskUserActivityName})
}
