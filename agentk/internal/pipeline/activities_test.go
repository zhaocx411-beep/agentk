package pipeline

import (
	"encoding/json"
	"errors"
	"testing"

	"go.temporal.io/sdk/temporal"

	"agentk/internal/deltabuf"
	"agentk/internal/event"
	"agentk/internal/store/memory"
	"agentk/internal/types"
)

func newTestActivities(t *testing.T) (*Activities, types.Session, *memory.Store, *deltabuf.Memory) {
	t.Helper()
	store := memory.New()
	session, err := store.CreateSession(t.Context(), "tenant-test", "pipeline 测试",
		json.RawMessage(`{"system":"s"}`))
	if err != nil {
		t.Fatal(err)
	}
	deltas := deltabuf.NewMemory()
	return NewActivities(store, deltas), session, store, deltas
}

func pipelineInput(session types.Session) WorkflowInput {
	return WorkflowInput{
		Execution: types.ExecutionRef{
			ID: types.NewExecutionID(),
			Session: types.SessionRef{
				SessionID: session.ID, SessionPublicID: session.PublicID,
			},
		},
		Name: "demo", CapabilityID: "contrib.test.demo",
		Args: json.RawMessage(`{"topic":"hi"}`),
	}
}

func TestBeginExecutionActivityIsIdempotent(t *testing.T) {
	activities, session, store, deltas := newTestActivities(t)
	input := pipelineInput(session)
	if err := activities.BeginExecutionActivity(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if err := activities.BeginExecutionActivity(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	events, err := store.LoadSessionEvents(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ExecutionID != input.Execution.ID {
		t.Fatalf("events = %+v", events)
	}
	started, ok := events[0].Payload.(event.ExecutionStarted)
	if !ok || started.ExecutionKind != types.ExecutionKindPipeline ||
		started.Pipeline == nil || started.Pipeline.CapabilityID != input.CapabilityID ||
		started.ProfileVersion == "" {
		t.Fatalf("started = %+v", events[0].Payload)
	}
	stream, err := deltas.Open(t.Context(), input.Execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()
	if item, err := stream.Recv(t.Context()); err != nil || item.EventID != events[0].ID {
		t.Fatalf("buffer item = %+v, %v", item, err)
	}
	bad := WorkflowInput{}
	assertNonRetryable(t, activities.BeginExecutionActivity(t.Context(), bad))
}

func TestFinishExecutionActivityWritesOneTerminalEventAndEndsStream(t *testing.T) {
	activities, session, store, deltas := newTestActivities(t)
	input := pipelineInput(session)
	if err := activities.BeginExecutionActivity(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	finish := FinishExecutionInput{
		Execution: input.Execution,
		Status:    types.ExecutionStatusCompleted,
		Output:    json.RawMessage(`{"ok":true}`),
	}
	if err := activities.FinishExecutionActivity(t.Context(), finish); err != nil {
		t.Fatal(err)
	}
	finish.Status = types.ExecutionStatusFailed
	finish.Error = "late"
	if err := activities.FinishExecutionActivity(t.Context(), finish); err != nil {
		t.Fatal(err)
	}
	events, err := store.LoadSessionEvents(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[1].Payload.Kind() != event.KindExecutionFinished {
		t.Fatalf("events = %+v", events)
	}
	finished := events[1].Payload.(event.ExecutionFinished)
	if finished.Status != types.ExecutionStatusCompleted || finished.Pipeline == nil ||
		string(finished.Pipeline.Output) != `{"ok":true}` {
		t.Fatalf("finished = %+v", finished)
	}
	stream, err := deltas.Open(t.Context(), input.Execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()
	var last deltabuf.Item
	for range 3 {
		last, err = stream.Recv(t.Context())
		if err != nil {
			t.Fatal(err)
		}
	}
	if last.Kind != deltabuf.StreamEndKind {
		t.Fatalf("last stream item = %+v", last)
	}
}

func assertNonRetryable(t *testing.T, err error) {
	t.Helper()
	var appErr *temporal.ApplicationError
	if err == nil || !temporal.IsApplicationError(err) {
		t.Fatalf("err = %v, want application error", err)
	}
	if !errors.As(err, &appErr) || !appErr.NonRetryable() {
		t.Fatalf("err = %v, want non-retryable", err)
	}
}
