package anthropic

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

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func testClient(t *testing.T, status int, events []string, check func(*http.Request)) *ChatClient {
	t.Helper()
	var s strings.Builder
	for _, event := range events {
		var e struct{ Type string }
		if err := json.Unmarshal([]byte(event), &e); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&s, "event: %s\ndata: %s\n\n", e.Type, event)
	}
	c, err := NewChat(route.New(gateway.MustURL("https://test.invalid/klink"), nil, gateway.Bearer("key")), Config{MaxTokens: 1024}, upstream.WithHTTPClient(&http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/klink/v1/messages" || r.Header.Get("Authorization") != "Bearer key" || r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Error("invalid route or headers")
		}
		if check != nil {
			check(r)
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(s.String())), Request: r}, nil
	})}))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const start = `{"type":"message_start","message":{"id":"msg","type":"message","role":"assistant","content":[],"usage":{"input_tokens":3,"cache_creation_input_tokens":4,"cache_read_input_tokens":5}}}`
const finish = `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":7}}`
const stop = `{"type":"message_stop"}`

func TestToolRoundtrip(t *testing.T) {
	events := []string{start,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"reason"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"signature"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"native","name":"lookup","input":{}}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"n\":"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"42}"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"checking"}}`,
		`{"type":"content_block_stop","index":2}`, finish, stop}
	req := chat.Request{Model: "haiku", System: "system", Messages: []chat.Message{{Role: chat.RoleUser, Text: "lookup", Media: []chat.MediaRef{{MIMEType: "image/png", Data: []byte("png")}}}}, Tools: []chat.ToolDef{{Name: "lookup", InputSchema: json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}}}`)}}, OutputSchema: json.RawMessage(`{"type":"object","properties":{"answer":{"type":"integer"}},"required":["answer"],"additionalProperties":false}`)}
	c := testClient(t, 200, events, func(r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["max_tokens"] != float64(1024) || body["stream"] != true {
			t.Error("missing required settings")
		}
		format := body["output_config"].(map[string]any)["format"].(map[string]any)
		if format["type"] != "json_schema" {
			t.Error("schema not transmitted")
		}
		image := body["messages"].([]any)[0].(map[string]any)["content"].([]any)[1].(map[string]any)
		if image["source"].(map[string]any)["data"] != "cG5n" {
			t.Error("image not transmitted")
		}
	})
	response, err := c.Stream(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = response.Result(); err == nil {
		t.Fatal("premature result")
	}
	var deltas []chat.Delta
	for d, err := range response.Stream {
		if err != nil {
			t.Fatal(err)
		}
		deltas = append(deltas, d)
	}
	result, err := response.Result()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(deltas, []chat.Delta{{Block: 0, Kind: chat.BlockReasoning, Text: "reason"}, {Block: 1, Kind: chat.BlockText, Text: "checking"}}) || result.Usage != (chat.Usage{InputTokens: 12, OutputTokens: 7, CacheReadTokens: 5, CacheWriteTokens: 4}) {
		t.Fatalf("bad deltas or usage: %+v %+v", deltas, result.Usage)
	}
	if len(result.ToolCalls) != 1 || string(result.ToolCalls[0].Args) != `{"n":42}` {
		t.Fatalf("bad tools: %+v", result.ToolCalls)
	}
	result.ToolCalls[0].ID = "logical"
	req.Messages = append(req.Messages, chat.Message{Role: chat.RoleAssistant, Blocks: result.Blocks, ToolCalls: result.ToolCalls}, chat.Message{Role: chat.RoleTool, ToolCallID: "logical", Text: "42"})
	p, err := requestParams(req, Config{MaxTokens: 1024})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(p.Messages[1].Content)
	var blocks []map[string]any
	_ = json.Unmarshal(raw, &blocks)
	if len(blocks) != 3 || blocks[0]["signature"] != "signature" || blocks[1]["type"] != "tool_use" || blocks[2]["text"] != "checking" {
		t.Fatalf("history corrupted: %s", raw)
	}
	raw, _ = json.Marshal(p.Messages[2].Content)
	if !strings.Contains(string(raw), `"tool_use_id":"native"`) {
		t.Fatalf("wrong tool ID: %s", raw)
	}
}

func TestIncompleteAndRejectedStreams(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		events   []string
		rejected bool
	}{
		{"truncated", 200, []string{start}, false},
		{"missing-block-stop", 200, []string{start, `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"id","name":"lookup","input":{}}}`, finish, stop}, false},
		{"invalid-tool-json", 200, []string{start, `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"id","name":"lookup","input":{}}}`, `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{"}}`, `{"type":"content_block_stop","index":0}`, finish, stop}, false},
		{"bad-request", 400, nil, true}, {"rate-limit", 429, nil, false},
		{"sse-error", 200, []string{`{"type":"error","error":{"type":"invalid_request_error","message":"invalid"}}`}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testClient(t, tc.status, tc.events, nil)
			res, err := c.Stream(t.Context(), chat.Request{Model: "haiku", Messages: []chat.Message{{Role: chat.RoleUser, Text: "hello"}}})
			if err != nil {
				t.Fatal(err)
			}
			for _, err = range res.Stream {
				if err != nil {
					break
				}
			}
			if err == nil || errors.Is(err, chat.ErrRejected) != tc.rejected {
				t.Fatalf("unexpected error: %v", err)
			}
			out, err := res.Result()
			if err == nil || len(out.ToolCalls) != 0 {
				t.Fatal("partial result escaped")
			}
		})
	}
}
