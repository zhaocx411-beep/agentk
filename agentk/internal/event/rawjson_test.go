package event

import (
	"encoding/json"
	"testing"
)

// TestRawJSONIsNotBase64 验证 RawJSON 不使用 []byte 的 base64 编码。
func TestRawJSONIsNotBase64(t *testing.T) {
	call := AssistantToolCall{
		ToolCallID: "call_1",
		ToolName:   "echo",
		Args:       RawJSON(`{"text":"hi"}`),
	}
	raw, err := json.Marshal(call)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got struct {
		Args struct {
			Text string `json:"text"`
		} `json:"args"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("args did not survive as JSON: %v (%s)", err, raw)
	}
	if got.Args.Text != "hi" {
		t.Fatalf("args = %s, want inline object", raw)
	}
}

func TestRawJSONZeroValueIsNull(t *testing.T) {
	raw, err := json.Marshal(AssistantToolCall{ToolCallID: "call_1", ToolName: "noop"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(envelope["args"]) != "null" {
		t.Fatalf("args = %s, want null", envelope["args"])
	}
}

func TestRawJSONRoundTrip(t *testing.T) {
	var call AssistantToolCall
	if err := json.Unmarshal([]byte(`{"toolCallId":"c1","toolName":"t","args":{"a":[1,2]}}`), &call); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var args struct {
		A []int `json:"a"`
	}
	if err := call.Args.Into(&args); err != nil {
		t.Fatalf("into: %v", err)
	}
	if len(args.A) != 2 || args.A[1] != 2 {
		t.Fatalf("args = %+v", args)
	}
}

// TestRawJSONIntoRejectsEmpty 验证空 payload 不会被解码为零值结构。
func TestRawJSONIntoRejectsEmpty(t *testing.T) {
	var v map[string]any
	if err := RawJSON(nil).Into(&v); err == nil {
		t.Fatal("expected empty raw json to be rejected")
	}
	var call AssistantToolCall
	if err := json.Unmarshal([]byte(`{"args":null}`), &call); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if call.Args != nil {
		t.Fatalf("args = %v, want nil for JSON null", call.Args)
	}
}

func TestRawJSONRejectsInvalidJSON(t *testing.T) {
	if _, err := json.Marshal(AssistantToolCall{Args: RawJSON(`{`)}); err == nil {
		t.Fatal("expected invalid raw json to fail marshalling")
	}
}
