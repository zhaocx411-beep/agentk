package agentloop

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"agentk/internal/agent"
	"agentk/internal/chat"
	"agentk/internal/deltabuf"
	"agentk/internal/event"
	"agentk/internal/store/memory"
	"agentk/internal/temporalx"
	"agentk/internal/tool"
	"agentk/internal/types"
)

// stubModel 返回预设响应并记录模型调用幂等键。
type stubModel struct {
	blocks []chat.Block
	calls  []chat.ToolCall
	keys   []string
}

func (m *stubModel) Stream(_ context.Context, req chat.Request) (chat.Response, error) {
	m.keys = append(m.keys, req.IdempotencyKey)
	blocks := m.blocks
	calls := m.calls
	return chat.Response{
		Stream: func(yield func(chat.Delta, error) bool) {
			for i, b := range blocks {
				if !yield(chat.Delta{Block: i, Kind: b.Kind, Text: b.Text}, nil) {
					return
				}
			}
		},
		Result: func() (chat.Result, error) {
			return chat.Result{Blocks: blocks, ToolCalls: calls, FinishReason: "stop"}, nil
		},
	}, nil
}

func echoSpec() tool.Spec {
	return tool.Spec{
		Name:        "echo",
		Description: "回声",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Binding: tool.InlineBinding{Execute: func(_ context.Context, input json.RawMessage, _ tool.Context) (any, error) {
			return map[string]string{"echo": string(input)}, nil
		}},
	}
}

func frozenCall(
	t *testing.T,
	spec tool.Spec,
	id string,
	args json.RawMessage,
) ToolCallRef {
	t.Helper()
	registry, err := tool.NewRegistry(spec)
	if err != nil {
		t.Fatal(err)
	}
	refs := toolCallRefs([]chat.ToolCall{{ID: id, Name: spec.Name, Args: args}}, registry)
	if len(refs) != 1 {
		t.Fatalf("refs = %+v", refs)
	}
	return refs[0]
}

type activityFixture struct {
	acts   *Activities
	store  *memory.Store
	deltas *deltabuf.Memory
	model  *stubModel
	ref    types.ExecutionRef
}

func newActivityFixture(t *testing.T, model *stubModel, specs ...tool.Spec) activityFixture {
	t.Helper()
	if len(specs) == 0 {
		specs = []tool.Spec{echoSpec()}
	}
	registry, err := tool.NewRegistry(specs...)
	if err != nil {
		t.Fatal(err)
	}
	store := memory.New()
	sess, err := store.CreateSession(context.Background(), "org_test", "t", []byte(`{"version":"v1"}`))
	if err != nil {
		t.Fatal(err)
	}
	deltas := deltabuf.NewMemory()
	acts := NewActivities(Config{
		Store: store,
		Runtime: agent.StaticRuntime{
			Model: model, Registry: registry, System: "系统提示",
			Version: "v1", ModelName: "test-model", Dialect: event.DialectOpenAI,
		},
		WorkerID: "worker-1",
		Deltas:   deltas,
	})
	ref := types.ExecutionRef{
		ID:      types.NewExecutionID(),
		Session: types.SessionRef{SessionID: sess.ID, SessionPublicID: sess.PublicID},
	}
	if err := deltas.Begin(context.Background(), ref.ID); err != nil {
		t.Fatal(err)
	}
	return activityFixture{
		acts: acts, store: store, deltas: deltas, model: model,
		ref: ref,
	}
}

func (f activityFixture) events(t *testing.T) []event.Event {
	t.Helper()
	events, err := f.store.LoadSessionEvents(context.Background(), f.ref.Session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func (f activityFixture) kinds(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, ev := range f.events(t) {
		out = append(out, string(ev.Payload.Kind()))
	}
	return out
}

func (f activityFixture) appendUser(t *testing.T, text string) {
	t.Helper()
	_, err := f.store.AppendEvents(context.Background(), f.ref.Session.SessionID, f.ref.ID,
		event.BatchID("u:"+text), []event.Draft{{Payload: event.UserInput{Text: text}}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBeginExecutionActivityAppendsTriggerIdempotently(t *testing.T) {
	f := newActivityFixture(t, &stubModel{})
	acts := NewActivities(Config{Store: f.store})
	input := BeginExecutionInput{
		Execution: f.ref,
		Trigger:   AgentTrigger{Message: &event.UserInput{Text: "在吗"}},
	}

	if err := acts.BeginExecutionActivity(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if err := acts.BeginExecutionActivity(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	events := f.events(t)
	if len(events) != 2 {
		t.Fatalf("events = %+v, want one start and one input", events)
	}
	message, ok := events[1].Payload.(event.UserInput)
	if !ok || message.Text != "在吗" {
		t.Fatalf("payload = %+v", events[0].Payload)
	}
}

func TestBeginExecutionActivityAppendsAnswer(t *testing.T) {
	f := newActivityFixture(t, &stubModel{})
	acts := NewActivities(Config{Store: f.store})
	answer := event.AskUserResponded{
		ToolCallID: "call-1",
		Answers:    []event.Answer{{QuestionID: "q1", Selected: []string{"A"}}},
	}
	if err := acts.BeginExecutionActivity(t.Context(), BeginExecutionInput{
		Execution: f.ref, Trigger: AgentTrigger{Answer: &answer},
	}); err != nil {
		t.Fatal(err)
	}
	if got := f.events(t)[1].Payload.(event.AskUserResponded); got.ToolCallID != "call-1" {
		t.Fatalf("answer = %+v", got)
	}
}

func TestBeginExecutionActivityRejectsInvalidTrigger(t *testing.T) {
	f := newActivityFixture(t, &stubModel{})
	acts := NewActivities(Config{Store: f.store})
	message := event.UserInput{Text: "hello"}
	answer := event.AskUserResponded{ToolCallID: "call-1"}
	for _, input := range []BeginExecutionInput{
		{Execution: f.ref},
		{Execution: f.ref, Trigger: AgentTrigger{Message: &message, Answer: &answer}},
	} {
		err := acts.BeginExecutionActivity(t.Context(), input)
		if !temporalx.IsPermanentActivityError(err) {
			t.Fatalf("input = %+v, err = %v, want permanent input error", input, err)
		}
	}
	if events := f.events(t); len(events) != 0 {
		t.Fatalf("invalid inputs wrote events: %+v", events)
	}
}

func TestFinishExecutionActivityWritesOneTerminalEventAndEndsStream(t *testing.T) {
	f := newActivityFixture(t, &stubModel{})
	message := event.UserInput{Text: "hello"}
	if err := f.acts.BeginExecutionActivity(t.Context(), BeginExecutionInput{
		Execution: f.ref, Trigger: AgentTrigger{Message: &message},
	}); err != nil {
		t.Fatal(err)
	}
	finish := FinishExecutionInput{
		Execution: f.ref, Status: types.ExecutionStatusCompleted,
	}
	if err := f.acts.FinishExecutionActivity(t.Context(), finish); err != nil {
		t.Fatal(err)
	}
	finish.Status = types.ExecutionStatusFailed
	finish.Error = "late"
	if err := f.acts.FinishExecutionActivity(t.Context(), finish); err != nil {
		t.Fatal(err)
	}
	events := f.events(t)
	if len(events) != 3 {
		t.Fatalf("events = %+v", events)
	}
	finished, ok := events[2].Payload.(event.ExecutionFinished)
	if !ok || finished.Status != types.ExecutionStatusCompleted || finished.Error != "" {
		t.Fatalf("execution.finished = %+v", events[2].Payload)
	}
	stream, err := f.deltas.Open(t.Context(), f.ref.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()
	for range 3 {
		if _, err := stream.Recv(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	last, err := stream.Recv(t.Context())
	if err != nil || last.Kind != deltabuf.StreamEndKind {
		t.Fatalf("stream end = %+v, %v", last, err)
	}

	finish.Status = types.ExecutionStatusRunning
	if err := f.acts.FinishExecutionActivity(t.Context(), finish); !temporalx.IsPermanentActivityError(err) {
		t.Fatalf("invalid terminal status error = %v", err)
	}
}

// 无需响应时不调用模型。
func TestRoundActivityIdleOnConvergedEventLog(t *testing.T) {
	model := &stubModel{}
	f := newActivityFixture(t, model)

	out, err := f.acts.RoundActivity(context.Background(), RoundInput{
		Execution: f.ref, RoundID: "rnd_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Idle {
		t.Fatalf("out = %+v, want idle on an empty event log", out)
	}
	if len(model.keys) != 0 {
		t.Fatal("idle round must not call the model")
	}
}

// 完整 round 原子追加，模型幂等键由 RoundID 决定。
func TestRoundActivityAppendsWholeRoundAtOnce(t *testing.T) {
	model := &stubModel{blocks: []chat.Block{
		{Kind: chat.BlockReasoning, Text: "想一下"},
		{Kind: chat.BlockText, Text: "你好"},
	}}
	f := newActivityFixture(t, model)
	f.appendUser(t, "在吗")

	if _, err := f.acts.RoundActivity(context.Background(), RoundInput{
		Execution: f.ref, RoundID: "rnd_1",
	}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.kinds(t), ","); got !=
		"user.input,round.started,assistant.reasoning,assistant.text,round.completed" {
		t.Fatalf("events = %s", got)
	}
	if len(model.keys) != 1 || model.keys[0] != agent.ModelInvocationKey("rnd_1") {
		t.Fatalf("idempotency keys = %v", model.keys)
	}
}

// Activity 重试相同 RoundID 时保留首次提交的 round。
func TestAppendIsIdempotentPerBatch(t *testing.T) {
	f := newActivityFixture(t, &stubModel{})
	sess, err := f.store.GetSession(context.Background(), f.ref.Session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	drafts := []event.Draft{
		{RoundID: "rnd_1", Payload: event.RoundStarted{Model: "test-model"}},
		{RoundID: "rnd_1", Payload: event.AssistantText{Block: 0, Text: "第一次的说法"}},
	}
	retry := []event.Draft{
		{RoundID: "rnd_1", Payload: event.RoundStarted{Model: "test-model"}},
		{RoundID: "rnd_1", Payload: event.AssistantText{Block: 0, Text: "重试时换了个说法"}},
	}
	ctx := context.Background()
	if err := f.acts.appendExecutionEvents(ctx, f.ref.ID, sess, "rnd_1", drafts); err != nil {
		t.Fatal(err)
	}
	if err := f.acts.appendExecutionEvents(ctx, f.ref.ID, sess, "rnd_1", retry); err != nil {
		t.Fatal(err)
	}

	events := f.events(t)
	if len(events) != 2 {
		t.Fatalf("event log has %d events, want 2", len(events))
	}
	text, ok := events[1].Payload.(event.AssistantText)
	if !ok || text.Text != "第一次的说法" {
		t.Fatalf("committed payload = %+v, want the first attempt to stand", events[1].Payload)
	}
}

// 缺少 RoundID 返回不可重试错误。
func TestRoundActivityRequiresRoundID(t *testing.T) {
	f := newActivityFixture(t, &stubModel{})
	f.appendUser(t, "在吗")

	_, err := f.acts.RoundActivity(context.Background(), RoundInput{Execution: f.ref})
	if err == nil || !temporalx.IsPermanentActivityError(err) {
		t.Fatalf("err = %v, want a permanent error", err)
	}
}

// Round 完成后 delta 和事件仍保留在 run buffer。
func TestRoundActivityKeepsRunBufferActive(t *testing.T) {
	model := &stubModel{blocks: []chat.Block{{Kind: chat.BlockText, Text: "你好"}}}
	f := newActivityFixture(t, model)
	f.appendUser(t, "在吗")

	if _, err := f.acts.RoundActivity(context.Background(), RoundInput{
		Execution: f.ref, RoundID: "rnd_1",
	}); err != nil {
		t.Fatal(err)
	}

	stream, err := f.deltas.Open(context.Background(), f.ref.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()
	items := make([]deltabuf.Item, 4)
	for i := range items {
		items[i], err = stream.Recv(context.Background())
		if err != nil {
			t.Fatal(err)
		}
	}
	if items[0].Kind != deltabuf.DeltaKind ||
		items[1].Kind != string(event.KindRoundStarted) ||
		items[2].Kind != string(event.KindAssistantText) ||
		items[3].Kind != string(event.KindRoundCompleted) {
		t.Fatalf("run buffer kinds = %q, %q, %q, %q",
			items[0].Kind, items[1].Kind, items[2].Kind, items[3].Kind)
	}
}

// Activity 重试保留旧帧，并为新帧使用不同 Gen。
func TestDeltaSinkAppendsNewGeneration(t *testing.T) {
	ctx := context.Background()
	f := newActivityFixture(t, &stubModel{})
	// 预置上一次 activity 尝试的帧。
	staleRaw, _ := json.Marshal(deltabuf.Frame{
		RoundID: "rnd_1", Gen: "stale", Idx: 0, Kind: "text", Text: "旧",
	})
	if err := f.deltas.Append(ctx, f.ref.ID, deltabuf.Item{
		Kind: deltabuf.DeltaKind, Data: staleRaw,
	}); err != nil {
		t.Fatal(err)
	}

	sink := f.acts.deltaSink(ctx, f.ref.ID)
	sink.Delta(f.ref.Session.SessionPublicID, "rnd_1", chat.Delta{Block: 0, Kind: chat.BlockText, Text: "你"})
	sink.Delta(f.ref.Session.SessionPublicID, "rnd_1", chat.Delta{Block: 0, Kind: chat.BlockText, Text: "好"})

	stream, err := f.deltas.Open(ctx, f.ref.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()
	items := make([]deltabuf.Item, 3)
	for i := range items {
		items[i], err = stream.Recv(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(items) != 3 {
		t.Fatalf("items = %+v, want stale plus 2 new frames", items)
	}
	frames := make([]deltabuf.Frame, len(items))
	for i, item := range items {
		if err := json.Unmarshal(item.Data, &frames[i]); err != nil {
			t.Fatal(err)
		}
	}
	if frames[0].Gen != "stale" || frames[1].Gen == "" || frames[1].Gen != frames[2].Gen {
		t.Fatalf("gen = %q / %q / %q", frames[0].Gen, frames[1].Gen, frames[2].Gen)
	}
	if frames[1].Idx != 0 || frames[2].Idx != 1 ||
		frames[1].Text != "你" || frames[2].Text != "好" {
		t.Fatalf("frames = %+v", frames)
	}
}

// InlineBinding 分别追加开始和结果事件。
func TestExecuteInlineToolActivityAppendsStartedThenResult(t *testing.T) {
	f := newActivityFixture(t, &stubModel{})
	call := ToolCallRef{ToolCallID: "c1", ToolName: "echo", Args: json.RawMessage(`{"x":1}`)}

	if _, err := f.acts.ExecuteInlineToolActivity(context.Background(), ToolCallActivityInput{
		Execution: f.ref, RoundID: "rnd_1", CallIdx: 0, Call: call,
	}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.kinds(t), ","); got != "tool.exec_started,tool.exec_succeeded" {
		t.Fatalf("events = %s", got)
	}
}

// heavySpec 定义 ActivityBinding 及默认模型参数。
func heavySpec() tool.Spec {
	return tool.Spec{
		Name: "heavy", Description: "重", InputSchema: json.RawMessage(`{"type":"object"}`),
		Binding: tool.ActivityBinding{
			ActivityName: "agentk.test.heavy",
		},
	}
}

// Begin 合并默认值和模型参数，并追加开始事件。
func TestBeginToolActivityPassesArgsThrough(t *testing.T) {
	heavy := heavySpec()
	heavy.DefaultArgs = json.RawMessage(`{"quality":"high","model":"default-model"}`)
	f := newActivityFixture(t, &stubModel{}, echoSpec(), heavy)
	f.acts.runtime = nil

	out, err := f.acts.BeginToolActivity(context.Background(), ToolCallActivityInput{
		Execution: f.ref, RoundID: "rnd_1", CallIdx: 0,
		Call: frozenCall(t, heavy, "c1",
			json.RawMessage(`{"prompt":"p","model":"m-1"}`)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Args["prompt"] != "p" || out.Args["model"] != "m-1" ||
		out.Args["quality"] != "high" {
		t.Fatalf("args = %+v (defaults must apply and LLM args must win)", out.Args)
	}
	if got := strings.Join(f.kinds(t), ","); got != "tool.exec_started" {
		t.Fatalf("events = %s", got)
	}

	// 非 ActivityBinding 输入返回不可重试错误。
	if _, err := f.acts.BeginToolActivity(context.Background(), ToolCallActivityInput{
		Execution: f.ref, RoundID: "rnd_1", CallIdx: 1,
		Call: ToolCallRef{ToolCallID: "c2", ToolName: "echo", Args: json.RawMessage(`{}`)},
	}); err == nil || !temporalx.IsPermanentActivityError(err) {
		t.Fatalf("err = %v, want a permanent error", err)
	}
}

// Finish 成功提取 artifacts，并在重试时保持幂等。
func TestFinishToolActivitySuccessExtractsArtifacts(t *testing.T) {
	f := newActivityFixture(t, &stubModel{}, echoSpec(), heavySpec())
	f.acts.runtime = nil
	in := FinishToolInput{
		Execution: f.ref, RoundID: "rnd_1", CallIdx: 0,
		Call:           frozenCall(t, heavySpec(), "c1", json.RawMessage(`{}`)),
		Result:         event.RawJSON(`{"artifacts":[{"type":"image","uri":"http://blob/i.png","description":"测试图片","model":"m-1","params":{"aspectRatio":"1:1"}}]}`),
		DurationMillis: 1200,
	}
	if _, err := f.acts.FinishToolActivity(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if _, err := f.acts.FinishToolActivity(context.Background(), in); err != nil {
		t.Fatal(err) // 重试幂等
	}
	events := f.events(t)
	if len(events) != 1 {
		t.Fatalf("event log has %d events, want 1 (batch key must dedupe retries)", len(events))
	}
	ok, isOK := events[0].Payload.(event.ToolExecSucceeded)
	if !isOK || ok.ToolCallID != "c1" || ok.DurationMillis != 1200 {
		t.Fatalf("payload = %+v", events[0].Payload)
	}
	if len(ok.Artifacts) != 1 || ok.Artifacts[0].URI != "http://blob/i.png" ||
		ok.Artifacts[0].Description != "测试图片" ||
		ok.Artifacts[0].Model != "m-1" || ok.Artifacts[0].Type != "image" {
		t.Fatalf("artifacts = %+v", ok.Artifacts)
	}
}

// Finish 失败生成 ToolExecFailed，不生成资源。
func TestFinishToolActivityFailureAppendsFailed(t *testing.T) {
	f := newActivityFixture(t, &stubModel{}, echoSpec(), heavySpec())
	if _, err := f.acts.FinishToolActivity(context.Background(), FinishToolInput{
		Execution: f.ref, RoundID: "rnd_1", CallIdx: 0,
		Call:  frozenCall(t, heavySpec(), "c1", json.RawMessage(`{}`)),
		Error: "audit rejected",
	}); err != nil {
		t.Fatal(err)
	}
	events := f.events(t)
	if len(events) != 1 {
		t.Fatalf("events = %v", f.kinds(t))
	}
	failed, isFailed := events[0].Payload.(event.ToolExecFailed)
	if !isFailed || failed.Error != "audit rejected" {
		t.Fatalf("payload = %+v", events[0].Payload)
	}
}

// filmSpec 定义 WorkflowBinding。
func filmSpec() tool.Spec {
	return tool.Spec{
		Name: "film", Description: "成片", InputSchema: json.RawMessage(`{"type":"object"}`),
		Binding: tool.WorkflowBinding{
			WorkflowType:     "agentk.test.film",
			TaskQueue:        "film-worker",
			ExecutionTimeout: 2 * time.Hour,
		},
	}
}

// WorkflowBinding 的 Begin 追加开始事件并返回参数。
func TestBeginToolActivityAcceptsWorkflowBinding(t *testing.T) {
	f := newActivityFixture(t, &stubModel{}, echoSpec(), filmSpec())
	out, err := f.acts.BeginToolActivity(context.Background(), ToolCallActivityInput{
		Execution: f.ref, RoundID: "rnd_1", CallIdx: 0,
		Call: frozenCall(t, filmSpec(), "c1", json.RawMessage(`{"topic":"猫"}`)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Args["topic"] != "猫" {
		t.Fatalf("args = %+v", out.Args)
	}
	if got := strings.Join(f.kinds(t), ","); got != "tool.exec_started" {
		t.Fatalf("events = %s", got)
	}
}

// 超限参数不内联到 Workflow History，并从对应 ToolCall 事件回载。
func TestResolveCallReloadsArgsFromEventLog(t *testing.T) {
	f := newActivityFixture(t, &stubModel{})
	big := `{"blob":"` + strings.Repeat("x", 64) + `"}`
	_, err := f.store.AppendEvents(context.Background(), f.ref.Session.SessionID, f.ref.ID, "rnd_1",
		[]event.Draft{{RoundID: "rnd_1", Payload: event.AssistantToolCall{
			ToolCallID: "c1", ToolName: "echo", Args: event.RawJSON(big),
		}}})
	if err != nil {
		t.Fatal(err)
	}

	call, err := f.acts.resolveCall(context.Background(), f.ref.Session.SessionID, "rnd_1",
		ToolCallRef{ToolCallID: "c1", ToolName: "echo", ArgsInEventLog: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(call.Args) != big {
		t.Fatalf("args = %s", call.Args)
	}

	if _, err := f.acts.resolveCall(context.Background(), f.ref.Session.SessionID, "rnd_other",
		ToolCallRef{ToolCallID: "c1", ToolName: "echo", ArgsInEventLog: true}); err == nil {
		t.Fatal("a call missing from the named round must fail")
	}
}

// 不匹配的 Session 引用返回不可重试错误。
func TestLoadSessionRejectsMismatchedRef(t *testing.T) {
	f := newActivityFixture(t, &stubModel{})
	_, err := f.acts.loadSession(context.Background(), types.SessionRef{
		SessionID: f.ref.Session.SessionID, SessionPublicID: "ses_wrong",
	})
	if err == nil {
		t.Fatal("mismatched ref must fail")
	}
	if _, err := f.acts.loadSession(context.Background(), types.SessionRef{}); err == nil {
		t.Fatal("empty ref must fail")
	}
}

// 批键区分 round 内步骤和调用，并在重试时保持稳定。
func TestBatchKeyIsDeterministicPerStep(t *testing.T) {
	seen := map[event.BatchID]string{}
	for _, c := range []struct {
		tag  string
		idx  int
		name string
	}{{"s", 0, "started#0"}, {"t", 0, "result#0"}, {"s", 1, "started#1"}, {"a", 0, "ask#0"}} {
		key := batchKey("rnd_1", c.tag, c.idx)
		if prev, dup := seen[key]; dup {
			t.Fatalf("%s collides with %s on %s", c.name, prev, key)
		}
		seen[key] = c.name
		if key != batchKey("rnd_1", c.tag, c.idx) {
			t.Fatalf("%s is not deterministic", c.name)
		}
	}
	if batchKey("rnd_1", "s", 0) == batchKey("rnd_2", "s", 0) {
		t.Fatal("different rounds must not share a batch key")
	}
}

// toolCallRefs 固定 binding，并对超限参数启用回载。
func TestToolCallRefsClaimCheckAndBinding(t *testing.T) {
	registry, err := tool.NewRegistry(echoSpec(),
		tool.Spec{
			Name: "ask_user", Description: "问", Binding: tool.UserBinding{},
			InputSchema: json.RawMessage(`{"type":"object"}`),
		},
		tool.Spec{
			Name: "heavy", Description: "重", InputSchema: json.RawMessage(`{"type":"object"}`),
			DefaultArgs: json.RawMessage(`{"quality":"high"}`),
			Binding: tool.ActivityBinding{
				ActivityName:        "agentk.test.heavy",
				TaskQueue:           "heavy-worker",
				StartToCloseTimeout: 6 * time.Minute,
				MaxAttempts:         2,
			},
		},
		tool.Spec{
			Name: "film", Description: "成片", InputSchema: json.RawMessage(`{"type":"object"}`),
			Binding: tool.WorkflowBinding{
				WorkflowType:     "agentk.test.film",
				TaskQueue:        "film-worker",
				ExecutionTimeout: 2 * time.Hour,
			},
		})
	if err != nil {
		t.Fatal(err)
	}
	big := json.RawMessage(`{"blob":"` + strings.Repeat("x", maxInlineToolArgs) + `"}`)
	refs := toolCallRefs([]chat.ToolCall{
		{ID: "c1", Name: "echo", Args: json.RawMessage(`{"x":1}`)},
		{ID: "c2", Name: "ask_user", Args: json.RawMessage(`{"questions":[]}`)},
		{ID: "c3", Name: "echo", Args: big},
		{ID: "c4", Name: "heavy", Args: json.RawMessage(`{}`)},
		{ID: "c5", Name: "film", Args: json.RawMessage(`{}`)},
	}, registry)

	if len(refs) != 5 {
		t.Fatalf("refs = %+v", refs)
	}
	if refs[0].Binding != tool.BindInline || refs[0].ArgsInEventLog || string(refs[0].Args) != `{"x":1}` {
		t.Fatalf("inline call = %+v", refs[0])
	}
	if refs[1].Binding != tool.BindUser {
		t.Fatalf("ask_user must be marked as user binding: %+v", refs[1])
	}
	if !refs[2].ArgsInEventLog || refs[2].Args != nil {
		t.Fatalf("oversized args must stay in the event log: len=%d inEventLog=%v",
			len(refs[2].Args), refs[2].ArgsInEventLog)
	}
	if refs[3].Binding != tool.BindActivity || refs[3].Activity == nil ||
		refs[3].Activity.Name != "agentk.test.heavy" ||
		refs[3].Activity.TaskQueue != "heavy-worker" ||
		refs[3].Activity.StartToCloseSeconds != 360 || refs[3].Activity.MaxAttempts != 2 ||
		string(refs[3].DefaultArgs) != `{"quality":"high"}` {
		t.Fatalf("activity call = %+v (dispatch=%+v)", refs[3], refs[3].Activity)
	}
	if refs[4].Binding != tool.BindWorkflow || refs[4].Workflow == nil ||
		refs[4].Workflow.Type != "agentk.test.film" ||
		refs[4].Workflow.TaskQueue != "film-worker" ||
		refs[4].Workflow.ExecutionTimeoutSeconds != 7200 {
		t.Fatalf("workflow call = %+v (dispatch=%+v)", refs[4], refs[4].Workflow)
	}
}

// 缺少必需依赖返回不可重试错误。
func TestActivitiesRejectIncompleteWiring(t *testing.T) {
	ctx := context.Background()
	testSessionRef := types.SessionRef{SessionID: 1, SessionPublicID: "ses_test"}
	if _, err := (&Activities{}).loadSession(ctx, testSessionRef); err == nil {
		t.Fatal("missing store must fail")
	}
	store := memory.New()
	acts := NewActivities(Config{Store: store})
	if _, _, err := acts.resolveSession(ctx, testSessionRef); err == nil {
		t.Fatal("missing runtime must fail")
	}
	acts = NewActivities(Config{Store: store, Runtime: agent.StaticRuntime{}})
	if _, _, err := acts.resolveSession(ctx, testSessionRef); err == nil {
		t.Fatal("missing worker id must fail")
	}
}

// 不完整的解析结果返回不可重试错误。
func TestResolveSessionRequiresModelAndRegistry(t *testing.T) {
	f := newActivityFixture(t, &stubModel{})
	f.acts.runtime = agent.StaticRuntime{ModelName: "test-model"}

	if _, _, err := f.acts.resolveSession(context.Background(), f.ref.Session); err == nil {
		t.Fatal("a runtime resolving to no model must fail")
	}
}

// Deltas 为 nil 时模型调用和事件追加仍正常完成。
func TestNilDeltaBufferIsNoop(t *testing.T) {
	acts := NewActivities(Config{})
	if sink := acts.deltaSink(context.Background(), types.NewExecutionID()); sink != nil {
		t.Fatalf("deltaSink = %v, want nil when no buffer configured", sink)
	}
	if err := acts.appendExecutionEvents(context.Background(), types.NewExecutionID(),
		types.Session{ID: 1, PublicID: "ses_x"}, "b1", nil); err != nil {
		t.Fatal(err)
	}
}
