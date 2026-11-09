// Package execution 定义 Agent Loop 与 Pipeline 共用的执行身份、互斥和事件投影。
package execution

import (
	"context"
	"errors"
	"fmt"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"agentk/internal/types"
)

type StartClient interface {
	ExecuteWorkflow(
		ctx context.Context,
		options client.StartWorkflowOptions,
		workflow any,
		args ...any,
	) (client.WorkflowRun, error)
}

type TemporalClient interface {
	StartClient
	CancelWorkflow(ctx context.Context, workflowID, runID string) error
}

func WorkflowID(sessionPublicID string) string {
	return "session/" + sessionPublicID
}

func StartOptions(sessionPublicID, taskQueue string) client.StartWorkflowOptions {
	return client.StartWorkflowOptions{
		ID:                                       WorkflowID(sessionPublicID),
		TaskQueue:                                taskQueue,
		WorkflowIDConflictPolicy:                 enumspb.WORKFLOW_ID_CONFLICT_POLICY_FAIL,
		WorkflowIDReusePolicy:                    enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowExecutionErrorWhenAlreadyStarted: true,
	}
}

func ClassifyStartError(err error) error {
	var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
	if errors.As(err, &alreadyStarted) {
		return types.ErrExecutionActive
	}
	return err
}

type Canceller struct {
	temporal TemporalClient
}

func NewCanceller(temporal TemporalClient) *Canceller {
	if temporal == nil {
		panic("execution: canceller requires temporal client")
	}
	return &Canceller{temporal: temporal}
}

func (c *Canceller) Cancel(ctx context.Context, sess types.Session) error {
	err := c.temporal.CancelWorkflow(ctx, WorkflowID(sess.PublicID), "")
	if err == nil {
		return nil
	}
	var notFound *serviceerror.NotFound
	var notRunning *serviceerror.WorkflowNotReady
	if errors.As(err, &notFound) || errors.As(err, &notRunning) {
		return types.ErrExecutionIdle
	}
	return fmt.Errorf("cancel execution for session %s: %w", sess.PublicID, err)
}
