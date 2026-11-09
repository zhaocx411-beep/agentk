package agentloop

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"agentk/internal/event"
	"agentk/internal/tool"
	"agentk/internal/types"
)

var testExecution = types.ExecutionRef{
	ID:      "exe_0191f38d-99f2-7d91-9f2b-712ab18e0a00",
	Session: types.SessionRef{SessionID: 7, SessionPublicID: "ses_test"},
}

type workflowHarness struct {
	env            *testsuite.TestWorkflowEnvironment
	beginExecution func(context.Context, BeginExecutionInput) error
	rounds         func(context.Context, RoundInput) (RoundOutput, error)
	inline         func(context.Context, ToolCallActivityInput) (ExecuteInlineToolOutput, error)
	begin          func(context.Context, ToolCallActivityInput) (BeginToolOutput, error)
	finish         func(context.Context, FinishToolInput) (FinishToolOutput, error)
	ask            func(context.Context, AskUserInput) (AskUserOutput, error)
	finished       []FinishExecutionInput
}

func newWorkflowHarness(t *testing.T) *workflowHarness {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	h := &workflowHarness{env: suite.NewTestWorkflowEnvironment()}
	h.beginExecution = func(context.Context, BeginExecutionInput) error { return nil }
	h.rounds = func(context.Context, RoundInput) (RoundOutput, error) {
		return RoundOutput{}, nil
	}
	h.inline = func(context.Context, ToolCallActivityInput) (ExecuteInlineToolOutput, error) {
		return ExecuteInlineToolOutput{}, nil
	}
	h.begin = func(context.Context, ToolCallActivityInput) (BeginToolOutput, error) {
		return BeginToolOutput{Args: map[string]any{}}, nil
	}
	h.finish = func(context.Context, FinishToolInput) (FinishToolOutput, error) {
		return FinishToolOutput{}, nil
	}
	h.ask = func(context.Context, AskUserInput) (AskUserOutput, error) {
		return AskUserOutput{AwaitingUser: true}, nil
	}
	h.env.RegisterActivityWithOptions(
		func(ctx context.Context, input BeginExecutionInput) error { return h.beginExecution(ctx, input) },
		activity.RegisterOptions{Name: BeginExecutionActivityName})
	h.env.RegisterActivityWithOptions(
		func(ctx context.Context, input RoundInput) (RoundOutput, error) {
			return h.rounds(ctx, input)
		}, activity.RegisterOptions{Name: RoundActivityName})
	h.env.RegisterActivityWithOptions(
		func(ctx context.Context, input ToolCallActivityInput) (ExecuteInlineToolOutput, error) {
			return h.inline(ctx, input)
		}, activity.RegisterOptions{Name: ExecuteInlineToolActivityName})
	h.env.RegisterActivityWithOptions(
		func(ctx context.Context, input ToolCallActivityInput) (BeginToolOutput, error) {
			return h.begin(ctx, input)
		}, activity.RegisterOptions{Name: BeginToolActivityName})
	h.env.RegisterActivityWithOptions(
		func(ctx context.Context, input FinishToolInput) (FinishToolOutput, error) {
			return h.finish(ctx, input)
		}, activity.RegisterOptions{Name: FinishToolActivityName})
	h.env.RegisterActivityWithOptions(
		func(ctx context.Context, input AskUserInput) (AskUserOutput, error) {
			return h.ask(ctx, input)
		}, activity.RegisterOptions{Name: AskUserActivityName})
	h.env.RegisterActivityWithOptions(
		func(_ context.Context, input FinishExecutionInput) error {
			h.finished = append(h.finished, input)
			return nil
		}, activity.RegisterOptions{Name: FinishExecutionActivityName})
	return h
}

func (h *workflowHarness) execute() {
	h.env.ExecuteWorkflow(AgentWorkflow, AgentWorkflowInput{
		Execution: testExecution,
		Trigger:   AgentTrigger{Message: &event.UserInput{Text: "hello"}},
	})
}

func TestAgentWorkflowCompletesAfterOneTextRound(t *testing.T) {
	h := newWorkflowHarness(t)
	var calls int
	h.rounds = func(_ context.Context, input RoundInput) (RoundOutput, error) {
		calls++
		if input.Execution != testExecution || input.RoundID == "" {
			t.Fatalf("round input = %+v", input)
		}
		return RoundOutput{}, nil
	}
	h.execute()
	if err := h.env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(h.finished) != 1 ||
		h.finished[0].Status != types.ExecutionStatusCompleted {
		t.Fatalf("calls=%d finished=%+v", calls, h.finished)
	}
}

func TestAgentWorkflowDoesNotRunRoundWhenBeginFails(t *testing.T) {
	h := newWorkflowHarness(t)
	h.beginExecution = func(context.Context, BeginExecutionInput) error {
		return temporal.NewNonRetryableApplicationError("event log unavailable", "EventLogError", nil)
	}
	var rounds int
	h.rounds = func(context.Context, RoundInput) (RoundOutput, error) {
		rounds++
		return RoundOutput{}, nil
	}
	h.execute()
	if h.env.GetWorkflowError() == nil || rounds != 0 {
		t.Fatalf("workflow error = %v, rounds = %d", h.env.GetWorkflowError(), rounds)
	}
	if len(h.finished) != 1 || h.finished[0].Status != types.ExecutionStatusFailed {
		t.Fatalf("finished = %+v", h.finished)
	}
}

func TestAgentWorkflowAskUserCompletesExecution(t *testing.T) {
	h := newWorkflowHarness(t)
	h.rounds = func(context.Context, RoundInput) (RoundOutput, error) {
		return RoundOutput{ToolCalls: []ToolCallRef{{
			ToolCallID: "call-1", ToolName: tool.AskUserToolName, Binding: tool.BindUser,
		}}}, nil
	}
	h.execute()
	if err := h.env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	if len(h.finished) != 1 || h.finished[0].Status != types.ExecutionStatusCompleted {
		t.Fatalf("finished = %+v", h.finished)
	}
}

func TestAgentWorkflowFailureFinishesFailed(t *testing.T) {
	h := newWorkflowHarness(t)
	h.rounds = func(context.Context, RoundInput) (RoundOutput, error) {
		return RoundOutput{}, temporal.NewNonRetryableApplicationError("model down", "ModelUnavailable", nil)
	}
	h.execute()
	if h.env.GetWorkflowError() == nil {
		t.Fatal("workflow must fail")
	}
	if len(h.finished) != 1 || h.finished[0].Status != types.ExecutionStatusFailed ||
		h.finished[0].Error == "" {
		t.Fatalf("finished = %+v", h.finished)
	}
}

func TestAgentWorkflowCancellationFinishesCancelled(t *testing.T) {
	h := newWorkflowHarness(t)
	h.rounds = func(ctx context.Context, _ RoundInput) (RoundOutput, error) {
		<-ctx.Done()
		return RoundOutput{}, ctx.Err()
	}
	h.env.RegisterDelayedCallback(h.env.CancelWorkflow, time.Second)
	h.execute()
	if !temporal.IsCanceledError(h.env.GetWorkflowError()) {
		t.Fatalf("workflow error = %v", h.env.GetWorkflowError())
	}
	if len(h.finished) != 1 || h.finished[0].Status != types.ExecutionStatusCancelled {
		t.Fatalf("finished = %+v", h.finished)
	}
}

func TestAgentWorkflowEnforcesToolRoundLimit(t *testing.T) {
	h := newWorkflowHarness(t)
	h.rounds = func(context.Context, RoundInput) (RoundOutput, error) {
		return RoundOutput{
			ToolCalls:     []ToolCallRef{{ToolCallID: "c1", ToolName: "echo", Binding: tool.BindInline}},
			MaxToolRounds: 1,
		}, nil
	}
	h.execute()
	if h.env.GetWorkflowError() == nil {
		t.Fatal("workflow must fail at the tool round limit")
	}
	if len(h.finished) != 1 || h.finished[0].Status != types.ExecutionStatusFailed {
		t.Fatalf("finished = %+v", h.finished)
	}
}

func TestExecuteToolCallsStartsSameRoundInlineToolsConcurrently(t *testing.T) {
	h := newWorkflowHarness(t)
	var (
		mu      sync.Mutex
		started int
		both    = make(chan struct{})
		once    sync.Once
	)
	h.inline = func(ctx context.Context, _ ToolCallActivityInput) (ExecuteInlineToolOutput, error) {
		mu.Lock()
		started++
		if started == 2 {
			once.Do(func() { close(both) })
		}
		mu.Unlock()
		select {
		case <-both:
			return ExecuteInlineToolOutput{}, nil
		case <-ctx.Done():
			return ExecuteInlineToolOutput{}, ctx.Err()
		}
	}
	h.rounds = func(context.Context, RoundInput) (RoundOutput, error) {
		return RoundOutput{ToolCalls: []ToolCallRef{
			{ToolCallID: "c1", ToolName: "one", Binding: tool.BindInline},
			{ToolCallID: "c2", ToolName: "two", Binding: tool.BindInline},
		}, MaxToolRounds: 1}, nil
	}
	h.execute()
	if err := h.env.GetWorkflowError(); err == nil {
		// MaxToolRounds=1 intentionally terminates after both tool Activities complete.
		t.Fatal("workflow should stop at the tool round limit")
	}
	mu.Lock()
	defer mu.Unlock()
	if started != 2 {
		t.Fatalf("started = %d, want 2", started)
	}
}

func scriptedRoundOutputs(outputs ...RoundOutput) func(context.Context, RoundInput) (RoundOutput, error) {
	var (
		mu    sync.Mutex
		index int
	)
	return func(context.Context, RoundInput) (RoundOutput, error) {
		mu.Lock()
		defer mu.Unlock()
		if index >= len(outputs) {
			return RoundOutput{Idle: true}, nil
		}
		out := outputs[index]
		index++
		return out, nil
	}
}

func activityToolCall(activityName string) ToolCallRef {
	return ToolCallRef{
		ToolCallID: "call-activity", ToolName: "heavy", Binding: tool.BindActivity,
		Args: json.RawMessage(`{"prompt":"p"}`),
		Activity: &ActivityDispatch{
			Name: activityName, StartToCloseSeconds: 60, MaxAttempts: 1,
		},
	}
}

func workflowToolCall(workflowType string) ToolCallRef {
	return ToolCallRef{
		ToolCallID: "call-workflow", ToolName: "film", Binding: tool.BindWorkflow,
		Args:     json.RawMessage(`{"topic":"cat"}`),
		Workflow: &WorkflowDispatch{Type: workflowType},
	}
}

func TestAgentWorkflowActivityToolPersistsSuccessAndFailure(t *testing.T) {
	for _, tc := range []struct {
		name      string
		dispatch  func(context.Context, map[string]any) (json.RawMessage, error)
		wantError string
	}{
		{
			name: "success",
			dispatch: func(context.Context, map[string]any) (json.RawMessage, error) {
				return json.RawMessage(`{"ok":true}`), nil
			},
		},
		{
			name: "failure",
			dispatch: func(context.Context, map[string]any) (json.RawMessage, error) {
				return nil, temporal.NewNonRetryableApplicationError("upstream rejected", "Rejected", nil)
			},
			wantError: "upstream rejected",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const activityName = "agentk.test.activity_tool"
			h := newWorkflowHarness(t)
			h.rounds = scriptedRoundOutputs(
				RoundOutput{ToolCalls: []ToolCallRef{activityToolCall(activityName)}},
				RoundOutput{},
			)
			h.begin = func(context.Context, ToolCallActivityInput) (BeginToolOutput, error) {
				return BeginToolOutput{Args: map[string]any{"prompt": "p"}}, nil
			}
			var finishes []FinishToolInput
			h.finish = func(_ context.Context, input FinishToolInput) (FinishToolOutput, error) {
				finishes = append(finishes, input)
				return FinishToolOutput{}, nil
			}
			h.env.RegisterActivityWithOptions(tc.dispatch,
				activity.RegisterOptions{Name: activityName})
			h.execute()
			if err := h.env.GetWorkflowError(); err != nil {
				t.Fatal(err)
			}
			if len(finishes) != 1 {
				t.Fatalf("finishes = %+v", finishes)
			}
			if tc.wantError == "" {
				if finishes[0].Error != "" || !strings.Contains(string(finishes[0].Result), `"ok":true`) {
					t.Fatalf("finish = %+v", finishes[0])
				}
			} else if !strings.Contains(finishes[0].Error, tc.wantError) || len(finishes[0].Result) != 0 {
				t.Fatalf("finish = %+v", finishes[0])
			}
		})
	}
}

func TestAgentWorkflowWorkflowToolPersistsSuccessFailureAndTimeout(t *testing.T) {
	for _, tc := range []struct {
		name      string
		child     func(workflow.Context, map[string]any) (json.RawMessage, error)
		wantError string
	}{
		{
			name: "success",
			child: func(workflow.Context, map[string]any) (json.RawMessage, error) {
				return json.RawMessage(`{"uri":"bs://film"}`), nil
			},
		},
		{
			name: "failure",
			child: func(workflow.Context, map[string]any) (json.RawMessage, error) {
				return nil, temporal.NewNonRetryableApplicationError("render failed", "RenderFailed", nil)
			},
			wantError: "render failed",
		},
		{
			name: "timeout",
			child: func(workflow.Context, map[string]any) (json.RawMessage, error) {
				return nil, temporal.NewTimeoutError(enumspb.TIMEOUT_TYPE_START_TO_CLOSE, nil)
			},
			wantError: "timeout",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const workflowType = "agentk.test.workflow_tool"
			h := newWorkflowHarness(t)
			h.rounds = scriptedRoundOutputs(
				RoundOutput{ToolCalls: []ToolCallRef{workflowToolCall(workflowType)}},
				RoundOutput{},
			)
			h.begin = func(context.Context, ToolCallActivityInput) (BeginToolOutput, error) {
				return BeginToolOutput{Args: map[string]any{"topic": "cat"}}, nil
			}
			var finishes []FinishToolInput
			h.finish = func(_ context.Context, input FinishToolInput) (FinishToolOutput, error) {
				finishes = append(finishes, input)
				return FinishToolOutput{}, nil
			}
			h.env.RegisterWorkflowWithOptions(tc.child,
				workflow.RegisterOptions{Name: workflowType})
			h.execute()
			if err := h.env.GetWorkflowError(); err != nil {
				t.Fatal(err)
			}
			if len(finishes) != 1 {
				t.Fatalf("finishes = %+v", finishes)
			}
			if tc.wantError == "" {
				if finishes[0].Error != "" || !strings.Contains(string(finishes[0].Result), "bs://film") {
					t.Fatalf("finish = %+v", finishes[0])
				}
			} else if !strings.Contains(strings.ToLower(finishes[0].Error), tc.wantError) {
				t.Fatalf("finish = %+v", finishes[0])
			}
		})
	}
}

func TestAgentWorkflowCancellationPropagatesThroughWorkflowTool(t *testing.T) {
	const workflowType = "agentk.test.cancelled_workflow_tool"
	h := newWorkflowHarness(t)
	h.rounds = scriptedRoundOutputs(RoundOutput{
		ToolCalls: []ToolCallRef{workflowToolCall(workflowType)},
	})
	h.begin = func(context.Context, ToolCallActivityInput) (BeginToolOutput, error) {
		return BeginToolOutput{Args: map[string]any{}}, nil
	}
	var finishes []FinishToolInput
	h.finish = func(_ context.Context, input FinishToolInput) (FinishToolOutput, error) {
		finishes = append(finishes, input)
		return FinishToolOutput{}, nil
	}
	h.env.RegisterWorkflowWithOptions(
		func(ctx workflow.Context, _ map[string]any) (json.RawMessage, error) {
			if err := workflow.Sleep(ctx, time.Hour); err != nil {
				return nil, err
			}
			return json.RawMessage(`{}`), nil
		}, workflow.RegisterOptions{Name: workflowType})
	h.env.RegisterDelayedCallback(h.env.CancelWorkflow, time.Second)
	h.execute()
	if !temporal.IsCanceledError(h.env.GetWorkflowError()) {
		t.Fatalf("workflow error = %v", h.env.GetWorkflowError())
	}
	if len(finishes) != 0 {
		t.Fatalf("cancelled child was recorded as tool failure: %+v", finishes)
	}
	if len(h.finished) != 1 || h.finished[0].Status != types.ExecutionStatusCancelled {
		t.Fatalf("execution finish = %+v", h.finished)
	}
}

func TestBoundedDetail(t *testing.T) {
	long := make([]byte, maxExecutionErrorDetail+10)
	for i := range long {
		long[i] = 'x'
	}
	got := boundedDetail(string(long))
	if len(got) != maxExecutionErrorDetail+len("...(truncated)") {
		t.Fatalf("bounded detail length = %d", len(got))
	}
}
