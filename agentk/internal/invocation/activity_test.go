package invocation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	sdkinvocation "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/invocation"
	"go.temporal.io/sdk/temporal"

	"agentk/internal/event"
	"agentk/internal/types"
)

type fakeStore struct {
	batches map[event.BatchID][]event.Draft
}

func (s *fakeStore) AppendEvents(
	_ context.Context,
	_ int64,
	_ types.ExecutionID,
	batchID event.BatchID,
	drafts []event.Draft,
) ([]event.Event, error) {
	if s.batches == nil {
		s.batches = make(map[event.BatchID][]event.Draft)
	}
	if _, exists := s.batches[batchID]; !exists {
		s.batches[batchID] = append([]event.Draft(nil), drafts...)
	}
	return nil, nil
}

func validProgressInput() sdkinvocation.ReportProgressInput {
	return sdkinvocation.ReportProgressInput{
		SessionID:      7,
		InvocationID:   "exe_0191f38d-99f2-7d91-9f2b-712ab18e0a00",
		InvocationKind: sdkinvocation.KindPipeline,
		InvocationName: "summarize",
		StepID:         "clip/0",
		Message:        "done",
		Data:           json.RawMessage(`{"videoUrl":"s3://clip.mp4"}`),
	}
}

func TestReportProgressActivityIsIdempotentByInvocationAndStep(t *testing.T) {
	store := &fakeStore{}
	activities := NewActivities(store)
	input := validProgressInput()

	if err := activities.ReportProgressActivity(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if err := activities.ReportProgressActivity(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if len(store.batches) != 1 {
		t.Fatalf("batches = %+v", store.batches)
	}
	drafts := store.batches[progressBatchID(input.InvocationID, input.StepID)]
	if len(drafts) != 1 {
		t.Fatalf("drafts = %+v", drafts)
	}
	got, ok := drafts[0].Payload.(event.InvocationProgress)
	if !ok {
		t.Fatalf("payload = %T", drafts[0].Payload)
	}
	if got.InvocationID != input.InvocationID ||
		got.InvocationKind != string(input.InvocationKind) ||
		got.InvocationName != input.InvocationName ||
		got.StepID != input.StepID ||
		got.Message != input.Message ||
		string(got.Data) != string(input.Data) {
		t.Fatalf("payload = %+v", got)
	}

	input.StepID = "clip/1"
	if err := activities.ReportProgressActivity(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if len(store.batches) != 2 {
		t.Fatalf("batches = %+v", store.batches)
	}
}

func TestReportProgressActivityAcceptsToolInvocation(t *testing.T) {
	store := &fakeStore{}
	input := validProgressInput()
	input.InvocationKind = sdkinvocation.KindTool
	input.InvocationName = "search_web"

	if err := NewActivities(store).
		ReportProgressActivity(t.Context(), input); err != nil {
		t.Fatal(err)
	}
}

func TestProgressBatchIDFitsEventLogColumn(t *testing.T) {
	first := progressBatchID(strings.Repeat("i", 256), strings.Repeat("s", 256))
	if len(first) > 64 {
		t.Fatalf("batch id length = %d: %q", len(first), first)
	}
	if first != progressBatchID(strings.Repeat("i", 256), strings.Repeat("s", 256)) {
		t.Fatal("batch id is not deterministic")
	}
	if first == progressBatchID(strings.Repeat("i", 256), strings.Repeat("t", 256)) {
		t.Fatal("different steps share a batch id")
	}
}

func TestReportProgressActivityRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		edit func(*sdkinvocation.ReportProgressInput)
	}{
		{name: "session", edit: func(in *sdkinvocation.ReportProgressInput) { in.SessionID = 0 }},
		{name: "id", edit: func(in *sdkinvocation.ReportProgressInput) { in.InvocationID = "" }},
		{name: "kind", edit: func(in *sdkinvocation.ReportProgressInput) { in.InvocationKind = "batch" }},
		{name: "name", edit: func(in *sdkinvocation.ReportProgressInput) { in.InvocationName = "Bad Name" }},
		{name: "step", edit: func(in *sdkinvocation.ReportProgressInput) { in.StepID = "" }},
		{name: "data", edit: func(in *sdkinvocation.ReportProgressInput) { in.Data = json.RawMessage(`{`) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validProgressInput()
			tt.edit(&input)
			err := NewActivities(&fakeStore{}).
				ReportProgressActivity(t.Context(), input)
			assertNonRetryable(t, err)
		})
	}
}

func TestReportProgressActivityRequiresStore(t *testing.T) {
	err := NewActivities(nil).ReportProgressActivity(t.Context(), validProgressInput())
	assertNonRetryable(t, err)
	if !strings.Contains(err.Error(), "store") {
		t.Fatalf("error = %v", err)
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
