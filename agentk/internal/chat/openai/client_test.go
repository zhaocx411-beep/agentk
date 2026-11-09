package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentk/internal/chat"
)

// 测试通过 httptest 保留 SDK 的真实传输和 SSE 解析。

// sseServer 回放 SSE 并捕获请求体。
func sseServer(t *testing.T, lines []string, capture *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s, want /v1/chat/completions", r.URL.Path)
		}
		if capture != nil {
			if err := json.NewDecoder(r.Body).Decode(capture); err != nil {
				t.Errorf("decode request: %v", err)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, line := range lines {
			_, _ = w.Write([]byte(line + "\n"))
		}
	}))
}

func newTestClient(t *testing.T, srv *httptest.Server, headers map[string]string) *Client {
	t.Helper()
	return New(Config{BaseURL: srv.URL + "/v1", APIKey: "test-key", Model: "test-model", Headers: headers})
}

func collect(t *testing.T, resp chat.Response) []chat.Delta {
	t.Helper()
	var out []chat.Delta
	for d, err := range resp.Stream {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		out = append(out, d)
	}
	return out
}

func TestStreamTextAndUsage(t *testing.T) {
	captured := map[string]any{}
	srv := sseServer(t, []string{
		`data: {"choices":[{"delta":{"content":"你"}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":"好"},"finish_reason":"stop"}]}`,
		``,
		`data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":4}}}`,
		``,
		`data: [DONE]`,
	}, &captured)
	defer srv.Close()

	resp, err := newTestClient(t, srv, nil).Stream(context.Background(), chat.Request{
		System:   "系统提示",
		Messages: []chat.Message{{Role: chat.RoleUser, Text: "打个招呼"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	deltas := collect(t, resp)
	if len(deltas) != 2 || deltas[0].Text != "你" || deltas[0].Kind != chat.BlockText || deltas[0].Block != 0 {
		t.Fatalf("deltas = %#v", deltas)
	}

	result, err := resp.Result()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Blocks) != 1 || result.Blocks[0].Text != "你好" {
		t.Fatalf("blocks = %#v, want one text block", result.Blocks)
	}
	if result.FinishReason != "stop" {
		t.Errorf("finishReason = %q, want stop", result.FinishReason)
	}
	want := chat.Usage{InputTokens: 10, OutputTokens: 2, CacheReadTokens: 4}
	if result.Usage != want {
		t.Errorf("usage = %+v, want %+v", result.Usage, want)
	}

	if captured["model"] != "test-model" || captured["stream"] != true {
		t.Errorf("request = %v", captured)
	}
	msgs, _ := captured["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %v, want system + user", captured["messages"])
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "系统提示" {
		t.Errorf("messages[0] = %v", first)
	}
	opts, _ := captured["stream_options"].(map[string]any)
	if opts["include_usage"] != true {
		t.Errorf("stream_options = %v, want include_usage", captured["stream_options"])
	}
}

func TestStreamAssemblesToolCallFragments(t *testing.T) {
	captured := map[string]any{}
	srv := sseServer(t, []string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"echo","arguments":""}}]}}]}`,
		``,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"msg\":"}}]}}]}`,
		``,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"hi\"}"}}]}}]}`,
		``,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		``,
		`data: [DONE]`,
	}, &captured)
	defer srv.Close()

	resp, err := newTestClient(t, srv, nil).Stream(context.Background(), chat.Request{
		Messages: []chat.Message{{Role: chat.RoleUser, Text: "回显 hi"}},
		Tools: []chat.ToolDef{{
			Name: "echo", Description: "回显",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}}}`),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if deltas := collect(t, resp); len(deltas) != 0 {
		t.Errorf("deltas = %#v, want none (tool call args are not streamed to clients)", deltas)
	}

	result, err := resp.Result()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("toolCalls = %#v, want one", result.ToolCalls)
	}
	call := result.ToolCalls[0]
	if call.ID != "call_abc" || call.Name != "echo" || string(call.Args) != `{"msg":"hi"}` {
		t.Errorf("toolCall = %#v", call)
	}

	tools, _ := captured["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %v, want one", captured["tools"])
	}
	fn, _ := tools[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "echo" || fn["description"] != "回显" {
		t.Errorf("tool function = %v", fn)
	}
	if _, ok := fn["parameters"].(map[string]any); !ok {
		t.Errorf("tool parameters = %v, want the schema object", fn["parameters"])
	}
}

// TestStreamCapturesReasoning 覆盖 ExtraFields 中的 reasoning_content。
func TestStreamCapturesReasoning(t *testing.T) {
	srv := sseServer(t, []string{
		`data: {"choices":[{"delta":{"reasoning_content":"想一"}}]}`,
		``,
		`data: {"choices":[{"delta":{"reasoning_content":"想"}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":"答案是 42"},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
	}, nil)
	defer srv.Close()

	resp, err := newTestClient(t, srv, nil).Stream(context.Background(), chat.Request{
		Messages: []chat.Message{{Role: chat.RoleUser, Text: "答案是什么"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	deltas := collect(t, resp)
	if len(deltas) != 3 {
		t.Fatalf("deltas = %#v, want two reasoning + one text", deltas)
	}
	if deltas[0].Kind != chat.BlockReasoning || deltas[0].Block != 0 {
		t.Errorf("deltas[0] = %#v, want reasoning block 0", deltas[0])
	}
	if deltas[2].Kind != chat.BlockText || deltas[2].Block != 1 {
		t.Errorf("deltas[2] = %#v, want text block 1", deltas[2])
	}

	result, err := resp.Result()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Blocks) != 2 {
		t.Fatalf("blocks = %#v, want reasoning then text", result.Blocks)
	}
	if result.Blocks[0].Kind != chat.BlockReasoning || result.Blocks[0].Text != "想一想" {
		t.Errorf("blocks[0] = %#v", result.Blocks[0])
	}
	if result.Blocks[1].Kind != chat.BlockText || result.Blocks[1].Text != "答案是 42" {
		t.Errorf("blocks[1] = %#v", result.Blocks[1])
	}
}

// TestStreamAssistantMessageDropsReasoning 验证 assistant 输入不发送 reasoning_content。
func TestStreamAssistantMessageDropsReasoning(t *testing.T) {
	captured := map[string]any{}
	srv := sseServer(t, []string{
		`data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
	}, &captured)
	defer srv.Close()

	resp, err := newTestClient(t, srv, nil).Stream(context.Background(), chat.Request{
		Messages: []chat.Message{
			{Role: chat.RoleUser, Text: "继续"},
			{Role: chat.RoleAssistant, Text: "我调个工具", Reasoning: "内心戏", ToolCalls: []chat.ToolCall{
				{ID: "c1", Name: "echo", Args: json.RawMessage(`{"a":1}`)},
			}},
			{Role: chat.RoleTool, ToolCallID: "c1", Text: `{"ok":true}`},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	collect(t, resp)

	body, err := json.Marshal(captured)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "内心戏") {
		t.Errorf("request leaked reasoning: %s", body)
	}
	msgs, _ := captured["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages = %v, want user + assistant + tool", captured["messages"])
	}
	asst, _ := msgs[1].(map[string]any)
	calls, _ := asst["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("assistant tool_calls = %v", asst["tool_calls"])
	}
	fn, _ := calls[0].(map[string]any)["function"].(map[string]any)
	if fn["arguments"] != `{"a":1}` {
		t.Errorf("tool call arguments = %v, want the raw JSON text", fn["arguments"])
	}
	toolMsg, _ := msgs[2].(map[string]any)
	if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "c1" {
		t.Errorf("tool message = %v", toolMsg)
	}
}

func TestStreamSurfacesUpstreamStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer srv.Close()

	resp, err := newTestClient(t, srv, nil).Stream(context.Background(), chat.Request{})
	if err != nil {
		t.Fatal(err)
	}
	var streamErr error
	for _, err := range resp.Stream {
		if err != nil {
			streamErr = err
		}
	}
	// SDK 错误转换为统一 StatusCode 接口。
	var statusErr interface{ StatusCode() int }
	if !errors.As(streamErr, &statusErr) || statusErr.StatusCode() != http.StatusUnauthorized {
		t.Fatalf("err = %T %v, want typed status 401", streamErr, streamErr)
	}
	if !strings.Contains(streamErr.Error(), "bad key") {
		t.Errorf("err = %v, want the upstream message", streamErr)
	}
	if _, err := resp.Result(); err == nil {
		t.Error("Result must fail after a failed stream")
	}
}

func TestStreamRejectsTruncatedStream(t *testing.T) {
	// 缺少 finish_reason 表示响应被截断。
	srv := sseServer(t, []string{
		`data: {"choices":[{"delta":{"content":"半"}}]}`,
		``,
	}, nil)
	defer srv.Close()

	resp, err := newTestClient(t, srv, nil).Stream(context.Background(), chat.Request{})
	if err != nil {
		t.Fatal(err)
	}
	var streamErr error
	for _, err := range resp.Stream {
		if err != nil {
			streamErr = err
		}
	}
	if streamErr == nil || !strings.Contains(streamErr.Error(), "finish reason") {
		t.Fatalf("stream err = %v, want a truncation error", streamErr)
	}
	if _, err := resp.Result(); err == nil {
		t.Error("Result must fail after a truncated stream")
	}
}

func TestStreamResultBeforeDrainFails(t *testing.T) {
	srv := sseServer(t, []string{
		`data: {"choices":[{"delta":{"content":"hi"},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
	}, nil)
	defer srv.Close()

	resp, err := newTestClient(t, srv, nil).Stream(context.Background(), chat.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resp.Result(); err == nil {
		t.Fatal("Result before draining the stream must fail")
	}
	collect(t, resp)
	if _, err := resp.Result(); err != nil {
		t.Fatalf("Result after draining: %v", err)
	}
}

func TestStreamSendsGatewayHeaders(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	headers := map[string]string{"x-api-key": "secret", "x-ks-llm-model": "m"}
	client := New(Config{BaseURL: srv.URL + "/v1", Model: "m", Headers: headers})
	headers["x-api-key"] = "mutated"
	resp, err := client.Stream(context.Background(), chat.Request{IdempotencyKey: "session:ses_1:round:1"})
	if err != nil {
		t.Fatal(err)
	}
	collect(t, resp)

	if got.Get("x-api-key") != "secret" || got.Get("x-ks-llm-model") != "m" {
		t.Errorf("headers = %v", got)
	}
	if got.Get("Idempotency-Key") != "session:ses_1:round:1" {
		t.Errorf("Idempotency-Key = %q", got.Get("Idempotency-Key"))
	}
	// APIKey 为空时不发送 Authorization。
	if got.Get("Authorization") != "" {
		t.Errorf("Authorization = %q, want none", got.Get("Authorization"))
	}
}

// TestBaseURLKeepsItsPath 验证 SDK 相对路径拼接保留 BaseURL 的最后一段。
func TestBaseURLKeepsItsPath(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
	}))
	defer srv.Close()

	for _, base := range []string{srv.URL + "/v1", srv.URL + "/v1/"} {
		client := New(Config{BaseURL: base, Model: "m"})
		resp, err := client.Stream(context.Background(), chat.Request{})
		if err != nil {
			t.Fatal(err)
		}
		collect(t, resp)
		if path != "/v1/chat/completions" {
			t.Errorf("base %q hit %q, want /v1/chat/completions", base, path)
		}
	}
}
