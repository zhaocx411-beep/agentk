package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"studio/platform/chat"
	"studio/platform/core/coretest"
)

func newTestActivities(fake *coretest.FakeAgentK) *CallAgentActivities {
	return &CallAgentActivities{
		Client:       fake,
		Registry:     chat.DefaultRegistry(),
		PollInterval: 2 * time.Millisecond,
	}
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestCallAgentCompletesTurn(t *testing.T) {
	fake := coretest.NewFakeAgentK()
	fake.OnPostMessage = func(sessionID, text string) {
		fake.Seed(t, sessionID, "round.started", map[string]any{})
		fake.Seed(t, sessionID, "assistant.text", map[string]string{"text": "海报做好了"})
		fake.Seed(t, sessionID, "tool.exec_succeeded", map[string]any{
			"toolCallId": "call_1",
			"artifacts": []map[string]string{
				{"type": "image", "uri": "https://example.invalid/poster.png"},
			},
		})
		fake.Seed(t, sessionID, "round.completed", map[string]any{})
	}

	output, err := newTestActivities(fake).Run(testContext(t), CallAgentInput{
		Agent: "media-creator", Task: "做一张猫咪咖啡馆海报",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if output.Status != "completed" || output.Reply != "海报做好了" {
		t.Fatalf("output = %+v", output)
	}
	if len(output.Artifacts) != 1 ||
		output.Artifacts[0].URI != "https://example.invalid/poster.png" {
		t.Fatalf("artifacts = %+v", output.Artifacts)
	}
	if output.SessionID == "" {
		t.Fatal("sessionId is empty")
	}
	// 子会话固化的是目标 Agent 的 profile：media-creator 不带 web_search。
	profile := string(fake.Profiles[output.SessionID])
	if !strings.Contains(profile, "kling_omni_video") ||
		strings.Contains(profile, "web_search") {
		t.Fatalf("unexpected sub-session profile: %s", profile)
	}
}

func TestCallAgentSurfacesPendingQuestions(t *testing.T) {
	fake := coretest.NewFakeAgentK()
	fake.OnPostMessage = func(sessionID, text string) {
		fake.Seed(t, sessionID, "round.started", map[string]any{})
		fake.Seed(t, sessionID, "assistant.text", map[string]string{"text": "先确认画风"})
		fake.Seed(t, sessionID, "ask_user.requested", map[string]any{
			"toolCallId": "ask_1",
			"questions": []map[string]any{{
				"id": "q1", "question": "要什么画风？",
				"options": []string{"写实", "卡通"},
			}},
		})
	}

	output, err := newTestActivities(fake).Run(testContext(t), CallAgentInput{
		Agent: "creative-studio", Task: "做一张海报",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if output.Status != "awaiting_user" {
		t.Fatalf("status = %q, want awaiting_user", output.Status)
	}
	if len(output.Questions) != 1 || output.Questions[0].Question != "要什么画风？" ||
		len(output.Questions[0].Options) != 2 {
		t.Fatalf("questions = %+v", output.Questions)
	}
}

func TestCallAgentContinuesSessionAndAnswersAsk(t *testing.T) {
	fake := coretest.NewFakeAgentK()
	sessionID, err := fake.CreateSession(
		context.Background(), "sub", json.RawMessage(`{}`),
	)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	fake.Seed(t, sessionID, "user.input", map[string]string{"text": "做个海报"})
	fake.Seed(t, sessionID, "round.started", map[string]any{})
	fake.Seed(t, sessionID, "ask_user.requested", map[string]any{
		"toolCallId": "ask_1",
		"questions":  []map[string]any{{"id": "q1", "question": "要什么画风？"}},
	})
	fake.OnAnswerQuestions = func(id, toolCallID string) {
		fake.Seed(t, id, "assistant.text", map[string]string{"text": "好的，用写实风"})
		fake.Seed(t, id, "round.completed", map[string]any{})
	}

	output, err := newTestActivities(fake).Run(testContext(t), CallAgentInput{
		Agent: "creative-studio", Task: "写实风", SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if output.Status != "completed" || output.Reply != "好的，用写实风" {
		t.Fatalf("output = %+v", output)
	}
	if output.SessionID != sessionID {
		t.Fatalf("sessionId = %q, want %q", output.SessionID, sessionID)
	}
	// 回答走 AnswerQuestions（闭合 ask_user），而不是再投一条普通消息。
	var sawAnswer bool
	fake.Mu.Lock()
	for _, event := range fake.Sessions[sessionID] {
		if event.Kind == "user.input" && strings.Contains(string(event.Data), "写实风") {
			t.Fatal("answer was posted as a plain message")
		}
		if event.Kind == "ask_user.responded" {
			sawAnswer = strings.Contains(string(event.Data), "ask_1") &&
				strings.Contains(string(event.Data), "写实风")
		}
	}
	fake.Mu.Unlock()
	if !sawAnswer {
		t.Fatal("ask_user.responded event is missing")
	}
}

func TestCallAgentRebuildsWhenSessionLost(t *testing.T) {
	fake := coretest.NewFakeAgentK()
	fake.OnPostMessage = func(sessionID, text string) {
		fake.Seed(t, sessionID, "round.started", map[string]any{})
		fake.Seed(t, sessionID, "assistant.text", map[string]string{"text": "重新开工"})
		fake.Seed(t, sessionID, "round.completed", map[string]any{})
	}

	output, err := newTestActivities(fake).Run(testContext(t), CallAgentInput{
		Agent: "audio", Task: "合成一段语音", SessionID: "ses_gone",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if output.Status != "completed" || output.SessionID == "ses_gone" {
		t.Fatalf("output = %+v", output)
	}
}

func TestCallAgentPropagatesSubAgentError(t *testing.T) {
	fake := coretest.NewFakeAgentK()
	fake.OnPostMessage = func(sessionID, text string) {
		fake.Seed(t, sessionID, "error", map[string]string{"message": "boom"})
	}

	_, err := newTestActivities(fake).Run(testContext(t), CallAgentInput{
		Agent: "web-research", Task: "查资料",
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want sub-agent failure", err)
	}
}

// 同步工具间隙：round 收在 tool_calls、工具已成功但下一个 round 未开始时，
// 折叠状态短暂 Idle，不能被当成本轮完成（否则拿到空回复）。
func TestCallAgentDoesNotFinishInToolCallGap(t *testing.T) {
	fake := coretest.NewFakeAgentK()
	fake.OnPostMessage = func(sessionID, text string) {
		fake.Seed(t, sessionID, "round.started", map[string]any{})
		fake.Seed(t, sessionID, "assistant.tool_call", map[string]any{
			"toolCallId": "call_1", "toolName": "web_search",
		})
		fake.Seed(t, sessionID, "round.completed",
			map[string]string{"finishReason": "tool_calls"})
		fake.Seed(t, sessionID, "tool.exec_succeeded",
			map[string]any{"toolCallId": "call_1", "result": map[string]any{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := newTestActivities(fake).Run(ctx, CallAgentInput{
		Agent: "web-research", Task: "查资料",
	})
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("err = %v, want deadline (should keep waiting)", err)
	}
}

// 唤醒间隙：后台 job 以 resume 档位收口后、被唤醒的 round 开始前，
// 折叠状态短暂 Idle，同样不能被当成本轮完成。
func TestCallAgentDoesNotFinishInWakeGap(t *testing.T) {
	fake := coretest.NewFakeAgentK()
	fake.OnPostMessage = func(sessionID, text string) {
		fake.Seed(t, sessionID, "round.started", map[string]any{})
		fake.Seed(t, sessionID, "assistant.text", map[string]string{"text": "已提交"})
		fake.Seed(t, sessionID, "job.submitted", map[string]string{"jobId": "job_1"})
		fake.Seed(t, sessionID, "round.completed",
			map[string]string{"finishReason": "stop"})
		fake.Seed(t, sessionID, "job.completed", map[string]any{
			"jobId": "job_1", "wakePolicy": "resume",
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := newTestActivities(fake).Run(ctx, CallAgentInput{
		Agent: "media-creator", Task: "做视频",
	})
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("err = %v, want deadline (should keep waiting)", err)
	}
}

func TestCallAgentCanTargetComicDrama(t *testing.T) {
	fake := coretest.NewFakeAgentK()
	fake.OnPostMessage = func(sessionID, text string) {
		fake.Seed(t, sessionID, "round.started", map[string]any{})
		fake.Seed(t, sessionID, "assistant.text", map[string]string{"text": "开始做漫剧"})
		fake.Seed(t, sessionID, "round.completed", map[string]any{})
	}

	output, err := newTestActivities(fake).Run(testContext(t), CallAgentInput{
		Agent: "comic-drama", Task: "雨夜橘猫进便利店，做一集漫剧",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if output.Status != "completed" || output.Reply != "开始做漫剧" {
		t.Fatalf("output = %+v", output)
	}
	profile := string(fake.Profiles[output.SessionID])
	if !strings.Contains(profile, "专门制作漫剧") {
		t.Fatalf("comic-drama sub-session did not use profile-agent: %s", profile[:min(200, len(profile))])
	}
}

func TestCallAgentRejectsBadTargets(t *testing.T) {
	fake := coretest.NewFakeAgentK()
	activities := newTestActivities(fake)
	for _, agent := range []string{"assistant", "nope", ""} {
		if _, err := activities.Run(testContext(t), CallAgentInput{
			Agent: agent, Task: "任务",
		}); err == nil {
			t.Fatalf("agent %q was accepted", agent)
		}
	}
	if _, err := activities.Run(testContext(t), CallAgentInput{
		Agent: "audio", Task: "   ",
	}); err == nil {
		t.Fatal("blank task was accepted")
	}
}
