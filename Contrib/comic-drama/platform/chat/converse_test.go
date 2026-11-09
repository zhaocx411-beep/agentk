package chat

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"studio/platform/core/coretest"
)

func newConverseTestServer(t *testing.T) (*httptest.Server, *coretest.FakeAgentK) {
	t.Helper()
	fake := coretest.NewFakeAgentK()
	server := NewServer(fake, DefaultRegistry(), nil)
	server.pollInterval = 2 * time.Millisecond
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return httpServer, fake
}

// respondWithText 让 fake 在每次收到消息后完成一轮：输出固定助手文本。
func respondWithText(t *testing.T, fake *coretest.FakeAgentK, text string) {
	t.Helper()
	fake.OnPostMessage = func(sessionID, _ string) {
		fake.Seed(t, sessionID, "round.started", map[string]any{})
		fake.Seed(t, sessionID, "assistant.text", map[string]string{"text": text})
		fake.Seed(t, sessionID, "round.completed", map[string]any{})
	}
}

func countUserInputs(fake *coretest.FakeAgentK) int {
	fake.Mu.Lock()
	defer fake.Mu.Unlock()
	count := 0
	for _, events := range fake.Sessions {
		for _, event := range events {
			if event.Kind == "user.input" {
				count++
			}
		}
	}
	return count
}

func sessionCount(fake *coretest.FakeAgentK) int {
	fake.Mu.Lock()
	defer fake.Mu.Unlock()
	return len(fake.Sessions)
}

func lastPostedText(fake *coretest.FakeAgentK) string {
	fake.Mu.Lock()
	defer fake.Mu.Unlock()
	last := ""
	lastExecution := ""
	for _, events := range fake.Sessions {
		for _, event := range events {
			if event.Kind != "user.input" || event.ExecutionID < lastExecution {
				continue
			}
			var data struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(event.Data, &data); err == nil {
				last = data.Text
				lastExecution = event.ExecutionID
			}
		}
	}
	return last
}

func TestConverseFirstMessagePostsPlainText(t *testing.T) {
	server, fake := newConverseTestServer(t)
	respondWithText(t, fake, "你好！")

	var response converseResponse
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/agents/media-creator/converse",
		map[string]any{"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		}},
		http.StatusOK, &response,
	)
	if response.Status != "completed" || response.Reply != "你好！" {
		t.Fatalf("response = %+v", response)
	}
	if response.ConversationID == "" || response.Rebuilt {
		t.Fatalf("response = %+v", response)
	}
	// 全新对话（单条消息）不加历史包裹，原样投递。
	if got := lastPostedText(fake); got != "hi" {
		t.Fatalf("posted text = %q", got)
	}
}

func TestConverseIncrementalReusesSession(t *testing.T) {
	server, fake := newConverseTestServer(t)
	respondWithText(t, fake, "第一轮回复")

	var first converseResponse
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/agents/media-creator/converse",
		map[string]any{"messages": []map[string]string{
			{"role": "user", "content": "u1"},
		}},
		http.StatusOK, &first,
	)

	respondWithText(t, fake, "第二轮回复")
	var second converseResponse
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/agents/media-creator/converse",
		map[string]any{
			"conversationId": first.ConversationID,
			"messages": []map[string]string{
				{"role": "user", "content": "u1"},
				{"role": "assistant", "content": first.Reply},
				{"role": "user", "content": "u2"},
			},
		},
		http.StatusOK, &second,
	)
	if second.Status != "completed" || second.Reply != "第二轮回复" || second.Rebuilt {
		t.Fatalf("second = %+v", second)
	}
	if sessionCount(fake) != 1 {
		t.Fatalf("sessions = %d, want 1 (增量复用)", sessionCount(fake))
	}
	if got := lastPostedText(fake); got != "u2" {
		t.Fatalf("posted text = %q, want 只投递最新一条", got)
	}
}

func TestConverseRebuildsAfterMappingLoss(t *testing.T) {
	fake := coretest.NewFakeAgentK()
	respondWithText(t, fake, "回复A")

	serverA := NewServer(fake, DefaultRegistry(), nil)
	serverA.pollInterval = 2 * time.Millisecond
	httpA := httptest.NewServer(serverA.Handler())
	var first converseResponse
	coretest.DoJSON(t, http.MethodPost, httpA.URL+"/v1/agents/media-creator/converse",
		map[string]any{"messages": []map[string]string{
			{"role": "user", "content": "u1"},
		}},
		http.StatusOK, &first,
	)
	httpA.Close()

	// 模拟服务重启：全新进程内映射，同一个 AgentK。
	serverB := NewServer(fake, DefaultRegistry(), nil)
	serverB.pollInterval = 2 * time.Millisecond
	httpB := httptest.NewServer(serverB.Handler())
	defer httpB.Close()

	respondWithText(t, fake, "回复B")
	var second converseResponse
	coretest.DoJSON(t, http.MethodPost, httpB.URL+"/v1/agents/media-creator/converse",
		map[string]any{
			"conversationId": first.ConversationID,
			"messages": []map[string]string{
				{"role": "user", "content": "u1"},
				{"role": "assistant", "content": first.Reply},
				{"role": "user", "content": "u2"},
			},
		},
		http.StatusOK, &second,
	)
	if second.Status != "completed" || second.Reply != "回复B" || !second.Rebuilt {
		t.Fatalf("second = %+v", second)
	}
	if sessionCount(fake) != 2 {
		t.Fatalf("sessions = %d, want 2 (重建新 Session)", sessionCount(fake))
	}
	want := "<chat_history>\n[user]\nu1\n\n[assistant]\n回复A\n\n</chat_history>\n\nu2"
	if got := lastPostedText(fake); got != want {
		t.Fatalf("seed = %q, want %q", got, want)
	}
}

func TestConverseHistoryMismatchTriggersRebuild(t *testing.T) {
	server, fake := newConverseTestServer(t)
	respondWithText(t, fake, "回复")

	var first converseResponse
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/agents/media-creator/converse",
		map[string]any{"messages": []map[string]string{
			{"role": "user", "content": "u1"},
		}},
		http.StatusOK, &first,
	)

	var second converseResponse
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/agents/media-creator/converse",
		map[string]any{
			"conversationId": first.ConversationID,
			"messages": []map[string]string{
				{"role": "user", "content": "u1 被篡改"},
				{"role": "assistant", "content": first.Reply},
				{"role": "user", "content": "u2"},
			},
		},
		http.StatusOK, &second,
	)
	if !second.Rebuilt || sessionCount(fake) != 2 {
		t.Fatalf("second = %+v, sessions = %d", second, sessionCount(fake))
	}
}

func TestConverseRunningThenResendContinuesSameTurn(t *testing.T) {
	server, fake := newConverseTestServer(t)
	// 第一次只推进到一半：没有 round.completed，轮次未收口。
	fake.OnPostMessage = func(sessionID, _ string) {
		fake.Seed(t, sessionID, "round.started", map[string]any{})
		fake.Seed(t, sessionID, "assistant.text", map[string]string{"text": "写到一半"})
	}

	body := map[string]any{
		"conversationId": "cnv_fixed",
		"messages": []map[string]string{
			{"role": "user", "content": "开始"},
		},
	}
	var running converseResponse
	coretest.DoJSON(t, http.MethodPost,
		server.URL+"/v1/agents/media-creator/converse?wait=0",
		body, http.StatusOK, &running,
	)
	if running.Status != "running" || running.Reply != "写到一半" {
		t.Fatalf("running = %+v", running)
	}

	// 补上收口事件后，原样重发同一请求 = 继续等待，而不是再投一条消息。
	fake.Mu.Lock()
	var sessionID string
	for id := range fake.Sessions {
		sessionID = id
	}
	fake.Mu.Unlock()
	fake.Seed(t, sessionID, "round.completed", map[string]any{})

	var done converseResponse
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/agents/media-creator/converse",
		body, http.StatusOK, &done,
	)
	if done.Status != "completed" || done.Reply != "写到一半" {
		t.Fatalf("done = %+v", done)
	}
	if countUserInputs(fake) != 1 {
		t.Fatalf("user inputs = %d, want 1 (重发不得重复投递)", countUserInputs(fake))
	}

	// 已收口请求的再次重发：幂等返回缓存结果，不产生任何新事件。
	var replay converseResponse
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/agents/media-creator/converse",
		body, http.StatusOK, &replay,
	)
	if replay.Status != "completed" || replay.Reply != "写到一半" ||
		countUserInputs(fake) != 1 || sessionCount(fake) != 1 {
		t.Fatalf("replay = %+v, inputs = %d, sessions = %d",
			replay, countUserInputs(fake), sessionCount(fake))
	}
}

func TestConverseSurfacesAskUserAsAwaiting(t *testing.T) {
	server, fake := newConverseTestServer(t)
	fake.OnPostMessage = func(sessionID, _ string) {
		fake.Seed(t, sessionID, "round.started", map[string]any{})
		fake.Seed(t, sessionID, "assistant.tool_call", map[string]any{
			"toolCallId": "call_ask", "toolName": "ask_user",
		})
		fake.Seed(t, sessionID, "round.completed", map[string]any{})
		fake.Seed(t, sessionID, "ask_user.requested", map[string]any{
			"toolCallId": "call_ask",
			"questions": []map[string]any{{
				"id": "q1", "question": "选择画风", "options": []string{"写实", "水墨"},
			}},
		})
	}

	var response converseResponse
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/agents/media-creator/converse",
		map[string]any{"messages": []map[string]string{
			{"role": "user", "content": "帮我做海报"},
		}},
		http.StatusOK, &response,
	)
	if response.Status != "awaiting_user" || len(response.PendingQuestions) != 1 {
		t.Fatalf("response = %+v", response)
	}
	// 问题文本进入 reply，回灌重建后模型仍能看到自己问过什么。
	if !strings.Contains(response.Reply, "选择画风") ||
		!strings.Contains(response.Reply, "写实 / 水墨") {
		t.Fatalf("reply = %q", response.Reply)
	}

	// 用户直接把答案当普通消息发回来：走增量路径。
	respondWithText(t, fake, "好的，用水墨。")
	var next converseResponse
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/agents/media-creator/converse",
		map[string]any{
			"conversationId": response.ConversationID,
			"messages": []map[string]string{
				{"role": "user", "content": "帮我做海报"},
				{"role": "assistant", "content": response.Reply},
				{"role": "user", "content": "水墨"},
			},
		},
		http.StatusOK, &next,
	)
	if next.Status != "completed" || next.Rebuilt || sessionCount(fake) != 1 {
		t.Fatalf("next = %+v, sessions = %d", next, sessionCount(fake))
	}
}

func TestConverseErrorDropsMappingForCleanRetry(t *testing.T) {
	server, fake := newConverseTestServer(t)
	fake.OnPostMessage = func(sessionID, _ string) {
		fake.Seed(t, sessionID, "round.started", map[string]any{})
		fake.Seed(t, sessionID, "execution.finished", map[string]string{
			"status": "failed", "error": "model unavailable",
		})
	}

	body := map[string]any{
		"conversationId": "cnv_err",
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	}
	var failed converseResponse
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/agents/media-creator/converse",
		body, http.StatusOK, &failed,
	)
	if failed.Status != "error" || failed.Error != "model unavailable" {
		t.Fatalf("failed = %+v", failed)
	}

	// 重试同一请求：映射已丢弃，走重建，新 Session 干净开始。
	respondWithText(t, fake, "这次成功")
	var retried converseResponse
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/agents/media-creator/converse",
		body, http.StatusOK, &retried,
	)
	if retried.Status != "completed" || retried.Reply != "这次成功" {
		t.Fatalf("retried = %+v", retried)
	}
	if sessionCount(fake) != 2 {
		t.Fatalf("sessions = %d, want 2", sessionCount(fake))
	}
}

func TestConverseValidation(t *testing.T) {
	server, _ := newConverseTestServer(t)
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/agents/media-creator/converse",
		map[string]any{"messages": []map[string]string{}},
		http.StatusBadRequest, nil,
	)
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/agents/media-creator/converse",
		map[string]any{"messages": []map[string]string{
			{"role": "user", "content": "hi"},
			{"role": "assistant", "content": "hello"},
		}},
		http.StatusBadRequest, nil,
	)
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/agents/media-creator/converse",
		map[string]any{"messages": []map[string]string{
			{"role": "system", "content": "hi"},
		}},
		http.StatusBadRequest, nil,
	)
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/agents/unknown/converse",
		map[string]any{"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		}},
		http.StatusNotFound, nil,
	)
}

func TestSeedTextFormatIsStable(t *testing.T) {
	single := []ConverseMessage{{Role: "user", Content: "只有一条"}}
	if SeedText(single) != "只有一条" {
		t.Fatalf("single = %q", SeedText(single))
	}

	short := []ConverseMessage{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "u2"},
	}
	want := "<chat_history>\n[user]\nu1\n\n[assistant]\na1\n\n</chat_history>\n\nu2"
	if got := SeedText(short); got != want {
		t.Fatalf("seed = %q, want %q", got, want)
	}

	// append-only：更长记录的序列化必须以更短记录的历史段为前缀
	//（footer 之前的部分），保证多次重建间提示词前缀共享。
	long := []ConverseMessage{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "u3"},
	}
	shortHistory := "<chat_history>\n[user]\nu1\n\n[assistant]\na1\n\n"
	if !strings.HasPrefix(SeedText(long), shortHistory) {
		t.Fatalf("long seed = %q 未以短记录历史段为前缀", SeedText(long))
	}
}

func TestConverseSelectedAgentAnnotatesRouterTurn(t *testing.T) {
	server, fake := newConverseTestServer(t)
	respondWithText(t, fake, "转交给漫剧了")

	var response converseResponse
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/agents/assistant/converse",
		map[string]any{
			"selectedAgent": "comic-drama",
			"messages": []map[string]string{
				{"role": "user", "content": "雨夜橘猫进便利店"},
			},
		},
		http.StatusOK, &response,
	)
	if response.Status != "completed" {
		t.Fatalf("status = %q", response.Status)
	}
	posted := lastPostedText(fake)
	if !strings.HasPrefix(posted, "[用户指定能力: comic-drama]\n\n") ||
		!strings.HasSuffix(posted, "雨夜橘猫进便利店") {
		t.Fatalf("posted = %q", posted)
	}
}

func TestConverseRejectsUnknownSelectedAgent(t *testing.T) {
	server, _ := newConverseTestServer(t)
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/agents/assistant/converse",
		map[string]any{
			"selectedAgent": "nope",
			"messages": []map[string]string{
				{"role": "user", "content": "hi"},
			},
		},
		http.StatusBadRequest, nil,
	)
}

func TestConverseStreamsTextDeltasAndFinalResult(t *testing.T) {
	server, fake := newConverseTestServer(t)
	fake.OnPostMessage = func(sessionID, _ string) {
		fake.Seed(t, sessionID, "round.started", map[string]any{})
		fake.SeedDelta(t, sessionID, "gen_1", 0, "reasoning", "不应暴露的思考")
		fake.SeedDelta(t, sessionID, "gen_1", 1, "text", "你")
		fake.SeedDelta(t, sessionID, "gen_1", 2, "text", "好")
		fake.Seed(t, sessionID, "assistant.text", map[string]string{"text": "你好"})
		fake.Seed(t, sessionID, "round.completed", map[string]any{})
	}

	body := map[string]any{
		"conversationId": "cnv_stream",
		"stream":         true,
		"messages":       []map[string]string{{"role": "user", "content": "hi"}},
	}
	content, headers := doConverseSSE(t, server.URL+"/v1/agents/assistant/converse", body, "")
	if got := headers.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q", got)
	}
	if headers.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("X-Accel-Buffering = %q", headers.Get("X-Accel-Buffering"))
	}
	for _, want := range []string{
		"event: meta\n",
		"event: delta\ndata: {\"generation\":\"gen_1\",\"index\":1,\"block\":0,\"text\":\"你\"}",
		"event: delta\ndata: {\"generation\":\"gen_1\",\"index\":2,\"block\":0,\"text\":\"好\"}",
		"event: done\n",
		`"status":"completed"`,
		`"reply":"你好"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("SSE body missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "不应暴露的思考") {
		t.Fatalf("reasoning delta leaked:\n%s", content)
	}

	// 已收口请求可用 Accept 头切换到 SSE，并直接重放缓存，不重复投递消息。
	replayed, _ := doConverseSSE(
		t, server.URL+"/v1/agents/assistant/converse",
		map[string]any{
			"conversationId": "cnv_stream",
			"messages":       []map[string]string{{"role": "user", "content": "hi"}},
		},
		"text/event-stream",
	)
	if !strings.Contains(replayed, "event: done\n") || countUserInputs(fake) != 1 {
		t.Fatalf("cached SSE = %q, inputs = %d", replayed, countUserInputs(fake))
	}
}

func doConverseSSE(
	t *testing.T,
	url string,
	body any,
	accept string,
) (string, http.Header) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if accept != "" {
		request.Header.Set("Accept", accept)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	content, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, content)
	}
	return string(content), response.Header
}
