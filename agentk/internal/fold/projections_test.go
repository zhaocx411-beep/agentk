package fold

import (
	"testing"
	"time"

	"agentk/internal/event"
	"agentk/internal/tool"
)

// 空事件历史不需要运行模型。
func TestLoopStatusOnEmptyEventLog(t *testing.T) {
	st := LoopStatus(nil, testOptions(t))
	if st.NeedsRound || st.AwaitingAnswer {
		t.Fatalf("status = %+v", st)
	}
}

// 用户输入需要模型响应。
func TestLoopStatusNeedsRoundAfterUserInput(t *testing.T) {
	l := (&eventLog{}).add("", event.UserInput{Text: "在吗"})

	st := LoopStatus(l.events, testOptions(t))
	if !st.NeedsRound || st.AwaitingAnswer {
		t.Fatalf("status = %+v", st)
	}
}

// 最终 assistant 消息不需要新一轮模型调用。
func TestLoopStatusIdleAfterAssistantText(t *testing.T) {
	l := (&eventLog{}).
		add("", event.UserInput{Text: "在吗"}).
		round("r1", testModel,
			event.AssistantText{Block: 0, Text: "在的"},
			event.RoundCompleted{})

	st := LoopStatus(l.events, testOptions(t))
	if st.NeedsRound || st.AwaitingAnswer {
		t.Fatalf("status = %+v", st)
	}
}

// 工具结果需要模型继续处理。
func TestLoopStatusNeedsRoundAfterToolResult(t *testing.T) {
	l := (&eventLog{}).
		add("", event.UserInput{Text: "查一下"}).
		round("r1", testModel,
			event.AssistantToolCall{ToolCallID: "c1", ToolName: "fetch", Args: event.RawJSON(`{}`)},
			event.RoundCompleted{}).
		add("r1", event.ToolExecSucceeded{ToolCallID: "c1", Result: event.RawJSON(`{"content":"x"}`)})

	if st := LoopStatus(l.events, testOptions(t)); !st.NeedsRound {
		t.Fatalf("status = %+v", st)
	}
}

// 未回答的 ask_user 等待用户输入。
func TestLoopStatusAwaitsAnswer(t *testing.T) {
	l := (&eventLog{}).
		add("", event.UserInput{Text: "帮我做个视频"}).
		round("r1", testModel,
			event.AssistantToolCall{ToolCallID: "c1", ToolName: tool.AskUserToolName, Args: event.RawJSON(`{}`)},
			event.RoundCompleted{}).
		add("r1", event.AskUserRequested{ToolCallID: "c1", Questions: []event.Question{{ID: "q1", Question: "多长？"}}})

	st := LoopStatus(l.events, testOptions(t))
	if !st.AwaitingAnswer || st.NeedsRound {
		t.Fatalf("status = %+v", st)
	}
}

// 用户回答后需要模型继续处理。
func TestLoopStatusNeedsRoundAfterAnswer(t *testing.T) {
	l := (&eventLog{}).
		add("", event.UserInput{Text: "帮我做个视频"}).
		round("r1", testModel,
			event.AssistantToolCall{ToolCallID: "c1", ToolName: tool.AskUserToolName, Args: event.RawJSON(`{}`)},
			event.RoundCompleted{}).
		add("r1", event.AskUserRequested{ToolCallID: "c1", Questions: []event.Question{{ID: "q1", Question: "多长？"}}}).
		add("", event.AskUserResponded{ToolCallID: "c1", Answers: []event.Answer{{QuestionID: "q1", Selected: []string{"15 秒"}}}})

	st := LoopStatus(l.events, testOptions(t))
	if st.AwaitingAnswer || !st.NeedsRound {
		t.Fatalf("status = %+v", st)
	}
}

// 新用户消息取代待回答请求，LoopStatus 必须与 PendingAsks 一致。
func TestLoopStatusUserInputTakesOverPendingAsk(t *testing.T) {
	l := (&eventLog{}).
		add("", event.UserInput{Text: "帮我做个视频"}).
		round("r1", testModel,
			event.AssistantToolCall{ToolCallID: "c1", ToolName: tool.AskUserToolName, Args: event.RawJSON(`{}`)},
			event.RoundCompleted{}).
		add("r1", event.AskUserRequested{ToolCallID: "c1", Questions: []event.Question{{ID: "q1", Question: "多长？"}}}).
		add("", event.UserInput{Text: "算了，直接做"})

	st := LoopStatus(l.events, testOptions(t))
	if st.AwaitingAnswer || !st.NeedsRound {
		t.Fatalf("status = %+v", st)
	}
	if asks := PendingAsks(l.events); len(asks) != 0 {
		t.Fatalf("pending asks = %+v", asks)
	}
}

func TestPendingAsksTracksOpenQuestions(t *testing.T) {
	asked := time.Now().Truncate(time.Second)
	events := []event.Event{
		{Payload: event.AskUserRequested{
			ToolCallID: "c1", Questions: []event.Question{{ID: "q1", Question: "多长？"}},
		}, CreatedAt: asked},
		{Payload: event.AskUserRequested{
			ToolCallID: "c2", Questions: []event.Question{{ID: "q2", Question: "什么风格？"}},
		}, CreatedAt: asked},
	}

	asks := PendingAsks(events)
	if len(asks) != 2 || asks[0].ToolCallID != "c1" || asks[1].ToolCallID != "c2" {
		t.Fatalf("asks = %+v", asks)
	}
	if !asks[0].AskedAt.Equal(asked) || len(asks[0].Questions) != 1 {
		t.Fatalf("ask = %+v", asks[0])
	}

	answered := append(events, event.Event{Payload: event.AskUserResponded{
		ToolCallID: "c1", Answers: []event.Answer{{QuestionID: "q1", Selected: []string{"15 秒"}}},
	}})
	if asks := PendingAsks(answered); len(asks) != 1 || asks[0].ToolCallID != "c2" {
		t.Fatalf("asks after answering c1 = %+v", asks)
	}
}

// 工具产出的资源关联原始 tool call。
func TestArtifactsProjectsToolResults(t *testing.T) {
	at := time.Now().Truncate(time.Second)
	events := []event.Event{
		{Payload: event.AssistantToolCall{
			ToolCallID: "c1", ToolName: "draw", Args: event.RawJSON(`{"prompt":"cat"}`),
		}, CreatedAt: at},
		{Payload: event.ToolExecSucceeded{
			ToolCallID: "c1",
			Artifacts: []event.Artifact{{
				Type: "image", URI: "https://cdn/a.png", Description: "猫咪图片", Model: "image-model",
				Params: event.RawJSON(`{"size":"1024x1024"}`),
			}, {
				Type: "image", URI: "https://cdn/b.png", Description: "猫咪图片二",
			}},
		}, CreatedAt: at},
		{Payload: event.AssistantToolCall{
			ToolCallID: "c2", ToolName: "kling_image_to_video", Args: event.RawJSON(`{"firstFrame":"https://cdn/a.png"}`),
		}, CreatedAt: at},
		{Payload: event.ToolExecSucceeded{
			ToolCallID: "c2",
			Artifacts:  []event.Artifact{{Type: "video", URI: "https://cdn/v.mp4"}},
		}, CreatedAt: at},
	}

	arts := Artifacts(events)
	if len(arts) != 3 {
		t.Fatalf("artifacts = %+v", arts)
	}
	if arts[0].URL != "https://cdn/a.png" || arts[0].Description != "猫咪图片" || arts[0].Tool != "draw" ||
		arts[0].Model != "image-model" || string(arts[0].Params) != `{"size":"1024x1024"}` {
		t.Fatalf("sync artifact = %+v", arts[0])
	}
	if arts[0].CreatedAt != at.Format(time.RFC3339) {
		t.Fatalf("created at = %q", arts[0].CreatedAt)
	}
	if arts[1].URL != "https://cdn/b.png" || arts[1].Tool != "draw" {
		t.Fatalf("second sync artifact = %+v", arts[1])
	}
	if arts[2].URL != "https://cdn/v.mp4" || arts[2].Tool != "kling_image_to_video" {
		t.Fatalf("workflow artifact = %+v", arts[2])
	}
	// 来源资源 URI 保留在派生工具的参数中。
	if string(arts[2].Input) != `{"firstFrame":"https://cdn/a.png"}` {
		t.Fatalf("lineage input = %s", arts[2].Input)
	}
}

func TestArtifactsEmptyWithoutDeclarations(t *testing.T) {
	events := []event.Event{
		{Payload: event.AssistantToolCall{ToolCallID: "c1", ToolName: "fetch", Args: event.RawJSON(`{}`)}},
		{Payload: event.ToolExecSucceeded{ToolCallID: "c1", Result: event.RawJSON(`{"content":"x"}`)}},
	}
	if arts := Artifacts(events); len(arts) != 0 {
		t.Fatalf("artifacts = %+v", arts)
	}
}
