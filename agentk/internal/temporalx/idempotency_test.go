package temporalx

import (
	"context"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestActivityIdempotencyKeyUsesTemporalExecution(t *testing.T) {
	type result struct {
		Key        string
		WorkflowID string
		RunID      string
		ActivityID string
	}
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()
	fn := func(ctx context.Context) (result, error) {
		info := activity.GetInfo(ctx)
		return result{
			Key:        ActivityIdempotencyKey(ctx),
			WorkflowID: info.WorkflowExecution.ID,
			RunID:      info.WorkflowExecution.RunID,
			ActivityID: info.ActivityID,
		}, nil
	}
	env.RegisterActivity(fn)
	value, err := env.ExecuteActivity(fn)
	if err != nil {
		t.Fatal(err)
	}
	var got result
	if err := value.Get(&got); err != nil {
		t.Fatal(err)
	}
	want := got.WorkflowID + "/" + got.RunID + "/" + got.ActivityID
	if got.WorkflowID == "" || got.RunID == "" || got.ActivityID == "" {
		t.Fatalf("incomplete activity info: %+v", got)
	}
	if got.Key != want {
		t.Fatalf("key = %q, want %q", got.Key, want)
	}
}
