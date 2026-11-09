package agentloop

import (
	"context"
	"errors"
	"testing"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"agentk/internal/event"
	"agentk/internal/types"
)

type startCall struct {
	Options  client.StartWorkflowOptions
	Workflow any
	Args     []any
}

type fakeTemporal struct {
	starts    []startCall
	startErr  error
	cancelled []string
	cancelErr error
}

func (f *fakeTemporal) ExecuteWorkflow(
	_ context.Context,
	options client.StartWorkflowOptions,
	workflow any,
	args ...any,
) (client.WorkflowRun, error) {
	f.starts = append(f.starts, startCall{Options: options, Workflow: workflow, Args: args})
	return nil, f.startErr
}

func (f *fakeTemporal) CancelWorkflow(_ context.Context, workflowID, _ string) error {
	f.cancelled = append(f.cancelled, workflowID)
	return f.cancelErr
}

func newStarterFixture() (*Starter, *fakeTemporal, types.Session) {
	tc := &fakeTemporal{}
	sess := types.Session{ID: 7, PublicID: "ses_test", TenantID: "org_test"}
	return NewStarter(tc, "queue-1"), tc, sess
}

func TestStartMessageStartsExclusiveAgentWorkflow(t *testing.T) {
	starter, tc, sess := newStarterFixture()

	executionID, err := starter.StartMessage(t.Context(), sess, "在吗")
	if err != nil {
		t.Fatal(err)
	}
	if len(tc.starts) != 1 {
		t.Fatalf("starts = %+v", tc.starts)
	}
	got := tc.starts[0]
	if got.Options.ID != "session/"+sess.PublicID || got.Options.TaskQueue != "queue-1" {
		t.Fatalf("options = %+v", got.Options)
	}
	if got.Options.WorkflowIDConflictPolicy != enumspb.WORKFLOW_ID_CONFLICT_POLICY_FAIL {
		t.Fatalf("conflict policy = %v, want FAIL", got.Options.WorkflowIDConflictPolicy)
	}
	if got.Options.WorkflowIDReusePolicy != enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE {
		t.Fatalf("reuse policy = %v, want ALLOW_DUPLICATE", got.Options.WorkflowIDReusePolicy)
	}
	if !got.Options.WorkflowExecutionErrorWhenAlreadyStarted {
		t.Fatal("already-started executions must return an error")
	}
	if got.Workflow != AgentWorkflowType || len(got.Args) != 1 {
		t.Fatalf("workflow = %v, args = %+v", got.Workflow, got.Args)
	}
	input, ok := got.Args[0].(AgentWorkflowInput)
	if !ok || input.Execution.ID != executionID || input.Execution.Session.SessionID != sess.ID ||
		input.Trigger.Message == nil ||
		input.Trigger.Message.Text != "在吗" || input.Trigger.Answer != nil {
		t.Fatalf("input = %+v", got.Args[0])
	}
}

func TestStartAnswerCarriesNormalizedAnswer(t *testing.T) {
	starter, tc, sess := newStarterFixture()
	answer := event.AskUserResponded{
		ToolCallID: "call-1",
		Answers:    []event.Answer{{QuestionID: "q1", Question: "画风？", Selected: []string{"动画"}}},
	}

	if _, err := starter.StartAnswer(t.Context(), sess, answer); err != nil {
		t.Fatal(err)
	}
	input := tc.starts[0].Args[0].(AgentWorkflowInput)
	if input.Trigger.Message != nil || input.Trigger.Answer == nil ||
		input.Trigger.Answer.ToolCallID != "call-1" {
		t.Fatalf("trigger = %+v", input.Trigger)
	}
}

func TestStartMapsAlreadyRunningToExecutionActive(t *testing.T) {
	starter, tc, sess := newStarterFixture()
	tc.startErr = &serviceerror.WorkflowExecutionAlreadyStarted{Message: "running"}

	if _, err := starter.StartMessage(t.Context(), sess, "hello"); !errors.Is(err, types.ErrExecutionActive) {
		t.Fatalf("err = %v, want ErrExecutionActive", err)
	}
}

func TestStartRejectsInvalidSessionBeforeTemporal(t *testing.T) {
	starter, tc, _ := newStarterFixture()
	for _, sess := range []types.Session{{}, {ID: 1}, {PublicID: "ses_x"}} {
		if _, err := starter.StartMessage(t.Context(), sess, "hello"); err == nil {
			t.Fatalf("session %+v must be rejected", sess)
		}
	}
	if len(tc.starts) != 0 {
		t.Fatal("invalid sessions must not reach Temporal")
	}
}

func TestNewStarterRequiresDependencies(t *testing.T) {
	tc := &fakeTemporal{}
	for name, build := range map[string]func(){
		"no temporal":   func() { NewStarter(nil, "q") },
		"no task queue": func() { NewStarter(tc, "") },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected a panic on incomplete wiring")
				}
			}()
			build()
		})
	}
}
