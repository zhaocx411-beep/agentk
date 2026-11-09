package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"agentk/internal/types"
)

const (
	testExecID       = types.ExecutionID("exe_0191f38d-99f2-7d91-9f2b-712ab18e0a00")
	testSessionID    = "ses_test"
	testCapabilityID = "contrib.test.demo"
)

func registerExecLifecycle(t *testing.T, env *testsuite.TestWorkflowEnvironment, finished *[]FinishExecutionInput) {
	t.Helper()
	env.RegisterActivityWithOptions(
		func(context.Context, WorkflowInput) error {
			return nil
		},
		activity.RegisterOptions{Name: BeginExecutionActivityName},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, in FinishExecutionInput) error {
			*finished = append(*finished, in)
			return nil
		},
		activity.RegisterOptions{Name: FinishExecutionActivityName},
	)
}

func execInput(kind Kind, implementationType string, args string) WorkflowInput {
	return WorkflowInput{
		Execution: types.ExecutionRef{
			ID:      testExecID,
			Session: types.SessionRef{SessionID: 7, SessionPublicID: testSessionID},
		},
		Name:         "demo",
		CapabilityID: testCapabilityID,
		Args:         json.RawMessage(args),
		Target: Target{
			Kind: kind, Type: implementationType,
			TaskQueue: "default-test-taskqueue", TimeoutSeconds: 3600,
		},
	}
}

func TestExecWorkflowDispatchesRawArgsToWorkflow(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var finished []FinishExecutionInput
	registerExecLifecycle(t, env, &finished)
	env.RegisterWorkflowWithOptions(ExecWorkflow, workflow.RegisterOptions{Name: ExecWorkflowType})

	var got map[string]any
	child := func(_ workflow.Context, args map[string]any) (map[string]any, error) {
		got = args
		return map[string]any{"ok": true}, nil
	}
	env.RegisterWorkflowWithOptions(child, workflow.RegisterOptions{Name: "demo.workflow"})
	env.ExecuteWorkflow(ExecWorkflowType, execInput(KindWorkflow, "demo.workflow", `{"topic":"猫"}`))
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	if got["topic"] != "猫" {
		t.Fatalf("implementation args = %#v", got)
	}
	if len(finished) != 1 || finished[0].Status != types.ExecutionStatusCompleted ||
		finished[0].Execution.ID != testExecID {
		t.Fatalf("finish calls = %+v", finished)
	}
}

func TestExecWorkflowDispatchesRawArgsToActivity(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var finished []FinishExecutionInput
	registerExecLifecycle(t, env, &finished)
	env.RegisterWorkflowWithOptions(ExecWorkflow, workflow.RegisterOptions{Name: ExecWorkflowType})

	var got map[string]any
	env.RegisterActivityWithOptions(
		func(_ context.Context, args map[string]any) (map[string]any, error) {
			got = args
			return map[string]any{"ok": true}, nil
		},
		activity.RegisterOptions{Name: "demo.activity"},
	)
	env.ExecuteWorkflow(ExecWorkflowType, execInput(KindActivity, "demo.activity", `{"n":2}`))
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	if got["n"] != float64(2) && got["n"] != json.Number("2") {
		t.Fatalf("implementation args = %#v", got)
	}
	if len(finished) != 1 || finished[0].Status != types.ExecutionStatusCompleted {
		t.Fatalf("finish calls = %+v", finished)
	}
}

func TestExecWorkflowFailureFinishesFailed(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var finished []FinishExecutionInput
	registerExecLifecycle(t, env, &finished)
	env.RegisterWorkflowWithOptions(ExecWorkflow, workflow.RegisterOptions{Name: ExecWorkflowType})
	env.RegisterWorkflowWithOptions(
		func(workflow.Context, map[string]any) (map[string]any, error) {
			return nil, errors.New("implementation failed")
		},
		workflow.RegisterOptions{Name: "demo.failure"},
	)
	env.ExecuteWorkflow(ExecWorkflowType, execInput(KindWorkflow, "demo.failure", `{}`))
	if env.GetWorkflowError() == nil {
		t.Fatal("workflow must fail")
	}
	if len(finished) != 1 || finished[0].Status != types.ExecutionStatusFailed ||
		finished[0].Execution.ID != testExecID || finished[0].Error == "" {
		t.Fatalf("finish calls = %+v", finished)
	}
}

func TestExecWorkflowCancellationFinishesCancelled(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var finished []FinishExecutionInput
	registerExecLifecycle(t, env, &finished)
	env.RegisterWorkflowWithOptions(ExecWorkflow, workflow.RegisterOptions{Name: ExecWorkflowType})
	env.RegisterWorkflowWithOptions(
		func(ctx workflow.Context, _ map[string]any) (map[string]any, error) {
			err := workflow.Await(ctx, func() bool { return false })
			return nil, err
		},
		workflow.RegisterOptions{Name: "demo.cancel"},
	)
	env.RegisterDelayedCallback(env.CancelWorkflow, time.Second)
	env.ExecuteWorkflow(ExecWorkflowType, execInput(KindWorkflow, "demo.cancel", `{}`))
	if !temporal.IsCanceledError(env.GetWorkflowError()) {
		t.Fatalf("workflow error = %v, want cancellation", env.GetWorkflowError())
	}
	if len(finished) != 1 || finished[0].Status != types.ExecutionStatusCancelled ||
		finished[0].Execution.ID != testExecID || finished[0].Error == "" {
		t.Fatalf("finish calls = %+v", finished)
	}
}
