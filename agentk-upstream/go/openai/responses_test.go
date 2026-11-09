package openai

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/chat"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
)

func responseSSE(events ...string) string {
	return "data: " + strings.Join(events, "\n\ndata: ") + "\n\n"
}

type responsesTransport func(*http.Request) (*http.Response, error)

func (f responsesTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func responsesTestClient(t *testing.T, fn func(*http.Request) (int, string)) *ResponsesClient {
	t.Helper()
	c, err := NewResponses(route.New(gateway.MustURL("https://gateway.test/v1/"), nil, gateway.Bearer("test-key")),
		upstream.WithHTTPClient(&http.Client{Transport: responsesTransport(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer test-key" {
				t.Errorf("incorrect route or authentication")
			}
			status, body := fn(r)
			return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})}))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestResponsesToolHistoryAndStreaming(t *testing.T) {
	output := `[{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"opaque"},{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{\"city\":\"北京\"}"},{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"查询中","annotations":[]}]}]`
	calls := 0
	c := responsesTestClient(t, func(r *http.Request) (int, string) {
		calls++
		var body struct {
			Model, Instructions string
			Stream, Store       bool
			Input               []map[string]any
			Tools               []map[string]any
			Include             []string
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "gpt-6-sol" || body.Instructions != "Be helpful" || !body.Stream || body.Store ||
			!reflect.DeepEqual(body.Include, []string{"reasoning.encrypted_content"}) || r.Header.Get("Idempotency-Key") != "round-1" {
			t.Errorf("request settings mismatch: %+v", body)
		}
		if body.Tools[0]["strict"] != false {
			t.Error("optional tool parameters must remain optional")
		}
		if calls == 1 {
			content := body.Input[0]["content"].([]any)
			if content[1].(map[string]any)["image_url"] != "data:image/png;base64,cG5n" {
				t.Error("image missing")
			}
			return 200, responseSSE(
				`{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning"}}`,
				`{"type":"response.output_item.added","output_index":2,"item":{"type":"message"}}`,
				`{"type":"response.output_text.delta","output_index":2,"delta":"查询中"}`,
				fmt.Sprintf(`{"type":"response.completed","response":{"status":"completed","output":%s,"usage":{"input_tokens":12,"output_tokens":8,"input_tokens_details":{"cached_tokens":3}}}}`, output))
		}
		var original []map[string]any
		_ = json.Unmarshal([]byte(output), &original)
		if !reflect.DeepEqual(body.Input[1:4], original) {
			t.Errorf("history lost order or protocol state: %+v", body.Input)
		}
		if body.Input[4]["call_id"] != "call_1" || body.Input[4]["output"] != "晴天" {
			t.Error("tool output missing")
		}
		return 200, responseSSE(`{"type":"response.output_text.delta","output_index":0,"delta":"北京晴天"}`,
			`{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","id":"msg_2","role":"assistant","status":"completed","content":[{"type":"output_text","text":"北京晴天"}]}]}}`)
	})
	req := chat.Request{Model: "gpt-6-sol", System: "Be helpful", IdempotencyKey: "round-1", Messages: []chat.Message{{Role: chat.RoleUser, Text: "天气？", Media: []chat.MediaRef{{MIMEType: "image/png", Data: []byte("png")}}}}, Tools: []chat.ToolDef{{Name: "lookup", InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`)}}}
	for round := 0; round < 2; round++ {
		resp, err := c.Stream(t.Context(), req)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = resp.Result(); err == nil {
			t.Fatal("premature result accepted")
		}
		var deltas []chat.Delta
		for d, err := range resp.Stream {
			if err != nil {
				t.Fatal(err)
			}
			deltas = append(deltas, d)
		}
		out, err := resp.Result()
		if err != nil {
			t.Fatal(err)
		}
		if len(deltas) != 1 || out.Blocks[deltas[0].Block].Text != deltas[0].Text {
			t.Fatalf("wrong delta mapping: %+v %+v", deltas, out)
		}
		if round == 0 {
			if out.Usage.InputTokens != 12 || out.Usage.CacheReadTokens != 3 || len(out.ToolCalls) != 1 || out.ToolCalls[0].ID != "call_1" {
				t.Fatalf("wrong result %+v", out)
			}
			req.Messages = append(req.Messages, chat.Message{Role: chat.RoleAssistant, Blocks: out.Blocks, ToolCalls: out.ToolCalls}, chat.Message{Role: chat.RoleTool, ToolCallID: out.ToolCalls[0].ID, Text: "晴天"})
		} else if out.Blocks[0].Text != "北京晴天" {
			t.Fatalf("wrong reply %+v", out)
		}
	}
}

func TestResponsesNeverAcceptsPartialResults(t *testing.T) {
	for _, tc := range []struct {
		name           string
		status         int
		body           string
		rejected, stop bool
	}{
		{name: "truncated", status: 200, body: responseSSE(`{"type":"response.output_text.delta","output_index":0,"delta":"partial"}`)},
		{name: "incomplete", status: 200, body: responseSSE(`{"type":"response.incomplete","response":{"incomplete_details":{"reason":"max_output_tokens"}}}`)},
		{name: "consumer stopped", status: 200, body: responseSSE(`{"type":"response.output_text.delta","output_index":0,"delta":"partial"}`), stop: true},
		{name: "forbidden", status: 403, body: `{"error":{"message":"forbidden"}}`, rejected: true},
		{name: "rate limited", status: 429, body: `{"error":{"message":"rate limited"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := responsesTestClient(t, func(*http.Request) (int, string) { return tc.status, tc.body })
			r, err := c.Stream(t.Context(), chat.Request{Model: "gpt-6-sol", Messages: []chat.Message{{Role: chat.RoleUser, Text: "hi"}}})
			if err != nil {
				t.Fatal(err)
			}
			var streamErr error
			for _, err = range r.Stream {
				if err != nil {
					streamErr = err
				}
				if tc.stop {
					break
				}
			}
			if !tc.stop && streamErr == nil {
				t.Fatal("stream failure hidden")
			}
			if _, err = r.Result(); err == nil || errors.Is(err, chat.ErrRejected) != tc.rejected {
				t.Fatalf("result error=%v", err)
			}
		})
	}
}

func TestResponsesAcceptsHistoryWithoutProtocolState(t *testing.T) {
	items, err := responsesAssistant(chat.Message{Role: chat.RoleAssistant, Text: "查询中",
		Blocks:    []chat.Block{{Kind: chat.BlockText, Text: "查询中"}},
		ToolCalls: []chat.ToolCall{{ID: "internal", ProviderID: "provider", Name: "lookup", Args: json.RawMessage(`{}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].(map[string]any)["content"] != "查询中" || items[1].(map[string]any)["call_id"] != "provider" {
		t.Fatalf("legacy history changed: %+v", items)
	}
	if _, err := responsesAssistant(chat.Message{Blocks: []chat.Block{{Kind: chat.BlockReasoning, ProtocolState: `{"index":0,"item":{"type":"function_call"}}`}}}); err == nil {
		t.Fatal("corrupt reasoning state silently dropped")
	}
}
