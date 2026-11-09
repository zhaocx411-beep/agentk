package chat

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"studio/platform/core"
)

func TestChatFoldBuildsConversationAndArtifacts(t *testing.T) {
	state := NewChatState()
	at := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	events := []core.LedgerEvent{
		chatEvent(t, 1, "user.input", map[string]any{"text": "做一张海报"}, at),
		chatEvent(t, 2, "round.started", map[string]any{}, at.Add(time.Second)),
		chatEvent(t, 3, "assistant.reasoning", map[string]any{"text": "先确定构图"}, at),
		chatEvent(t, 4, "assistant.text", map[string]any{"text": "我来制作。"}, at),
		chatEvent(t, 5, "assistant.tool_call", map[string]any{
			"toolCallId": "call_1",
			"toolName":   "generate_image",
			"args":       map[string]any{"prompt": "海报"},
		}, at),
		chatEvent(t, 6, "tool.exec_started", map[string]any{"toolCallId": "call_1"}, at),
		chatEvent(t, 7, "tool.exec_succeeded", map[string]any{
			"toolCallId": "call_1",
			"result": map[string]any{
				"artifacts": []map[string]string{
					{"type": "image", "uri": "https://files/poster.png"},
				},
			},
			"artifacts": []map[string]string{
				{"type": "image", "uri": "https://files/poster.png"},
			},
		}, at),
		chatEvent(t, 8, "round.completed", map[string]any{}, at),
	}
	for _, event := range events {
		state.Apply(event)
	}

	if state.Status != ChatIdle {
		t.Fatalf("status = %q, want idle", state.Status)
	}
	if len(state.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(state.Messages))
	}
	assistant := state.Messages[1]
	if assistant.Text != "我来制作。" || assistant.Reasoning != "先确定构图" {
		t.Fatalf("assistant = %+v", assistant)
	}
	if len(assistant.Tools) != 1 || assistant.Tools[0].Status != "completed" {
		t.Fatalf("tools = %+v", assistant.Tools)
	}
	if len(assistant.Artifacts) != 1 ||
		assistant.Artifacts[0].URI != "https://files/poster.png" {
		t.Fatalf("artifacts = %+v", assistant.Artifacts)
	}
}

func TestChatFoldKeepsMultipleRoundsAndIgnoresDuplicateEvents(t *testing.T) {
	state := NewChatState()
	at := time.Now().UTC()
	events := []core.LedgerEvent{
		chatEvent(t, 1, "user.input", map[string]string{"text": "第一问"}, at),
		chatEvent(t, 2, "round.started", map[string]any{}, at),
		chatEvent(t, 3, "assistant.text", map[string]string{"text": "第一答"}, at),
		chatEvent(t, 4, "round.completed", map[string]any{}, at),
		chatEvent(t, 5, "user.input", map[string]string{"text": "第二问"}, at),
		chatEvent(t, 6, "round.started", map[string]any{}, at),
		chatEvent(t, 7, "assistant.text", map[string]string{"text": "第二答"}, at),
		chatEvent(t, 8, "round.completed", map[string]any{}, at),
	}
	for _, event := range events {
		state.Apply(event)
	}
	state.Apply(events[len(events)-1])
	if len(state.Messages) != 4 {
		t.Fatalf("messages = %d, want 4", len(state.Messages))
	}
	if state.Messages[1].Text != "第一答" || state.Messages[3].Text != "第二答" {
		t.Fatalf("messages = %+v", state.Messages)
	}
}

func TestChatFoldKeepsSameArtifactURIAcrossToolCalls(t *testing.T) {
	state := NewChatState()
	at := time.Now().UTC()
	uri := "https://files.example/shared.png"
	events := []core.LedgerEvent{
		chatEvent(t, 1, "round.started", map[string]any{}, at),
		chatEvent(t, 2, "assistant.tool_call", map[string]any{
			"toolCallId": "call_a", "toolName": "generate_image",
		}, at),
		chatEvent(t, 3, "tool.exec_succeeded", map[string]any{
			"toolCallId": "call_a",
			"artifacts":  []map[string]string{{"type": "image", "uri": uri}},
		}, at),
		chatEvent(t, 4, "assistant.tool_call", map[string]any{
			"toolCallId": "call_b", "toolName": "edit_image",
		}, at),
		chatEvent(t, 5, "tool.exec_succeeded", map[string]any{
			"toolCallId": "call_b",
			"artifacts":  []map[string]string{{"type": "image", "uri": uri}},
		}, at),
		chatEvent(t, 6, "round.completed", map[string]any{}, at),
	}
	for _, event := range events {
		state.Apply(event)
	}
	if len(state.Messages) != 1 || len(state.Messages[0].Artifacts) != 2 ||
		state.Messages[0].Artifacts[0].Source == state.Messages[0].Artifacts[1].Source {
		t.Fatalf("same URI artifacts were collapsed across calls: %+v", state.Messages)
	}
}

func TestChatFoldNormalizesArtifactURLFields(t *testing.T) {
	state := NewChatState()
	at := time.Now().UTC()
	for _, event := range []core.LedgerEvent{
		chatEvent(t, 1, "round.started", map[string]any{}, at),
		chatEvent(t, 2, "assistant.tool_call", map[string]any{
			"toolCallId": "call_history", "toolName": "list_artifacts",
		}, at),
		chatEvent(t, 3, "tool.exec_succeeded", map[string]any{
			"toolCallId": "call_history",
			"result": map[string]any{
				"artifacts": []map[string]string{{
					"type": "image", "url": "https://files.example/history.png",
				}},
			},
		}, at),
	} {
		state.Apply(event)
	}
	if len(state.Messages) != 1 || len(state.Messages[0].Artifacts) != 1 ||
		state.Messages[0].Artifacts[0].URI != "https://files.example/history.png" {
		t.Fatalf("URL artifact was not normalized: %+v", state.Messages)
	}
}

func TestChatFoldTracksJobsAndErrors(t *testing.T) {
	state := NewChatState()
	at := time.Now().UTC()
	for _, event := range []core.LedgerEvent{
		chatEvent(t, 1, "user.input", map[string]string{"text": "生成视频"}, at),
		chatEvent(t, 2, "job.submitted", map[string]string{"jobId": "job_1"}, at),
		chatEvent(t, 3, "round.completed", map[string]any{}, at),
	} {
		state.Apply(event)
	}
	if state.Status != ChatRunning {
		t.Fatalf("submitted status = %q", state.Status)
	}
	state.Apply(chatEvent(t, 4, "job.completed", map[string]any{
		"jobId": "job_1",
		"artifacts": []map[string]string{
			{"type": "video", "uri": "https://files/video.mp4"},
		},
	}, at))
	if state.Status != ChatIdle {
		t.Fatalf("completed status = %q", state.Status)
	}
	state.Apply(chatEvent(t, 5, "error", map[string]string{"message": "模型失败"}, at))
	if state.Status != ChatError || state.ErrorMessage != "模型失败" {
		t.Fatalf("error state = %+v", state)
	}
	state.Apply(chatEvent(t, 6, "user.input", map[string]string{"text": "重试"}, at))
	if state.ErrorMessage != "" || state.Status != ChatRunning {
		t.Fatalf("recovered state = %+v", state)
	}
}

func TestChatFoldExecutionFinishedFailureClosesRunningWork(t *testing.T) {
	state := NewChatState()
	at := time.Now().UTC()
	for _, event := range []core.LedgerEvent{
		chatEvent(t, 1, "user.input", map[string]string{"text": "生成视频"}, at),
		chatEvent(t, 2, "round.started", map[string]any{}, at),
		chatEvent(t, 3, "assistant.tool_call", map[string]any{
			"toolCallId": "call_1", "toolName": "generate_video",
		}, at),
		chatEvent(t, 4, "job.submitted", map[string]string{"jobId": "job_1"}, at),
		chatEvent(t, 5, "execution.finished", map[string]string{
			"status": "failed", "error": "上游模型失败",
		}, at),
	} {
		state.Apply(event)
	}
	if state.Status != ChatError || state.ErrorMessage != "上游模型失败" {
		t.Fatalf("state = %+v", state)
	}
	if len(state.OpenJobs) != 0 {
		t.Fatalf("open jobs = %+v", state.OpenJobs)
	}
	tool := state.findTool("call_1")
	if tool == nil || tool.Status != "failed" || tool.Error != "上游模型失败" {
		t.Fatalf("tool = %+v", tool)
	}
	if state.Messages[state.CurrentAssistant].Status != "failed" {
		t.Fatalf("assistant = %+v", state.Messages[state.CurrentAssistant])
	}
}

func TestChatFoldExecutionFinishedCancelledUsesFallback(t *testing.T) {
	state := NewChatState()
	at := time.Now().UTC()
	state.Apply(chatEvent(t, 1, "user.input", map[string]string{"text": "开始"}, at))
	state.Apply(chatEvent(t, 2, "execution.finished", map[string]string{
		"status": "cancelled",
	}, at))
	if state.Status != ChatError || state.ErrorMessage != "execution cancelled" {
		t.Fatalf("state = %+v", state)
	}
}

func TestChatFoldExecutionFinishedCompletedClosesStreamingMessage(t *testing.T) {
	state := NewChatState()
	at := time.Now().UTC()
	state.Apply(chatEvent(t, 1, "round.started", map[string]any{}, at))
	state.Apply(chatEvent(t, 2, "assistant.text", map[string]string{"text": "完成"}, at))
	state.Apply(chatEvent(t, 3, "execution.finished", map[string]string{
		"status": "completed",
	}, at))
	if state.Status != ChatIdle || state.Messages[0].Status != "completed" {
		t.Fatalf("state = %+v", state)
	}
}

func TestChatFoldTracksAskUserUntilAnswered(t *testing.T) {
	state := NewChatState()
	at := time.Now().UTC()
	for _, event := range []core.LedgerEvent{
		chatEvent(t, 1, "round.started", map[string]any{}, at),
		chatEvent(t, 2, "assistant.tool_call", map[string]any{
			"toolCallId": "call_ask",
			"toolName":   "ask_user",
			"args": map[string]any{"questions": []map[string]any{{
				"id": "q1", "question": "选择画风", "options": []string{"写实", "水墨"},
			}}},
		}, at),
		chatEvent(t, 3, "round.completed", map[string]any{}, at),
		chatEvent(t, 4, "ask_user.requested", map[string]any{
			"toolCallId": "call_ask",
			"questions": []map[string]any{{
				"id": "q1", "question": "选择画风", "options": []string{"写实", "水墨"},
			}},
		}, at),
	} {
		state.Apply(event)
	}
	if len(state.PendingAsks) != 1 || state.PendingAsks[0].Questions[0].ID != "q1" {
		t.Fatalf("pending asks = %+v", state.PendingAsks)
	}
	if tool := state.findTool("call_ask"); tool == nil || tool.Status != "running" {
		t.Fatalf("ask tool = %+v", tool)
	}

	state.Apply(chatEvent(t, 5, "ask_user.responded", map[string]any{
		"toolCallId": "call_ask",
		"answers": []map[string]any{{
			"questionId": "q1", "question": "选择画风", "selected": []string{"水墨"},
		}},
	}, at))
	if len(state.PendingAsks) != 0 {
		t.Fatalf("pending asks after response = %+v", state.PendingAsks)
	}
	tool := state.findTool("call_ask")
	if tool == nil || tool.Status != "completed" ||
		!strings.Contains(string(tool.Result), `"水墨"`) {
		t.Fatalf("completed ask tool = %+v", tool)
	}
	if state.Status != ChatRunning {
		t.Fatalf("status after response = %q, want running", state.Status)
	}
}

func TestChatFoldUserMessageSkipsPendingAsk(t *testing.T) {
	state := NewChatState()
	at := time.Now().UTC()
	for _, event := range []core.LedgerEvent{
		chatEvent(t, 1, "round.started", map[string]any{}, at),
		chatEvent(t, 2, "assistant.tool_call", map[string]any{
			"toolCallId": "call_ask", "toolName": "ask_user",
		}, at),
		chatEvent(t, 3, "ask_user.requested", map[string]any{
			"toolCallId": "call_ask",
			"questions": []map[string]any{{
				"id": "q1", "question": "继续吗", "options": []string{"继续", "停止"},
			}},
		}, at),
		chatEvent(t, 4, "user.input", map[string]string{"text": "换个方向"}, at),
	} {
		state.Apply(event)
	}
	if len(state.PendingAsks) != 0 {
		t.Fatalf("pending asks = %+v", state.PendingAsks)
	}
	tool := state.findTool("call_ask")
	if tool == nil || !strings.Contains(string(tool.Result), `"unanswered"`) {
		t.Fatalf("skipped ask tool = %+v", tool)
	}
}

func chatEvent(
	t *testing.T,
	seq int64,
	kind string,
	data any,
	at time.Time,
) core.LedgerEvent {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return core.LedgerEvent{
		ID:      fmt.Sprintf("evt_%08d", seq),
		RoundID: "round", Kind: kind, Data: raw, CreatedAt: at,
	}
}
