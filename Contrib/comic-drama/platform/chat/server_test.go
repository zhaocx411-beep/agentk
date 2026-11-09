package chat

import (
	"bufio"
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

func newTestServer(t *testing.T) (*httptest.Server, *coretest.FakeAgentK) {
	t.Helper()
	fake := coretest.NewFakeAgentK()
	server := httptest.NewServer(NewServer(fake, DefaultRegistry(), nil).Handler())
	t.Cleanup(server.Close)
	return server, fake
}

func TestChatAgentLifecycleUsesFixedProfile(t *testing.T) {
	server, fake := newTestServer(t)

	var agents struct {
		Agents []agentDTO `json:"agents"`
	}
	coretest.DoJSON(t, http.MethodGet, server.URL+"/v1/agents", nil, http.StatusOK, &agents)
	// 目录含 8 个 chat Agent（含自动路由的 assistant）加漫剧工坊
	//（workshop 形态，供集市卡片）。
	if len(agents.Agents) != 9 {
		t.Fatalf("agents = %d, want 9", len(agents.Agents))
	}

	var created chatDTO
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/agents/media-creator/chats", map[string]string{
		"message": "生成一张纸张风海报",
	}, http.StatusCreated, &created)
	if created.AgentID != "media-creator" || created.Status != ChatRunning {
		t.Fatalf("created chat = %+v", created)
	}

	fake.Mu.Lock()
	var sessionID string
	for id := range fake.Sessions {
		sessionID = id
	}
	profile := append(json.RawMessage(nil), fake.Profiles[sessionID]...)
	fake.Mu.Unlock()
	definition, _ := DefaultRegistry().Get("media-creator")
	if !bytes.Equal(profile, definition.Profile) {
		t.Fatal("chat session did not use the fixed media profile")
	}

	fake.Seed(t, sessionID, "round.started", map[string]string{"model": "chat"})
	fake.Seed(t, sessionID, "assistant.text", map[string]string{"text": "海报已经生成。"})
	fake.Seed(t, sessionID, "assistant.tool_call", map[string]any{
		"toolCallId": "call_1", "toolName": "generate_image",
	})
	fake.Seed(t, sessionID, "tool.exec_succeeded", map[string]any{
		"toolCallId": "call_1",
		"artifacts": []map[string]string{
			{"type": "image", "uri": "https://files/poster.png"},
		},
	})
	fake.Seed(t, sessionID, "round.completed", map[string]any{})

	var fetched chatDTO
	coretest.DoJSON(t, http.MethodGet, server.URL+"/v1/chats/"+created.ID, nil, http.StatusOK, &fetched)
	if fetched.Status != ChatIdle || len(fetched.Messages) != 2 {
		t.Fatalf("fetched chat = %+v", fetched)
	}
	assistant := fetched.Messages[1]
	if assistant.Text != "海报已经生成。" || len(assistant.Artifacts) != 1 {
		t.Fatalf("assistant = %+v", assistant)
	}

	var continued chatDTO
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/chats/"+created.ID+"/messages",
		map[string]string{"text": "换成红色"},
		http.StatusAccepted, &continued,
	)
	if continued.Status != ChatRunning || len(continued.Messages) != 3 {
		t.Fatalf("continued chat = %+v", continued)
	}
}

func TestChatAnswersAskUserAndResumesConversation(t *testing.T) {
	server, fake := newTestServer(t)
	var created chatDTO
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/agents/media-creator/chats",
		map[string]string{"message": "帮我做海报"},
		http.StatusCreated, &created,
	)

	fake.Mu.Lock()
	var sessionID string
	for id := range fake.Sessions {
		sessionID = id
	}
	fake.Mu.Unlock()
	fake.Seed(t, sessionID, "round.started", map[string]any{})
	fake.Seed(t, sessionID, "assistant.tool_call", map[string]any{
		"toolCallId": "call_ask",
		"toolName":   "ask_user",
		"args": map[string]any{"questions": []map[string]any{{
			"id": "q1", "question": "选择画风", "options": []string{"写实", "水墨"},
		}}},
	})
	fake.Seed(t, sessionID, "round.completed", map[string]any{})
	fake.Seed(t, sessionID, "ask_user.requested", map[string]any{
		"toolCallId": "call_ask",
		"questions": []map[string]any{{
			"id": "q1", "question": "选择画风", "options": []string{"写实", "水墨"},
		}},
	})

	var waiting chatDTO
	coretest.DoJSON(t, http.MethodGet, server.URL+"/v1/chats/"+created.ID,
		nil, http.StatusOK, &waiting)
	if len(waiting.PendingAsks) != 1 {
		t.Fatalf("pending asks = %+v", waiting.PendingAsks)
	}

	var answered chatDTO
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/chats/"+created.ID+"/answers",
		map[string]any{
			"toolCallId": "call_ask",
			"answers": []map[string]any{{
				"questionId": "q1", "selected": []string{"水墨"},
			}},
		},
		http.StatusAccepted, &answered,
	)
	if len(answered.PendingAsks) != 0 || answered.Status != ChatRunning {
		t.Fatalf("answered chat = %+v", answered)
	}
	tool := answered.Messages[1].Tools[0]
	if tool.Status != "completed" || !strings.Contains(string(tool.Result), `"水墨"`) {
		t.Fatalf("answered tool = %+v", tool)
	}
}

func TestChatAnswerRejectsMalformedRequest(t *testing.T) {
	server, _ := newTestServer(t)
	var created chatDTO
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/agents/media-creator/chats",
		map[string]string{"message": "hello"},
		http.StatusCreated, &created,
	)
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/chats/"+created.ID+"/answers",
		map[string]any{"toolCallId": "call_ask", "answers": []map[string]any{}},
		http.StatusBadRequest, nil,
	)
}

func TestChatAPIRejectsUnknownAgentAndProfileOverride(t *testing.T) {
	server, _ := newTestServer(t)
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/agents/comic-drama/chats",
		map[string]string{"message": "hello"},
		http.StatusNotFound, nil,
	)
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/agents/media-creator/chats",
		map[string]any{"message": "hello", "profile": map[string]any{}},
		http.StatusBadRequest, nil,
	)
}

func TestChatStreamSendsInitialSnapshot(t *testing.T) {
	server, _ := newTestServer(t)
	var created chatDTO
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/agents/media-creator/chats",
		map[string]string{"message": "hello"},
		http.StatusCreated, &created,
	)

	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(server.URL + "/v1/chats/" + created.ID + "/stream")
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer response.Body.Close()
	if response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content type = %q", response.Header.Get("Content-Type"))
	}
	reader := bufio.NewReader(response.Body)
	var block strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			t.Fatalf("read stream: %v", err)
		}
		block.WriteString(line)
		if line == "\n" || err == io.EOF {
			break
		}
	}
	if !strings.Contains(block.String(), "event: snapshot") ||
		!strings.Contains(block.String(), created.ID) {
		t.Fatalf("stream block = %q", block.String())
	}
}
