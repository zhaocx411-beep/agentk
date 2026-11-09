package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"agentk/internal/chat"
	"agentk/internal/event"
	"agentk/internal/tool"
)

type fakeModel struct {
	req       chat.Request
	blocks    []chat.Block
	calls     []chat.ToolCall
	usage     chat.Usage
	finish    string
	streamErr error
	callErr   error
	resultErr error
}

func (m *fakeModel) Stream(_ context.Context, req chat.Request) (chat.Response, error) {
	m.req = req
	if m.callErr != nil {
		return chat.Response{}, m.callErr
	}
	blocks, streamErr, resultErr := m.blocks, m.streamErr, m.resultErr
	calls, usage, finish := m.calls, m.usage, m.finish
	return chat.Response{
		Stream: func(yield func(chat.Delta, error) bool) {
			for i, b := range blocks {
				if !yield(chat.Delta{Block: i, Kind: b.Kind, Text: b.Text}, nil) {
					return
				}
			}
			if streamErr != nil {
				yield(chat.Delta{}, streamErr)
			}
		},
		Result: func() (chat.Result, error) {
			if resultErr != nil {
				return chat.Result{}, resultErr
			}
			return chat.Result{
				Blocks: blocks, ToolCalls: calls, Usage: usage, FinishReason: finish,
			}, nil
		},
	}, nil
}

type recordingSink struct{ deltas []chat.Delta }

func (s *recordingSink) Delta(_ string, _ event.RoundID, d chat.Delta) {
	s.deltas = append(s.deltas, d)
}

func testResolved(t *testing.T, model chat.Client, specs ...tool.Spec) Resolved {
	t.Helper()
	if len(specs) == 0 {
		specs = []tool.Spec{{
			Name: "echo", Description: "回声",
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Binding: tool.InlineBinding{Execute: func(context.Context, json.RawMessage, tool.Context) (any, error) {
				return map[string]string{"ok": "1"}, nil
			}},
		}}
	}
	registry, err := tool.NewRegistry(specs...)
	if err != nil {
		t.Fatal(err)
	}
	return Resolved{
		Model: model, Registry: registry, System: "系统提示",
		Version: "v1", ModelName: "test-model", Dialect: event.DialectOpenAI,
	}
}

func testRoundRef(roundID event.RoundID) RoundRef {
	return RoundRef{
		SessionPublicID: "ses_1", RoundID: roundID, WorkerID: "worker-1",
	}
}

func kinds(drafts []event.Draft) []string {
	out := make([]string, 0, len(drafts))
	for _, d := range drafts {
		out = append(out, string(d.Payload.Kind()))
	}
	return out
}

// 一个 round 的事件顺序固定，并共享 RoundID。
func TestCallModelShapesRoundDrafts(t *testing.T) {
	model := &fakeModel{
		blocks: []chat.Block{
			{Kind: chat.BlockReasoning, Text: "先想想"},
			{Kind: chat.BlockText, Text: "这就查"},
		},
		calls:  []chat.ToolCall{{ID: "c1", Name: "echo", Args: json.RawMessage(`{"x":1}`)}},
		usage:  chat.Usage{InputTokens: 10, OutputTokens: 3, CacheReadTokens: 8},
		finish: "tool_calls",
	}
	sink := &recordingSink{}

	round, err := CallModel(context.Background(), testResolved(t, model), testRoundRef("rnd_1"), nil, sink)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"round.started", "assistant.reasoning", "assistant.text",
		"assistant.tool_call", "round.completed",
	}
	got := kinds(round.Drafts)
	if len(got) != len(want) {
		t.Fatalf("draft kinds = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("draft kinds = %v, want %v", got, want)
		}
	}
	for _, d := range round.Drafts {
		if d.RoundID != "rnd_1" {
			t.Fatalf("draft %s carries round %q", d.Payload.Kind(), d.RoundID)
		}
	}
	if len(round.Calls) != 1 || round.Calls[0].ID != "c1" {
		t.Fatalf("calls = %+v", round.Calls)
	}
	if len(sink.deltas) != 2 {
		t.Fatalf("deltas = %+v", sink.deltas)
	}
}

func TestCallModelAppliesProfileToolDefaultsBeforeEventLog(t *testing.T) {
	model := &fakeModel{
		calls: []chat.ToolCall{{
			ID: "c1", Name: "video",
			Args: json.RawMessage(`{"resolution":"720p","prompt":"run"}`),
		}},
		finish: "tool_calls",
	}
	spec, err := tool.WithDefaults(tool.Spec{
		Name: "video",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"resolution":{"type":"string","enum":["720p","1080p"]},
				"audio":{"type":"boolean"},
				"prompt":{"type":"string"}
			}
		}`),
		Binding: tool.InlineBinding{},
	}, map[string]any{"resolution": "1080p", "audio": true})
	if err != nil {
		t.Fatal(err)
	}
	round, err := CallModel(context.Background(), testResolved(t, model, spec),
		testRoundRef("rnd_defaults"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var effective map[string]any
	if err := json.Unmarshal(round.Calls[0].Args, &effective); err != nil {
		t.Fatal(err)
	}
	if effective["resolution"] != "720p" || effective["audio"] != true {
		t.Fatalf("effective args = %#v", effective)
	}
	callEvent, ok := round.Drafts[1].Payload.(event.AssistantToolCall)
	if !ok {
		t.Fatalf("draft = %T, want assistant.tool_call", round.Drafts[1].Payload)
	}
	if string(callEvent.Args) != string(round.Calls[0].Args) {
		t.Fatalf("event log args = %s, dispatch args = %s", callEvent.Args, round.Calls[0].Args)
	}
	if !strings.Contains(string(model.req.Tools[0].InputSchema), `"default":"1080p"`) {
		t.Fatalf("model schema = %s", model.req.Tools[0].InputSchema)
	}
}

// RoundStarted 保存模型、Dialect、Profile 版本和 prompt 摘要。
func TestCallModelSnapshotsRoundConfig(t *testing.T) {
	model := &fakeModel{blocks: []chat.Block{{Kind: chat.BlockText, Text: "好"}}}
	res := testResolved(t, model)

	round, err := CallModel(context.Background(), res, testRoundRef("rnd_1"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	started, ok := round.Drafts[0].Payload.(event.RoundStarted)
	if !ok {
		t.Fatalf("first draft = %T, want round.started", round.Drafts[0].Payload)
	}
	if started.Model != "test-model" || started.Dialect != event.DialectOpenAI {
		t.Fatalf("snapshot = %+v", started)
	}
	if started.ProfileVersion != "v1" || started.WorkerID != "worker-1" {
		t.Fatalf("snapshot = %+v", started)
	}
	if started.PromptSHA256 == "" || len(started.Tools) != 1 || started.Tools[0] != "echo" {
		t.Fatalf("snapshot = %+v", started)
	}

	// 事件只保存 prompt 摘要。
	other := testResolved(t, model)
	other.System = "换了个系统提示"
	changed, err := CallModel(context.Background(), other, testRoundRef("rnd_2"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Drafts[0].Payload.(event.RoundStarted).PromptSHA256 == started.PromptSHA256 {
		t.Fatal("a different system prompt must change the fingerprint")
	}
}

// 模型调用幂等键仅由 RoundID 决定。
func TestCallModelPassesIdempotencyKeyAndTools(t *testing.T) {
	model := &fakeModel{blocks: []chat.Block{{Kind: chat.BlockText, Text: "好"}}}
	res := testResolved(t, model)

	if _, err := CallModel(context.Background(), res, testRoundRef("rnd_1"),
		[]chat.Message{{Role: chat.RoleUser, Text: "在吗"}}, nil); err != nil {
		t.Fatal(err)
	}
	if model.req.IdempotencyKey != ModelInvocationKey("rnd_1") {
		t.Fatalf("idempotency key = %q", model.req.IdempotencyKey)
	}
	if model.req.System != "系统提示" || len(model.req.Messages) != 1 {
		t.Fatalf("request = %+v", model.req)
	}
	if len(model.req.Tools) != 1 || model.req.Tools[0].Name != "echo" {
		t.Fatalf("tool defs = %+v", model.req.Tools)
	}
}

// 模型调用各阶段失败均不返回部分 round。
func TestCallModelFailuresProduceNoDrafts(t *testing.T) {
	for name, model := range map[string]*fakeModel{
		"call":   {callErr: errors.New("dial failed")},
		"stream": {streamErr: errors.New("connection reset")},
		"result": {resultErr: errors.New("truncated stream")},
	} {
		t.Run(name, func(t *testing.T) {
			round, err := CallModel(context.Background(), testResolved(t, model), testRoundRef("rnd_1"), nil, nil)
			if err == nil {
				t.Fatal("expected the failure to surface")
			}
			if len(round.Drafts) != 0 {
				t.Fatalf("drafts = %v", kinds(round.Drafts))
			}
		})
	}
}

func TestCallModelRequiresModelAndRegistry(t *testing.T) {
	if _, err := CallModel(context.Background(), Resolved{},
		testRoundRef("rnd_1"), nil, nil); err == nil {
		t.Fatal("an unresolved runtime must fail")
	}
}

func TestInvocationKeys(t *testing.T) {
	if ModelInvocationKey("rnd_1") == ModelInvocationKey("rnd_2") {
		t.Fatal("model keys must differ per round")
	}
	if ToolInvocationKey("rnd_1", "c1") == ToolInvocationKey("rnd_1", "c2") {
		t.Fatal("tool keys must differ per call")
	}
	if ToolInvocationKey("rnd_1", "c1") == ToolInvocationKey("rnd_2", "c1") {
		t.Fatal("tool keys must differ per round")
	}
}
