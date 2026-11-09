package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"agentk/internal/authz"
	"agentk/internal/capcatalog"
	"agentk/internal/event"
	"agentk/internal/pipeline"
	"agentk/internal/store/memory"
	"agentk/internal/types"
)

type testAgentLoop struct {
	store     *memory.Store
	nextBatch int
	fail      bool
	active    bool
	idle      bool
}

func (d *testAgentLoop) append(
	ctx context.Context, sess types.Session, payload event.Payload,
) (types.ExecutionID, error) {
	if d.fail {
		return "", errors.New("agent loop unavailable")
	}
	if d.active {
		return "", types.ErrExecutionActive
	}
	executionID := types.NewExecutionID()
	d.nextBatch++
	_, err := d.store.AppendEvents(ctx, sess.ID, executionID,
		event.BatchID(fmt.Sprintf("service-test-%d", d.nextBatch)),
		[]event.Draft{{Payload: payload}})
	return executionID, err
}

func (d *testAgentLoop) StartMessage(ctx context.Context, sess types.Session, text string) (types.ExecutionID, error) {
	return d.append(ctx, sess, event.UserInput{Text: text})
}

func (d *testAgentLoop) StartAnswer(
	ctx context.Context, sess types.Session, answer event.AskUserResponded,
) (types.ExecutionID, error) {
	return d.append(ctx, sess, answer)
}

func (d *testAgentLoop) Cancel(
	ctx context.Context, sess types.Session,
) error {
	if d.fail {
		return errors.New("agent loop unavailable")
	}
	if d.idle {
		return types.ErrExecutionIdle
	}
	return nil
}

type testProfileRuntime struct{}

func (testProfileRuntime) MaterializeSessionProfile(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, errors.New("session profile is required")
	}
	return append(json.RawMessage(nil), raw...), nil
}

func (testProfileRuntime) ValidateSessionProfile(raw []byte) error {
	if strings.Contains(string(raw), "stale") {
		return errors.New("model no longer in catalog")
	}
	return nil
}

func newTestService() (*Service, *memory.Store, *testAgentLoop) {
	store := memory.New()
	agentLoop := &testAgentLoop{store: store}
	return New(Config{
		Store: store, AgentLoop: agentLoop, Executions: agentLoop,
		ProfileRuntime: testProfileRuntime{},
	}), store, agentLoop
}

func principal(tenant string) authz.Principal {
	return authz.Principal{UserPub: "sp_" + tenant, TenantID: tenant}
}

func TestSessionsAreMaterializedPagedAndTenantScoped(t *testing.T) {
	service, _, _ := newTestService()
	ctx := t.Context()
	a, b := principal("org_a"), principal("org_b")

	if _, err := service.CreateSession(ctx, a, "invalid", nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("create without profile: %v", err)
	}
	first, err := service.CreateSession(ctx, a, "first", json.RawMessage(`{"version":"v1"}`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateSession(ctx, a, "second", json.RawMessage(`{"version":"v1"}`))
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := service.CreateSession(ctx, b, "foreign", json.RawMessage(`{"version":"v1"}`))
	if err != nil {
		t.Fatal(err)
	}

	page, err := service.ListSessions(ctx, a, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Sessions) != 1 || page.Sessions[0].PublicID != second.PublicID || !page.HasMore {
		t.Fatalf("first page = %+v", page)
	}
	page, err = service.ListSessions(ctx, a, second.PublicID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Sessions) != 1 || page.Sessions[0].PublicID != first.PublicID || page.HasMore {
		t.Fatalf("second page = %+v", page)
	}
	if _, err := service.ListSessions(ctx, a, foreign.PublicID, 10); !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("foreign cursor error = %v", err)
	}
}

func TestMessageAnswersAndCancelUseSessionApplicationRules(t *testing.T) {
	service, store, driver := newTestService()
	ctx := t.Context()
	p := principal("org_a")

	messageSession, err := store.CreateSession(ctx, p.TenantID, "message", []byte(`{"version":"v1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SubmitMessage(ctx, p, messageSession.PublicID, "  "); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("blank message error = %v", err)
	}
	if _, err := service.SubmitMessage(ctx, p, messageSession.PublicID, "hello"); err != nil {
		t.Fatal(err)
	}
	events, err := store.LoadSessionEvents(ctx, messageSession.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Payload.(event.UserInput).Text != "hello" {
		t.Fatalf("message events = %+v", events)
	}

	stale, err := store.CreateSession(ctx, p.TenantID, "stale", []byte(`{"stale":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SubmitMessage(ctx, p, stale.PublicID, "hello"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale profile error = %v", err)
	}

	answerSession, err := store.CreateSession(ctx, p.TenantID, "answer", []byte(`{"version":"v1"}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.AppendEvents(ctx, answerSession.ID, types.NewExecutionID(), "ask", []event.Draft{{
		Payload: event.AskUserRequested{
			ToolCallID: "call-1",
			Questions: []event.Question{{
				ID: "q1", Question: "Which?", Options: []string{"A", "B"},
			}},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SubmitAnswers(ctx, p, answerSession.PublicID, "call-1",
		[]event.Answer{{QuestionID: "q1", Selected: []string{"A"}}}); err != nil {
		t.Fatal(err)
	}
	answerEvents, err := store.LoadSessionEvents(ctx, answerSession.ID)
	if err != nil {
		t.Fatal(err)
	}
	response := answerEvents[len(answerEvents)-1].Payload.(event.AskUserResponded)
	if response.Answers[0].Question != "Which?" {
		t.Fatalf("normalized answer = %+v", response.Answers[0])
	}
	if _, err := service.SubmitAnswers(ctx, p, answerSession.PublicID, "call-1",
		[]event.Answer{{QuestionID: "q1", Selected: []string{"B"}}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate answer error = %v", err)
	}
	if err := service.CancelSession(ctx, p, messageSession.PublicID); err != nil {
		t.Fatal(err)
	}

	driver.fail = true
	if _, err := service.SubmitMessage(ctx, p, messageSession.PublicID, "again"); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("agent loop failure = %v", err)
	}
}

func TestActiveAgentLoopRejectsInputWithoutWritingEventLog(t *testing.T) {
	service, store, agentLoop := newTestService()
	ctx := t.Context()
	p := principal("org_a")
	sess, err := store.CreateSession(ctx, p.TenantID, "busy", []byte(`{"version":"v1"}`))
	if err != nil {
		t.Fatal(err)
	}
	agentLoop.active = true

	_, err = service.SubmitMessage(ctx, p, sess.PublicID, "not queued")
	if !errors.Is(err, ErrConflict) || !errors.Is(err, types.ErrExecutionActive) {
		t.Fatalf("active loop error = %v", err)
	}
	events, err := store.LoadSessionEvents(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("conflicting input wrote events: %+v", events)
	}
}

func TestIdleAgentLoopRejectsCancelWithoutWritingEventLog(t *testing.T) {
	service, store, agentLoop := newTestService()
	ctx := t.Context()
	p := principal("org_a")
	sess, err := store.CreateSession(ctx, p.TenantID, "idle", []byte(`{"version":"v1"}`))
	if err != nil {
		t.Fatal(err)
	}
	agentLoop.idle = true

	err = service.CancelSession(ctx, p, sess.PublicID)
	if !errors.Is(err, ErrConflict) || !errors.Is(err, types.ErrExecutionIdle) {
		t.Fatalf("idle cancel error = %v", err)
	}
	events, err := store.LoadSessionEvents(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("idle cancel wrote events: %+v", events)
	}
}

type testPipelineRunner struct {
	store  *memory.Store
	active bool
}

func (r *testPipelineRunner) List(context.Context, types.Session) ([]pipeline.ListItem, error) {
	return []pipeline.ListItem{{Name: "render", CapabilityID: "contrib.test.render"}}, nil
}

func (r *testPipelineRunner) Start(
	ctx context.Context, sess types.Session, name string, args json.RawMessage,
) (types.Execution, error) {
	if name == "missing" {
		return types.Execution{}, fmt.Errorf("pipeline: %w", types.ErrNotFound)
	}
	if r.active {
		return types.Execution{}, types.ErrExecutionActive
	}
	now := time.Now()
	executionID := types.ExecutionID("exe_0191f38d-99f2-7d91-9f2b-712ab18e0a00")
	started := event.ExecutionStarted{
		ExecutionKind:  types.ExecutionKindPipeline,
		ProfileVersion: "v1",
		Pipeline: &event.PipelineStarted{
			Name: name, CapabilityID: "contrib.test.render", Args: event.RawJSON(args),
		},
	}
	completed := event.ExecutionFinished{
		Status:   types.ExecutionStatusCompleted,
		Pipeline: &event.PipelineFinished{Output: event.RawJSON(`{"ok":true}`)},
	}
	if _, err := r.store.AppendEvents(ctx, sess.ID, executionID, "pipeline-start",
		[]event.Draft{{Payload: started}}); err != nil {
		return types.Execution{}, err
	}
	if _, err := r.store.AppendEvents(ctx, sess.ID, executionID, "pipeline-finish",
		[]event.Draft{{Payload: completed}}); err != nil {
		return types.Execution{}, err
	}
	return types.Execution{
		ID: executionID, SessionID: sess.ID, SessionPublicID: sess.PublicID,
		TenantID: sess.TenantID, Kind: types.ExecutionKindPipeline,
		Pipeline: &types.PipelineExecution{
			Name: name, CapabilityID: "contrib.test.render", Args: args,
		},
		Status: types.ExecutionStatusRunning, CreatedAt: now,
	}, nil
}

type testCapabilities struct {
	err error
}

func (c testCapabilities) ListCapabilities(context.Context) ([]capcatalog.ResolvedCapability, error) {
	if c.err != nil {
		return nil, c.err
	}
	return []capcatalog.ResolvedCapability{{}}, nil
}

func TestPipelineProjectionCapabilitiesAndFeatureErrors(t *testing.T) {
	base, store, driver := newTestService()
	p := principal("org_a")
	ctx := t.Context()
	sess, err := store.CreateSession(ctx, p.TenantID, "pipeline", []byte(`{"version":"v1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.ListPipelines(ctx, p, sess.PublicID); !errors.Is(err, ErrFeatureDisabled) {
		t.Fatalf("disabled pipelines error = %v", err)
	}
	if _, err := base.ListCapabilities(ctx, p); !errors.Is(err, ErrFeatureDisabled) {
		t.Fatalf("disabled capabilities error = %v", err)
	}

	pipelineRunner := &testPipelineRunner{store: store}
	service := New(Config{
		Store: store, AgentLoop: driver, Executions: driver,
		ProfileRuntime: testProfileRuntime{},
		Pipelines:      pipelineRunner, Capabilities: testCapabilities{},
	})
	items, err := service.ListPipelines(ctx, p, sess.PublicID)
	if err != nil || len(items) != 1 || items[0].Name != "render" {
		t.Fatalf("pipelines = %+v, err = %v", items, err)
	}
	if _, err := service.StartPipeline(ctx, p, sess.PublicID, "missing", nil); !errors.Is(err, ErrOperationNotFound) {
		t.Fatalf("missing pipeline error = %v", err)
	}
	pipelineRunner.active = true
	if _, err := service.StartPipeline(ctx, p, sess.PublicID, "render", json.RawMessage(`{}`)); !errors.Is(err, ErrConflict) {
		t.Fatalf("active execution error = %v", err)
	}
	pipelineRunner.active = false
	if _, err := service.StartPipeline(ctx, p, sess.PublicID, "render", json.RawMessage(`{"x":1}`)); err != nil {
		t.Fatal(err)
	}
	executions, err := service.ListExecutions(ctx, p, sess.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if len(executions) != 1 || executions[0].Kind != types.ExecutionKindPipeline ||
		executions[0].Status != types.ExecutionStatusCompleted {
		t.Fatalf("executions = %+v", executions)
	}
	capabilities, err := service.ListCapabilities(ctx, p)
	if err != nil || len(capabilities) != 1 {
		t.Fatalf("capabilities = %+v, err = %v", capabilities, err)
	}
	failing := New(Config{
		Store: store, AgentLoop: driver, Executions: driver, ProfileRuntime: testProfileRuntime{},
		Capabilities: testCapabilities{err: errors.New("catalog unavailable")},
	})
	if _, err := failing.ListCapabilities(ctx, p); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("capability dependency error = %v", err)
	}
}
