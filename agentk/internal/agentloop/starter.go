package agentloop

import (
	"context"
	"fmt"

	"agentk/internal/event"
	"agentk/internal/execution"
	"agentk/internal/types"
)

// Starter 启动或取消 AgentWorkflow。每个输入启动一次新的 Workflow execution；
// 同一 Session 已有活跃 execution 时由 Temporal 原子拒绝。
type Starter struct {
	temporal  execution.TemporalClient
	taskQueue string
}

func NewStarter(temporal execution.TemporalClient, taskQueue string) *Starter {
	if temporal == nil || taskQueue == "" {
		panic("agentloop: starter requires temporal client and task queue")
	}
	return &Starter{temporal: temporal, taskQueue: taskQueue}
}

func (s *Starter) StartMessage(
	ctx context.Context, sess types.Session, text string,
) (types.ExecutionID, error) {
	return s.start(ctx, sess, AgentTrigger{Message: &event.UserInput{Text: text}})
}

func (s *Starter) StartAnswer(
	ctx context.Context, sess types.Session, answer event.AskUserResponded,
) (types.ExecutionID, error) {
	return s.start(ctx, sess, AgentTrigger{Answer: &answer})
}

func (s *Starter) start(
	ctx context.Context, sess types.Session, trigger AgentTrigger,
) (types.ExecutionID, error) {
	if sess.PublicID == "" || sess.ID <= 0 {
		return "", fmt.Errorf("start agent loop: sessionId and sessionPublicId are required")
	}
	if _, err := trigger.Draft(); err != nil {
		return "", fmt.Errorf("start agent loop: %w", err)
	}
	executionID := types.NewExecutionID()
	_, err := s.temporal.ExecuteWorkflow(ctx,
		execution.StartOptions(sess.PublicID, s.taskQueue),
		AgentWorkflowType, AgentWorkflowInput{
			Execution: types.ExecutionRef{
				ID: executionID,
				Session: types.SessionRef{
					SessionID: sess.ID, SessionPublicID: sess.PublicID,
				},
			},
			Trigger: trigger,
		},
	)
	if err != nil {
		return "", fmt.Errorf("start agent loop for session %s: %w",
			sess.PublicID, execution.ClassifyStartError(err))
	}
	return executionID, nil
}
