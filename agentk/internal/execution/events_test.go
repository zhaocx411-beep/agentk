package execution

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"agentk/internal/event"
	"agentk/internal/types"
)

func TestExecutionIDIsUUIDv7(t *testing.T) {
	id := types.NewExecutionID()
	parsed, err := uuid.Parse(string(id)[len("exe_"):])
	if err != nil || parsed.Version() != 7 {
		t.Fatalf("execution id = %q, parse err = %v", id, err)
	}
}

func TestProjectPipelineExecution(t *testing.T) {
	sess := types.Session{ID: 7, PublicID: "ses_1", TenantID: "org_1"}
	executionID := types.ExecutionID("exe_0191f38d-99f2-7d91-9f2b-712ab18e0a00")
	startedAt := time.Date(2026, 8, 16, 1, 2, 3, 0, time.UTC)
	finishedAt := startedAt.Add(time.Minute)
	events := []event.Event{
		{ExecutionID: executionID, CreatedAt: startedAt, Payload: event.ExecutionStarted{
			ExecutionKind:  types.ExecutionKindPipeline,
			ProfileVersion: "profile-v1",
			Pipeline: &event.PipelineStarted{
				Name: "render", CapabilityID: "contrib.test.render", Args: event.RawJSON(`{"topic":"cat"}`),
			},
		}},
		{ExecutionID: executionID, CreatedAt: finishedAt, Payload: event.ExecutionFinished{
			Status:   types.ExecutionStatusCompleted,
			Pipeline: &event.PipelineFinished{Output: event.RawJSON(`{"uri":"bs://video"}`)},
		}},
	}

	got := Project(sess, events)
	if len(got) != 1 {
		t.Fatalf("executions = %+v", got)
	}
	exec := got[0]
	if exec.ID != executionID || exec.Kind != types.ExecutionKindPipeline ||
		exec.Status != types.ExecutionStatusCompleted || exec.FinishedAt == nil ||
		exec.Pipeline == nil || exec.Pipeline.Name != "render" ||
		exec.Pipeline.CapabilityID != "contrib.test.render" {
		t.Fatalf("execution = %+v", exec)
	}
	var output map[string]string
	if err := json.Unmarshal(exec.Pipeline.Output, &output); err != nil || output["uri"] != "bs://video" {
		t.Fatalf("output = %s, err = %v", exec.Pipeline.Output, err)
	}
}
