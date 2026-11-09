package comicdrama

import (
	"encoding/json"
	"fmt"
	"testing"

	"studio/platform/core"
)

// mustEvent 用序号构造事件；事件 ID 按 evt_%08d 生成，与真实 evt_ UUIDv7
// 一样满足"字典序 == 时间序"。
func mustEvent(t *testing.T, seq int64, kind string, data any) core.LedgerEvent {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("encode event data: %v", err)
	}
	return core.LedgerEvent{ID: testEventID(seq), Kind: kind, Data: raw}
}

func testEventID(seq int64) string {
	return fmt.Sprintf("evt_%08d", seq)
}

func applyAll(t *testing.T, state *DramaState, events []core.LedgerEvent) {
	t.Helper()
	for _, ev := range events {
		state.Apply(ev)
	}
}

func TestEmptyStateIsIdle(t *testing.T) {
	state := NewDramaState()
	if got := state.Status(); got != DramaIdle {
		t.Fatalf("status = %q, want idle", got)
	}
}

func TestUserInputStartsRunning(t *testing.T) {
	state := NewDramaState()
	state.Apply(mustEvent(t, 1, "user.input", map[string]string{"text": "做一集漫剧"}))
	if got := state.Status(); got != DramaRunning {
		t.Fatalf("status = %q, want running", got)
	}
	if state.LastEventID != testEventID(1) {
		t.Fatalf("lastEventID = %q, want %q", state.LastEventID, testEventID(1))
	}
}

// 模型某一轮只输出正文、没有工具调用 → 收敛为 idle，正文成为 assistantSays。
func TestTextOnlyRoundConverges(t *testing.T) {
	state := NewDramaState()
	applyAll(t, &state, []core.LedgerEvent{
		mustEvent(t, 1, "user.input", map[string]string{"text": "做一集漫剧"}),
		mustEvent(t, 2, "round.started", map[string]string{"model": "deepseek-v4-pro"}),
		mustEvent(t, 3, "assistant.text", map[string]any{"block": 0, "text": "剧本如下，"}),
		mustEvent(t, 4, "assistant.text", map[string]any{"block": 0, "text": "成片已交付。"}),
		mustEvent(t, 5, "round.completed", map[string]any{"usage": map[string]int{}}),
	})
	if got := state.Status(); got != DramaIdle {
		t.Fatalf("status = %q, want idle", got)
	}
	if state.AssistantSays != "剧本如下，成片已交付。" {
		t.Fatalf("assistantSays = %q", state.AssistantSays)
	}
}

// 有未闭合的工具调用 → running；全部闭合但该轮发起过调用 → 仍 running
// （Agent 会继续下一轮），直到出现纯正文轮。
func TestToolRoundsKeepRunning(t *testing.T) {
	state := NewDramaState()
	applyAll(t, &state, []core.LedgerEvent{
		mustEvent(t, 1, "user.input", map[string]string{"text": "做一集漫剧"}),
		mustEvent(t, 2, "round.started", map[string]string{}),
		mustEvent(t, 3, "assistant.tool_call", map[string]any{
			"toolCallId": "call_1", "toolName": "generate_image", "args": map[string]any{},
		}),
		mustEvent(t, 4, "round.completed", map[string]any{}),
	})
	if got := state.Status(); got != DramaRunning {
		t.Fatalf("status after tool call = %q, want running", got)
	}

	state.Apply(mustEvent(t, 5, "tool.exec_succeeded", map[string]any{
		"toolCallId": "call_1",
		"result":     map[string]string{"imageUrl": "https://files/img1.png"},
		"artifacts":  []map[string]string{{"type": "image", "uri": "https://files/img1.png"}},
	}))
	// 调用闭合但本轮发起过工具调用：Agent 还会跑下一轮。
	if got := state.Status(); got != DramaRunning {
		t.Fatalf("status after tool result = %q, want running", got)
	}
	if state.ImageCount != 1 {
		t.Fatalf("imageCount = %d, want 1", state.ImageCount)
	}

	applyAll(t, &state, []core.LedgerEvent{
		mustEvent(t, 6, "round.started", map[string]string{}),
		mustEvent(t, 7, "assistant.text", map[string]any{"block": 0, "text": "定妆照已完成"}),
		mustEvent(t, 8, "round.completed", map[string]any{}),
	})
	if got := state.Status(); got != DramaIdle {
		t.Fatalf("status after text round = %q, want idle", got)
	}
}

// 异步视频任务：job.submitted 未终态时保持 running；job.completed 计入产物。
func TestPendingJobKeepsRunning(t *testing.T) {
	state := NewDramaState()
	applyAll(t, &state, []core.LedgerEvent{
		mustEvent(t, 1, "user.input", map[string]string{"text": "做一集漫剧"}),
		mustEvent(t, 2, "round.started", map[string]string{}),
		mustEvent(t, 3, "assistant.tool_call", map[string]any{
			"toolCallId": "call_1", "toolName": "kling_omni_video",
		}),
		mustEvent(t, 4, "round.completed", map[string]any{}),
		mustEvent(t, 5, "tool.exec_succeeded", map[string]any{
			"toolCallId": "call_1",
			"result":     map[string]string{"jobId": "jb_1"},
		}),
		mustEvent(t, 6, "job.submitted", map[string]any{
			"jobId": "jb_1", "toolCallId": "call_1", "wakePolicy": "resume",
		}),
	})
	if got := state.Status(); got != DramaRunning {
		t.Fatalf("status with pending job = %q, want running", got)
	}

	state.Apply(mustEvent(t, 7, "job.completed", map[string]any{
		"jobId": "jb_1", "toolCallId": "call_1",
		"artifacts": []map[string]string{{"type": "video", "uri": "https://files/clip1.mp4"}},
	}))
	if got := state.Status(); got != DramaRunning {
		t.Fatalf("status after job completion = %q, want running (agent resumes)", got)
	}
	if state.VideoCount != 1 {
		t.Fatalf("videoCount = %d, want 1", state.VideoCount)
	}
}

// concat_videos 成功 → 成片 URL 进 films。
func TestConcatVideosProducesFilm(t *testing.T) {
	state := NewDramaState()
	applyAll(t, &state, []core.LedgerEvent{
		mustEvent(t, 1, "user.input", map[string]string{"text": "做一集漫剧"}),
		mustEvent(t, 2, "round.started", map[string]string{}),
		mustEvent(t, 3, "assistant.tool_call", map[string]any{
			"toolCallId": "call_9", "toolName": "concat_videos",
		}),
		mustEvent(t, 4, "round.completed", map[string]any{}),
		mustEvent(t, 5, "tool.exec_succeeded", map[string]any{
			"toolCallId": "call_9",
			"result":     map[string]any{"videoUrl": "https://files/film.mp4", "clipCount": 3},
			"artifacts":  []map[string]string{{"type": "video", "uri": "https://files/film.mp4"}},
		}),
		mustEvent(t, 6, "round.started", map[string]string{}),
		mustEvent(t, 7, "assistant.text", map[string]any{"block": 0, "text": "成片：https://files/film.mp4"}),
		mustEvent(t, 8, "round.completed", map[string]any{}),
	})
	if got := state.Status(); got != DramaIdle {
		t.Fatalf("status = %q, want idle", got)
	}
	if len(state.Films) != 1 || state.Films[0] != "https://files/film.mp4" {
		t.Fatalf("films = %v", state.Films)
	}
}

// compose_video（新版 agent-k 的唯一合成工具）成功 → 成片 URL 进 films。
// 新契约的 result 没有顶层 videoUrl，只有标准化的 artifacts 列表。
func TestComposeVideoProducesFilm(t *testing.T) {
	state := NewDramaState()
	applyAll(t, &state, []core.LedgerEvent{
		mustEvent(t, 1, "user.input", map[string]string{"text": "做一集漫剧"}),
		mustEvent(t, 2, "round.started", map[string]string{}),
		mustEvent(t, 3, "assistant.tool_call", map[string]any{
			"toolCallId": "call_10", "toolName": "compose_video",
		}),
		mustEvent(t, 4, "round.completed", map[string]any{}),
		mustEvent(t, 5, "tool.exec_succeeded", map[string]any{
			"toolCallId": "call_10",
			"result": map[string]any{
				"artifacts": []map[string]string{{"type": "video", "uri": "https://files/film2.mp4"}},
				"clipCount": 3,
			},
			"artifacts": []map[string]string{{"type": "video", "uri": "https://files/film2.mp4"}},
		}),
		mustEvent(t, 6, "round.started", map[string]string{}),
		mustEvent(t, 7, "assistant.text", map[string]any{"block": 0, "text": "成片完成"}),
		mustEvent(t, 8, "round.completed", map[string]any{}),
	})
	if len(state.Films) != 1 || state.Films[0] != "https://files/film2.mp4" {
		t.Fatalf("films = %v", state.Films)
	}
}

// error / interrupted → error 状态；新的用户输入清空错误并回到 running。
func TestErrorThenNewMessageRecovers(t *testing.T) {
	state := NewDramaState()
	applyAll(t, &state, []core.LedgerEvent{
		mustEvent(t, 1, "user.input", map[string]string{"text": "做一集漫剧"}),
		mustEvent(t, 2, "error", map[string]string{"code": "UpstreamRejected", "message": "模型超时"}),
		mustEvent(t, 3, "interrupted", map[string]string{"reason": "error", "detail": "模型超时"}),
	})
	if got := state.Status(); got != DramaError {
		t.Fatalf("status = %q, want error", got)
	}
	if state.ErrorMessage != "模型超时" {
		t.Fatalf("errorMessage = %q", state.ErrorMessage)
	}

	state.Apply(mustEvent(t, 4, "user.input", map[string]string{"text": "重试"}))
	if got := state.Status(); got != DramaRunning {
		t.Fatalf("status after retry message = %q, want running", got)
	}
	if state.ErrorMessage != "" {
		t.Fatalf("errorMessage should be cleared, got %q", state.ErrorMessage)
	}
}

// loop 被打断（如超过 20 轮工具上限）但异步视频任务还在跑：任务完成后
// agent-k 会自动唤醒会话，所以对外状态保持 running，不报"任务中断"；
// 唤醒后的 round.started 清掉中断文案。
func TestInterruptWithPendingJobsStaysRunning(t *testing.T) {
	state := NewDramaState()
	applyAll(t, &state, []core.LedgerEvent{
		mustEvent(t, 1, "user.input", map[string]string{"text": "做一集漫剧"}),
		mustEvent(t, 2, "round.started", map[string]string{}),
		mustEvent(t, 3, "assistant.tool_call", map[string]any{
			"toolCallId": "call_vid", "toolName": "kling_omni_video",
		}),
		mustEvent(t, 4, "tool.exec_succeeded", map[string]any{
			"toolCallId": "call_vid", "result": map[string]string{"jobId": "jb_1"},
		}),
		mustEvent(t, 5, "job.submitted", map[string]any{"jobId": "jb_1", "toolCallId": "call_vid"}),
		mustEvent(t, 6, "error", map[string]string{
			"code": "MaxToolRoundsExceeded", "message": "session: exceeded max tool rounds 20",
		}),
		mustEvent(t, 7, "interrupted", map[string]string{
			"reason": "error", "detail": "session: exceeded max tool rounds 20",
		}),
	})
	if got := state.Status(); got != DramaRunning {
		t.Fatalf("status with pending job after interrupt = %q, want running", got)
	}

	// 任务完成（wakePolicy=resume）→ 仍 running，不闪 error/idle。
	state.Apply(mustEvent(t, 8, "job.completed", map[string]any{
		"jobId": "jb_1", "toolCallId": "call_vid", "wakePolicy": "resume",
		"artifacts": []map[string]string{{"type": "video", "uri": "https://files/clip1.mp4"}},
	}))
	if got := state.Status(); got != DramaRunning {
		t.Fatalf("status after wake-pending completion = %q, want running", got)
	}

	// 唤醒后的新一轮清掉中断文案，最终以纯正文轮收敛。
	applyAll(t, &state, []core.LedgerEvent{
		mustEvent(t, 9, "round.started", map[string]string{}),
		mustEvent(t, 10, "assistant.text", map[string]any{"text": "镜头已完成，继续合成。"}),
		mustEvent(t, 11, "round.completed", map[string]any{}),
	})
	if state.ErrorMessage != "" {
		t.Fatalf("errorMessage should be cleared after resume, got %q", state.ErrorMessage)
	}
	if got := state.Status(); got != DramaIdle {
		t.Fatalf("status after resumed text round = %q, want idle", got)
	}
}

// 中断且没有任何在途任务：这才是真正的 error 终态。
func TestInterruptWithoutWorkIsError(t *testing.T) {
	state := NewDramaState()
	applyAll(t, &state, []core.LedgerEvent{
		mustEvent(t, 1, "user.input", map[string]string{"text": "做一集漫剧"}),
		mustEvent(t, 2, "round.started", map[string]string{}),
		mustEvent(t, 3, "assistant.tool_call", map[string]any{
			"toolCallId": "call_1", "toolName": "generate_image",
		}),
		mustEvent(t, 4, "interrupted", map[string]string{
			"reason": "cancelled", "detail": "user cancelled",
		}),
	})
	// 随 loop 死掉的同步调用被清理，不会卡在 running。
	if got := state.Status(); got != DramaError {
		t.Fatalf("status = %q, want error", got)
	}
}

func TestExecutionFinishedFailureClosesAgentWork(t *testing.T) {
	state := NewDramaState()
	applyAll(t, &state, []core.LedgerEvent{
		mustEvent(t, 1, "user.input", map[string]string{"text": "做一集漫剧"}),
		mustEvent(t, 2, "round.started", map[string]string{}),
		mustEvent(t, 3, "assistant.tool_call", map[string]any{
			"toolCallId": "call_1", "toolName": "generate_image",
		}),
		mustEvent(t, 4, "job.submitted", map[string]any{
			"jobId": "job_1", "toolCallId": "call_1",
		}),
		mustEvent(t, 5, "execution.finished", map[string]string{
			"status": "failed", "error": "上游模型失败",
		}),
	})
	if got := state.Status(); got != DramaError {
		t.Fatalf("status = %q, want error", got)
	}
	if state.ErrorMessage != "上游模型失败" || len(state.OpenCalls) != 0 ||
		len(state.OpenJobs) != 0 {
		t.Fatalf("state = %+v", state)
	}
}

// 单个远端任务失败不判死任务：Agent 被唤醒后重试，状态保持 running。
func TestJobFailureDoesNotFailDrama(t *testing.T) {
	state := NewDramaState()
	applyAll(t, &state, []core.LedgerEvent{
		mustEvent(t, 1, "user.input", map[string]string{"text": "做一集漫剧"}),
		mustEvent(t, 2, "round.started", map[string]string{}),
		mustEvent(t, 3, "assistant.tool_call", map[string]any{
			"toolCallId": "call_1", "toolName": "kling_omni_video",
		}),
		mustEvent(t, 4, "round.completed", map[string]any{}),
		mustEvent(t, 5, "tool.exec_succeeded", map[string]any{
			"toolCallId": "call_1", "result": map[string]string{"jobId": "jb_1"},
		}),
		mustEvent(t, 6, "job.submitted", map[string]any{"jobId": "jb_1", "toolCallId": "call_1"}),
		mustEvent(t, 7, "job.failed", map[string]any{
			"jobId": "jb_1", "toolCallId": "call_1", "error": "内容审核未通过",
		}),
	})
	if got := state.Status(); got != DramaRunning {
		t.Fatalf("status = %q, want running", got)
	}
}

// 未知事件 kind 不影响折叠（账本协议只增不改）。
func TestUnknownKindIgnored(t *testing.T) {
	state := NewDramaState()
	state.Apply(mustEvent(t, 1, "some.future_kind", map[string]string{"x": "y"}))
	if got := state.Status(); got != DramaIdle {
		t.Fatalf("status = %q, want idle", got)
	}
	if state.LastEventID != testEventID(1) {
		t.Fatalf("lastEventID = %q, want %q（cursor 仍要推进）",
			state.LastEventID, testEventID(1))
	}
}

// 完整消息历史：用户指令与模型各轮正文按时间序进入 Messages。
func TestMessagesKeepFullHistory(t *testing.T) {
	state := NewDramaState()
	applyAll(t, &state, []core.LedgerEvent{
		mustEvent(t, 1, "user.input", map[string]string{"text": "做一集漫剧"}),
		mustEvent(t, 2, "round.started", map[string]string{}),
		mustEvent(t, 3, "assistant.text", map[string]any{"text": "剧本已完成。"}),
		mustEvent(t, 4, "round.completed", map[string]any{}),
		mustEvent(t, 5, "user.input", map[string]string{"text": "改成雨夜背景"}),
		mustEvent(t, 6, "round.started", map[string]string{}),
		mustEvent(t, 7, "assistant.text", map[string]any{"text": "已改为雨夜。"}),
		mustEvent(t, 8, "round.completed", map[string]any{}),
	})
	want := []DramaMessage{
		{Role: "user", Text: "做一集漫剧", EventID: testEventID(1)},
		{Role: "assistant", Text: "剧本已完成。", EventID: testEventID(4)},
		{Role: "user", Text: "改成雨夜背景", EventID: testEventID(5)},
		{Role: "assistant", Text: "已改为雨夜。", EventID: testEventID(8)},
	}
	if len(state.Messages) != len(want) {
		t.Fatalf("messages = %d, want %d: %+v", len(state.Messages), len(want), state.Messages)
	}
	for index, message := range want {
		got := state.Messages[index]
		if got.Role != message.Role || got.Text != message.Text ||
			got.EventID != message.EventID {
			t.Fatalf("messages[%d] = %+v, want %+v", index, got, message)
		}
	}
	// AssistantSays 仍是最近一轮正文（旧字段兼容）。
	if state.AssistantSays != "已改为雨夜。" {
		t.Fatalf("assistantSays = %q", state.AssistantSays)
	}
}

// 资产清单：同步工具与异步 job 的产物都入账并归因到工具；
// URI 去重，list_artifacts 重放不重复入账。
func TestAssetsCollectionAndDedupe(t *testing.T) {
	state := NewDramaState()
	applyAll(t, &state, []core.LedgerEvent{
		mustEvent(t, 1, "user.input", map[string]string{"text": "做一集漫剧"}),
		mustEvent(t, 2, "round.started", map[string]string{}),
		mustEvent(t, 3, "assistant.tool_call", map[string]any{
			"toolCallId": "call_img", "toolName": "generate_image",
		}),
		mustEvent(t, 4, "assistant.tool_call", map[string]any{
			"toolCallId": "call_vid", "toolName": "kling_omni_video",
		}),
		mustEvent(t, 5, "round.completed", map[string]any{}),
		mustEvent(t, 6, "tool.exec_succeeded", map[string]any{
			"toolCallId": "call_img",
			"artifacts":  []map[string]string{{"type": "image", "uri": "https://files/char1.png"}},
		}),
		mustEvent(t, 7, "tool.exec_succeeded", map[string]any{
			"toolCallId": "call_vid", "result": map[string]string{"jobId": "jb_1"},
		}),
		mustEvent(t, 8, "job.submitted", map[string]any{"jobId": "jb_1", "toolCallId": "call_vid"}),
		// 异步产物凭 toolCallId 归因到发起工具。
		mustEvent(t, 9, "job.completed", map[string]any{
			"jobId": "jb_1", "toolCallId": "call_vid",
			"artifacts": []map[string]string{{"type": "video", "uri": "https://files/clip1.mp4"}},
		}),
		// list_artifacts 重放已有产物：不重复入账。
		mustEvent(t, 10, "assistant.tool_call", map[string]any{
			"toolCallId": "call_list", "toolName": "list_artifacts",
		}),
		mustEvent(t, 11, "tool.exec_succeeded", map[string]any{
			"toolCallId": "call_list",
			"artifacts":  []map[string]string{{"type": "image", "uri": "https://files/char1.png"}},
		}),
	})
	if len(state.Assets) != 2 {
		t.Fatalf("assets = %d, want 2: %+v", len(state.Assets), state.Assets)
	}
	if state.Assets[0].Tool != "generate_image" || state.Assets[0].Kind != "image" {
		t.Fatalf("assets[0] = %+v", state.Assets[0])
	}
	if state.Assets[1].Tool != "kling_omni_video" || state.Assets[1].Kind != "video" {
		t.Fatalf("assets[1] = %+v", state.Assets[1])
	}
	if state.ImageCount != 1 || state.VideoCount != 1 {
		t.Fatalf("counts = %d/%d, want 1/1", state.ImageCount, state.VideoCount)
	}
}

// 生成血缘：调用入参里的已知资产 URL 连到本次产物；异步 job 的产物
// 凭 toolCallId 找回入参。场景图（edit_image + 描述）归定妆列而非首帧。
func TestLineageEdgesAndSceneClassification(t *testing.T) {
	state := NewDramaState()
	applyAll(t, &state, []core.LedgerEvent{
		mustEvent(t, 1, "user.input", map[string]string{"text": "做一集漫剧"}),
		mustEvent(t, 2, "round.started", map[string]string{}),
		// 两张定妆照。
		mustEvent(t, 3, "assistant.tool_call", map[string]any{
			"toolCallId": "call_c1", "toolName": "generate_image",
			"args": map[string]string{"prompt": "角色甲定妆"},
		}),
		mustEvent(t, 4, "tool.exec_succeeded", map[string]any{
			"toolCallId": "call_c1",
			"artifacts": []map[string]string{
				{"type": "image", "uri": "https://files/char1.png", "description": "角色甲定妆三视图"},
			},
		}),
		mustEvent(t, 5, "assistant.tool_call", map[string]any{
			"toolCallId": "call_c2", "toolName": "generate_image",
		}),
		mustEvent(t, 6, "tool.exec_succeeded", map[string]any{
			"toolCallId": "call_c2",
			"artifacts": []map[string]string{
				{"type": "image", "uri": "https://files/char2.png", "description": "角色乙定妆三视图"},
			},
		}),
		// 场景图：edit_image 参考定妆照生成 → 归定妆/场景列，且有血缘边。
		mustEvent(t, 7, "assistant.tool_call", map[string]any{
			"toolCallId": "call_scene", "toolName": "edit_image",
			"args": map[string]any{"referenceImages": []string{"https://files/char1.png"}},
		}),
		mustEvent(t, 8, "tool.exec_succeeded", map[string]any{
			"toolCallId": "call_scene",
			"artifacts": []map[string]string{
				{"type": "image", "uri": "https://files/scene1.png", "description": "深夜食堂场景图"},
			},
		}),
		// 首帧：两张定妆照 + 场景图参与生成。
		mustEvent(t, 9, "assistant.tool_call", map[string]any{
			"toolCallId": "call_f1", "toolName": "edit_image",
			"args": map[string]any{"referenceImages": []string{
				"https://files/char1.png", "https://files/char2.png", "https://files/scene1.png",
			}},
		}),
		mustEvent(t, 10, "tool.exec_succeeded", map[string]any{
			"toolCallId": "call_f1",
			"artifacts": []map[string]string{
				{"type": "image", "uri": "https://files/frame1.png", "description": "第1镜首帧"},
			},
		}),
		// 镜头视频：首帧 + 定妆照做输入，产物走异步 job 回来。
		mustEvent(t, 11, "assistant.tool_call", map[string]any{
			"toolCallId": "call_v1", "toolName": "kling_omni_video",
			"args": map[string]any{
				"firstFrame":      "https://files/frame1.png",
				"referenceImages": []string{"https://files/char1.png"},
			},
		}),
		mustEvent(t, 12, "tool.exec_succeeded", map[string]any{
			"toolCallId": "call_v1", "result": map[string]string{"jobId": "jb_1"},
		}),
		mustEvent(t, 13, "job.submitted", map[string]any{"jobId": "jb_1", "toolCallId": "call_v1"}),
		mustEvent(t, 14, "job.completed", map[string]any{
			"jobId": "jb_1", "toolCallId": "call_v1",
			"artifacts": []map[string]string{
				{"type": "video", "uri": "https://files/clip1.mp4", "description": "第1镜视频"},
			},
		}),
	})

	wantEdges := []DramaEdge{
		{From: "https://files/char1.png", To: "https://files/scene1.png"},
		{From: "https://files/char1.png", To: "https://files/frame1.png"},
		{From: "https://files/char2.png", To: "https://files/frame1.png"},
		{From: "https://files/scene1.png", To: "https://files/frame1.png"},
		{From: "https://files/frame1.png", To: "https://files/clip1.mp4"},
		{From: "https://files/char1.png", To: "https://files/clip1.mp4"},
	}
	if len(state.Edges) != len(wantEdges) {
		t.Fatalf("edges = %d, want %d: %+v", len(state.Edges), len(wantEdges), state.Edges)
	}
	for index, want := range wantEdges {
		if state.Edges[index] != want {
			t.Fatalf("edges[%d] = %+v, want %+v", index, state.Edges[index], want)
		}
	}

	// 场景图按描述归定妆/场景列，首帧归首帧列。
	sceneStage := stageForAsset(state.Assets[2])
	frameStage := stageForAsset(state.Assets[3])
	if sceneStage != "characters" {
		t.Fatalf("scene stage = %q, want characters", sceneStage)
	}
	if frameStage != "frames" {
		t.Fatalf("frame stage = %q, want frames", frameStage)
	}
}

// 画布阶段投影：产物按工具归类，在途调用把对应阶段标为 active。
func TestAgentStagesProjection(t *testing.T) {
	state := NewDramaState()
	applyAll(t, &state, []core.LedgerEvent{
		mustEvent(t, 1, "user.input", map[string]string{"text": "做一集漫剧"}),
		mustEvent(t, 2, "round.started", map[string]string{}),
		mustEvent(t, 3, "assistant.text", map[string]any{"text": "剧本设计完成，开始定妆。"}),
		mustEvent(t, 4, "assistant.tool_call", map[string]any{
			"toolCallId": "call_img", "toolName": "generate_image",
		}),
		mustEvent(t, 5, "round.completed", map[string]any{}),
		mustEvent(t, 6, "tool.exec_succeeded", map[string]any{
			"toolCallId": "call_img",
			"artifacts":  []map[string]string{{"type": "image", "uri": "https://files/char1.png"}},
		}),
		mustEvent(t, 7, "round.started", map[string]string{}),
		mustEvent(t, 8, "assistant.tool_call", map[string]any{
			"toolCallId": "call_frame", "toolName": "edit_image",
		}),
		mustEvent(t, 9, "round.completed", map[string]any{}),
	})
	stages := agentStages(state, state.Status())
	byID := map[string]stageDTO{}
	for _, stage := range stages {
		byID[stage.ID] = stage
	}
	if byID["script"].Status != "done" {
		t.Fatalf("script = %q, want done", byID["script"].Status)
	}
	if byID["characters"].Status != "done" || len(byID["characters"].Assets) != 1 {
		t.Fatalf("characters = %+v", byID["characters"])
	}
	// edit_image 在途 → 首帧阶段 active。
	if byID["frames"].Status != "active" {
		t.Fatalf("frames = %q, want active", byID["frames"].Status)
	}
	if byID["clips"].Status != "pending" || byID["film"].Status != "pending" {
		t.Fatalf("clips/film = %q/%q, want pending", byID["clips"].Status, byID["film"].Status)
	}

	// 成片完成后 film=done；任务收敛后没有 active 阶段。
	applyAll(t, &state, []core.LedgerEvent{
		mustEvent(t, 10, "tool.exec_succeeded", map[string]any{
			"toolCallId": "call_frame",
			"artifacts":  []map[string]string{{"type": "image", "uri": "https://files/frame1.png"}},
		}),
		mustEvent(t, 11, "round.started", map[string]string{}),
		mustEvent(t, 12, "assistant.tool_call", map[string]any{
			"toolCallId": "call_film", "toolName": "compose_video",
		}),
		mustEvent(t, 13, "round.completed", map[string]any{}),
		mustEvent(t, 14, "tool.exec_succeeded", map[string]any{
			"toolCallId": "call_film",
			"artifacts":  []map[string]string{{"type": "video", "uri": "https://files/film.mp4"}},
		}),
		mustEvent(t, 15, "round.started", map[string]string{}),
		mustEvent(t, 16, "assistant.text", map[string]any{"text": "成片完成。"}),
		mustEvent(t, 17, "round.completed", map[string]any{}),
	})
	stages = agentStages(state, state.Status())
	for _, stage := range stages {
		switch stage.ID {
		case "clips":
			if stage.Status != "pending" {
				t.Fatalf("clips = %q, want pending（没有镜头视频产物）", stage.Status)
			}
		case "film":
			if stage.Status != "done" || len(stage.Assets) != 1 {
				t.Fatalf("film = %+v", stage)
			}
		default:
			if stage.Status == "active" {
				t.Fatalf("idle 状态不应有 active 阶段：%+v", stage)
			}
		}
	}
}

func TestCloneIsolatesState(t *testing.T) {
	state := NewDramaState()
	state.Apply(mustEvent(t, 1, "assistant.tool_call", map[string]any{
		"toolCallId": "call_1", "toolName": "generate_image",
	}))
	clone := state.Clone()
	clone.Apply(mustEvent(t, 2, "tool.exec_succeeded", map[string]any{
		"toolCallId": "call_1", "result": map[string]string{},
	}))
	if len(state.OpenCalls) != 1 {
		t.Fatalf("original OpenCalls = %v, clone mutation leaked", state.OpenCalls)
	}
	if len(clone.OpenCalls) != 0 {
		t.Fatalf("clone OpenCalls = %v", clone.OpenCalls)
	}
}
