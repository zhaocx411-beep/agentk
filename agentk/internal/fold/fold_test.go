package fold

import (
	"encoding/json"
	"strings"
	"testing"

	"agentk/internal/chat"
	"agentk/internal/event"
	"agentk/internal/tool"
	"agentk/internal/types"
)

const testModel = "gpt-5.4"

func testOptions(t *testing.T) Options {
	t.Helper()
	reg, err := tool.NewRegistry(
		tool.Spec{Name: "fetch", HistoryTrim: &tool.HistoryTrim{Field: "content", MaxChars: 10}, Binding: tool.InlineBinding{}},
		tool.Spec{Name: "kling_image_to_video", Binding: tool.WorkflowBinding{
			WorkflowType: "test.kling_image_to_video",
		}},
		tool.Spec{Name: tool.AskUserToolName, Binding: tool.UserBinding{}},
	)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	return Options{Registry: reg, Model: testModel, Dialect: event.DialectOpenAI}
}

// eventLog 按追加顺序生成事件。
type eventLog struct {
	events []event.Event
}

func (l *eventLog) add(round event.RoundID, payload event.Payload) *eventLog {
	l.events = append(l.events, event.Event{
		RoundID: round,
		Payload: payload,
	})
	return l
}

func (l *eventLog) round(round event.RoundID, model string, payloads ...event.Payload) *eventLog {
	l.add(round, event.RoundStarted{
		Model: model, Dialect: event.DialectOpenAI,
	})
	for _, p := range payloads {
		l.add(round, p)
	}
	return l
}

func TestHistoryProjectsOneToolRound(t *testing.T) {
	l := (&eventLog{}).
		add("", event.UserInput{Text: "做个猫的视频"}).
		round("r1", testModel,
			event.AssistantText{Block: 0, Text: "好，我来生成。"},
			event.AssistantToolCall{ToolCallID: "c1", ToolName: "kling_image_to_video", Args: event.RawJSON(`{"prompt":"cat"}`)},
			event.RoundCompleted{ModelMillis: 10}).
		add("r1", event.ToolExecStarted{ToolCallID: "c1"}).
		add("r1", event.ToolExecSucceeded{ToolCallID: "c1", Result: event.RawJSON(`{"artifacts":[{"type":"video","uri":"s3://v.mp4"}]}`)})

	got := History(l.events, testOptions(t))
	want := []chat.Message{
		{Role: chat.RoleUser, Text: "做个猫的视频"},
		{Role: chat.RoleAssistant, Text: "好，我来生成。", ToolCalls: []chat.ToolCall{
			{ID: "c1", Name: "kling_image_to_video", Args: json.RawMessage(`{"prompt":"cat"}`)},
		}},
		{Role: chat.RoleTool, ToolCallID: "c1", Text: `{"artifacts":[{"type":"video","uri":"s3://v.mp4"}]}`},
	}
	assertMessages(t, want, got)
}

func TestHistoryClosesAskUserAcrossRestart(t *testing.T) {
	l := (&eventLog{}).
		add("", event.UserInput{Text: "做个视频"}).
		round("r1", testModel,
			event.AssistantToolCall{ToolCallID: "c1", ToolName: tool.AskUserToolName, Args: event.RawJSON(`{"questions":[]}`)},
			event.AskUserRequested{ToolCallID: "c1", Questions: []event.Question{{ID: "q1", Question: "画风？"}}}).
		add("", event.AskUserResponded{ToolCallID: "c1", Answers: []event.Answer{
			{QuestionID: "q1", Question: "画风？", Selected: []string{"动画"}},
		}})

	got := History(l.events, testOptions(t))
	if len(got) != 3 {
		t.Fatalf("got %d messages, want 3: %#v", len(got), got)
	}
	last := got[2]
	if last.Role != chat.RoleTool || last.ToolCallID != "c1" {
		t.Fatalf("last message = %#v, want tool result closing c1", last)
	}
	if !strings.Contains(last.Text, "动画") {
		t.Errorf("answer text = %q, want the user's selection", last.Text)
	}
}

// TestHistoryClosesDanglingCalls 验证缺失结果的 tool call 仍满足 provider 消息约束。
func TestHistoryClosesDanglingCalls(t *testing.T) {
	l := (&eventLog{}).
		round("r1", testModel,
			event.AssistantToolCall{ToolCallID: "c1", ToolName: tool.AskUserToolName, Args: event.RawJSON(`{}`)},
			event.AskUserRequested{ToolCallID: "c1"}).
		add("", event.UserInput{Text: "算了，直接做动画风"}).
		round("r2", testModel,
			event.AssistantToolCall{ToolCallID: "c2", ToolName: "kling_image_to_video", Args: event.RawJSON(`{}`)}).
		add("r2", event.ExecutionFinished{Status: types.ExecutionStatusCancelled})

	got := History(l.events, testOptions(t))
	if len(got) != 5 {
		t.Fatalf("got %d messages, want 5: %#v", len(got), got)
	}
	if got[1].Role != chat.RoleTool || !strings.Contains(got[1].Text, "unanswered") {
		t.Errorf("ask_user filler = %#v, want an unanswered notice", got[1])
	}
	if got[4].Role != chat.RoleTool || !strings.Contains(got[4].Text, "interrupted") {
		t.Errorf("interrupted filler = %#v, want an interrupted error", got[4])
	}
}

// TestHistoryFeedsReasoningBackOnlyToItsOwnModel 验证 reasoning 仅投影给原模型和 Dialect。
func TestHistoryFeedsReasoningBackOnlyToItsOwnModel(t *testing.T) {
	l := (&eventLog{}).
		add("", event.UserInput{Text: "hi"}).
		round("r1", testModel,
			event.AssistantReasoning{Block: 0, Text: "先想想"},
			event.AssistantText{Block: 1, Text: "你好"})

	opts := testOptions(t)
	got := History(l.events, opts)
	if got[1].Reasoning != "先想想" {
		t.Errorf("same model reasoning = %q, want it fed back", got[1].Reasoning)
	}

	opts.Model = "some-other-model"
	got = History(l.events, opts)
	if got[1].Reasoning != "" {
		t.Errorf("cross-model reasoning = %q, want it dropped", got[1].Reasoning)
	}
	if got[1].Text != "你好" {
		t.Errorf("text = %q, want it kept regardless of model", got[1].Text)
	}
}

func TestHistoryJoinsBlocksInIndexOrder(t *testing.T) {
	l := (&eventLog{}).round("r1", testModel,
		event.AssistantText{Block: 2, Text: "第二段"},
		event.AssistantText{Block: 1, Text: "第一段"})

	got := History(l.events, testOptions(t))
	if got[0].Text != "第一段\n\n第二段" {
		t.Errorf("text = %q, want blocks joined by index order", got[0].Text)
	}
}

// TestHistoryReplacesRepeatedBlock 验证重复 block 替换而非追加。
func TestHistoryReplacesRepeatedBlock(t *testing.T) {
	l := (&eventLog{}).round("r1", testModel,
		event.AssistantText{Block: 0, Text: "半截"},
		event.AssistantText{Block: 0, Text: "完整的一段"})

	got := History(l.events, testOptions(t))
	if got[0].Text != "完整的一段" {
		t.Errorf("text = %q, want the repeated block to replace", got[0].Text)
	}
}

func TestHistoryTrimsDeclaredToolResults(t *testing.T) {
	long := strings.Repeat("字", 50)
	l := (&eventLog{}).
		round("r1", testModel,
			event.AssistantToolCall{ToolCallID: "c1", ToolName: "fetch", Args: event.RawJSON(`{}`)}).
		add("r1", event.ToolExecSucceeded{ToolCallID: "c1",
			Result: event.RawJSON(`{"content":"` + long + `"}`)})

	got := History(l.events, testOptions(t))
	result := got[1]
	if len(result.Text) >= len(long) {
		t.Errorf("result was not trimmed: %q", result.Text)
	}
	if !strings.Contains(result.Text, "fetch") {
		t.Errorf("trimmed result = %q, want a note naming the tool to call again", result.Text)
	}
}

func TestHistoryIgnoresUntrimmableShapes(t *testing.T) {
	l := (&eventLog{}).
		round("r1", testModel,
			event.AssistantToolCall{ToolCallID: "c1", ToolName: "fetch", Args: event.RawJSON(`{}`)}).
		add("r1", event.ToolExecSucceeded{ToolCallID: "c1", Result: event.RawJSON(`["not an object"]`)})

	got := History(l.events, testOptions(t))
	if got[1].Text != `["not an object"]` {
		t.Errorf("result = %q, want the unrecognized shape passed through", got[1].Text)
	}
}

func TestHistoryProjectsFailedToolCall(t *testing.T) {
	l := (&eventLog{}).
		round("r1", testModel,
			event.AssistantToolCall{ToolCallID: "c1", ToolName: "kling_image_to_video", Args: event.RawJSON(`{}`)}).
		add("r1", event.ToolExecFailed{ToolCallID: "c1", Error: "upstream 503"})

	got := History(l.events, testOptions(t))
	if got[1].Role != chat.RoleTool || !strings.Contains(got[1].Text, "upstream 503") {
		t.Errorf("failure = %#v, want a tool result carrying the error", got[1])
	}
}

// TestHistoryIgnoresPipelineEvents 验证 Pipeline 事件不进入模型历史。
func TestHistoryIgnoresPipelineEvents(t *testing.T) {
	base := (&eventLog{}).
		add("", event.UserInput{Text: "做个猫的视频"}).
		round("r1", testModel, event.AssistantText{Block: 0, Text: "好。"})
	want := History(base.events, testOptions(t))

	withPipeline := (&eventLog{}).
		add("", event.UserInput{Text: "做个猫的视频"}).
		add("", event.ExecutionStarted{
			ExecutionKind: types.ExecutionKindPipeline,
			Pipeline: &event.PipelineStarted{
				Name: "storyboard_film", CapabilityID: "contrib.storyboard.film",
			},
		}).
		round("r1", testModel, event.AssistantText{Block: 0, Text: "好。"}).
		add("", event.InvocationProgress{
			InvocationID: "exe_1", InvocationKind: "pipeline",
			InvocationName: "storyboard_film", StepID: "board", Message: "分镜完成",
		}).
		add("", event.ExecutionFinished{
			Status: types.ExecutionStatusCompleted,
			Pipeline: &event.PipelineFinished{
				Output: event.RawJSON(`{"film":"s3://f.mp4"}`),
			},
		})

	got := History(withPipeline.events, testOptions(t))
	assertMessages(t, want, got)
}

func TestHistoryOfEmptyEventLogIsEmpty(t *testing.T) {
	if got := History(nil, testOptions(t)); len(got) != 0 {
		t.Fatalf("got %#v, want no messages", got)
	}
}

func assertMessages(t *testing.T, want, got []chat.Message) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("got %d messages, want %d:\n got %#v\nwant %#v", len(got), len(want), got, want)
	}
	for i := range want {
		w, g := want[i], got[i]
		if w.Role != g.Role || w.Text != g.Text || w.ToolCallID != g.ToolCallID ||
			w.Reasoning != g.Reasoning || len(w.ToolCalls) != len(g.ToolCalls) {
			t.Errorf("message %d:\n got %#v\nwant %#v", i, g, w)
			continue
		}
		for j := range w.ToolCalls {
			if w.ToolCalls[j].ID != g.ToolCalls[j].ID ||
				w.ToolCalls[j].Name != g.ToolCalls[j].Name ||
				string(w.ToolCalls[j].Args) != string(g.ToolCalls[j].Args) {
				t.Errorf("message %d tool call %d:\n got %#v\nwant %#v",
					i, j, g.ToolCalls[j], w.ToolCalls[j])
			}
		}
	}
}
