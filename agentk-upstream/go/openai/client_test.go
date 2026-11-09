package openai_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/chat"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway/kigress"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/openai"
)

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

func newTestClient(t *testing.T, srv *httptest.Server, headers http.Header) *openai.ChatClient {
	t.Helper()
	ep := route.New(gateway.MustURL(srv.URL+"/v1"), headers, gateway.Bearer("test-key"))
	client, err := openai.NewChat(ep, upstream.WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	return client
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

	resp, err := newTestClient(t, srv, nil).Stream(context.Background(), chat.Request{Model: "test-model",
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

func TestStreamEncodesUserImage(t *testing.T) {
	captured := map[string]any{}
	srv := sseServer(
		t,
		[]string{
			`data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`,
			``,
			`data: [DONE]`,
		},
		&captured,
	)
	defer srv.Close()
	resp, err := newTestClient(
		t,
		srv,
		nil,
	).Stream(
		context.Background(),
		chat.Request{
			Model: "test-model",
			Messages: []chat.Message{
				{
					Role: chat.RoleUser,
					Text: "inspect",
					Media: []chat.MediaRef{
						{
							MIMEType: "image/png",
							Data:     []byte("png"),
						},
					},
				},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, resp)
	if _, err := resp.Result(); err != nil {
		t.Fatal(err)
	}
	messages := captured["messages"].([]any)
	content := messages[0].(map[string]any)["content"].([]any)
	image := content[1].(map[string]any)["image_url"].(map[string]any)["url"]
	if image != "data:image/png;base64,cG5n" {
		t.Fatalf("image url = %v", image)
	}
}

func TestStreamRejectsToolImages(t *testing.T) {
	srv := sseServer(t, []string{`data: [DONE]`}, nil)
	t.Cleanup(srv.Close)
	_, err := newTestClient(t, srv, nil).Stream(t.Context(), chat.Request{Model: "test-model", Messages: []chat.Message{{
		Role: chat.RoleTool, ToolCallID: "call-1", Text: `{}`,
		Media: []chat.MediaRef{{MIMEType: "image/png", Data: []byte("png")}},
	}}})
	if err == nil || !strings.Contains(err.Error(), "does not support images in tool results") {
		t.Fatalf("error = %v", err)
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

	resp, err := newTestClient(t, srv, nil).Stream(context.Background(), chat.Request{Model: "test-model",
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

	resp, err := newTestClient(t, srv, nil).Stream(context.Background(), chat.Request{Model: "test-model",
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

func TestStreamAssistantMessageDropsReasoning(t *testing.T) {
	captured := map[string]any{}
	srv := sseServer(t, []string{
		`data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
	}, &captured)
	defer srv.Close()

	resp, err := newTestClient(t, srv, nil).Stream(context.Background(), chat.Request{Model: "test-model",
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

	resp, err := newTestClient(t, srv, nil).Stream(context.Background(), chat.Request{Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	var streamErr error
	for _, err := range resp.Stream {
		if err != nil {
			streamErr = err
		}
	}
	if streamErr == nil || !errors.Is(streamErr, chat.ErrRejected) {
		t.Fatalf("err = %T %v, want chat.ErrRejected for 401", streamErr, streamErr)
	}
	if !strings.Contains(streamErr.Error(), "401") || !strings.Contains(streamErr.Error(), "bad key") {
		t.Errorf("err = %v, want the upstream status and message", streamErr)
	}
	if _, err := resp.Result(); err == nil {
		t.Error("Result must fail after a failed stream")
	}
}

func TestStreamRateLimitIsNotRejected(t *testing.T) {
	bodies := map[string]string{
		"rate limit": `{"error":{"message":"slow down","code":"rate_limit_exceeded"}}`,
		"quota":      `{"error":{"message":"You exceeded your current quota","code":"insufficient_quota"}}`,
		"server":     `{"error":{"message":"overloaded"}}`,
	}
	statuses := map[string]int{"rate limit": 429, "quota": 429, "server": 503}
	wantRejected := map[string]bool{"rate limit": false, "quota": true, "server": false}
	for name := range bodies {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(statuses[name])
				_, _ = w.Write([]byte(bodies[name]))
			}))
			defer srv.Close()
			resp, err := newTestClient(t, srv, nil).Stream(context.Background(), chat.Request{Model: "test-model"})
			if err != nil {
				t.Fatal(err)
			}
			var streamErr error
			for _, err := range resp.Stream {
				if err != nil {
					streamErr = err
				}
			}
			if streamErr == nil || errors.Is(streamErr, chat.ErrRejected) != wantRejected[name] {
				t.Fatalf("err = %v, want rejected=%v", streamErr, wantRejected[name])
			}
		})
	}
}

func TestStreamRejectsTruncatedStream(t *testing.T) {
	srv := sseServer(t, []string{
		`data: {"choices":[{"delta":{"content":"半"}}]}`,
		``,
	}, nil)
	defer srv.Close()

	resp, err := newTestClient(t, srv, nil).Stream(context.Background(), chat.Request{Model: "test-model"})
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

	resp, err := newTestClient(t, srv, nil).Stream(context.Background(), chat.Request{Model: "test-model"})
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
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("KWS_SERVICE_STAGE", "PROD")
	var got http.Header
	hc := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		got = r.Header.Clone()
		if r.URL.String() != "http://kigress-gateway.internal/tenant/v1/chat/completions" {
			t.Errorf("URL = %s", r.URL)
		}
		return response(r, io.NopCloser(strings.NewReader(
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"+
				"data: [DONE]\n\n"))), nil
	})}
	tenant := kigress.NewTenant("tenant", kigress.Online, "secret")
	client, err := openai.NewChat(tenant.OpenAI("m"), upstream.WithHTTPClient(hc))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Stream(t.Context(), chat.Request{
		Model: "m", IdempotencyKey: "session:ses_1:round:1",
	})
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
	if got.Get("Authorization") != "" {
		t.Errorf("Authorization = %q", got.Get("Authorization"))
	}
}

func TestBaseURLKeepsItsPath(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
	}))
	defer srv.Close()

	for _, base := range []string{srv.URL + "/v1", srv.URL + "/v1/"} {
		client, err := openai.NewChat(route.New(gateway.MustURL(base), nil, nil), upstream.WithHTTPClient(srv.Client()))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Stream(context.Background(), chat.Request{Model: "test-model"})
		if err != nil {
			t.Fatal(err)
		}
		collect(t, resp)
		if path != "/v1/chat/completions" {
			t.Errorf("base %q hit %q, want /v1/chat/completions", base, path)
		}
	}
}
