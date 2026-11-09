package execution

import (
	"context"
	"errors"
	"testing"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"agentk/internal/types"
)

func TestStartOptionsReserveOneWorkflowIDPerSession(t *testing.T) {
	got := StartOptions("ses_123", "agentk-test")
	if got.ID != "session/ses_123" || got.TaskQueue != "agentk-test" ||
		got.WorkflowIDConflictPolicy != enumspb.WORKFLOW_ID_CONFLICT_POLICY_FAIL ||
		got.WorkflowIDReusePolicy != enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE ||
		!got.WorkflowExecutionErrorWhenAlreadyStarted {
		t.Fatalf("options = %+v", got)
	}
}

func TestClassifyStartError(t *testing.T) {
	if err := ClassifyStartError(&serviceerror.WorkflowExecutionAlreadyStarted{}); !errors.Is(err, types.ErrExecutionActive) {
		t.Fatalf("already started = %v", err)
	}
	want := errors.New("unavailable")
	if got := ClassifyStartError(want); !errors.Is(got, want) {
		t.Fatalf("other error = %v", got)
	}
}

type cancelClient struct {
	workflowID string
	err        error
}

func (c *cancelClient) ExecuteWorkflow(
	context.Context, client.StartWorkflowOptions, any, ...any,
) (client.WorkflowRun, error) {
	panic("unexpected ExecuteWorkflow")
}

func (c *cancelClient) CancelWorkflow(_ context.Context, workflowID, runID string) error {
	c.workflowID = workflowID
	if runID != "" {
		panic("run ID must remain private")
	}
	return c.err
}

func TestCancellerUsesSharedSessionWorkflowID(t *testing.T) {
	temporal := &cancelClient{}
	canceller := NewCanceller(temporal)
	if err := canceller.Cancel(context.Background(), types.Session{PublicID: "ses_123"}); err != nil {
		t.Fatal(err)
	}
	if temporal.workflowID != "session/ses_123" {
		t.Fatalf("workflow id = %q", temporal.workflowID)
	}
}

func TestCancellerMapsIdleExecution(t *testing.T) {
	temporal := &cancelClient{err: &serviceerror.NotFound{}}
	if err := NewCanceller(temporal).Cancel(
		context.Background(), types.Session{PublicID: "ses_123"},
	); !errors.Is(err, types.ErrExecutionIdle) {
		t.Fatalf("cancel error = %v", err)
	}
}
