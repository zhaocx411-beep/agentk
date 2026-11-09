package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"agentk/internal/chat"
	"agentk/internal/event"
	"agentk/internal/tool"
)

func call(name string) chat.ToolCall {
	return chat.ToolCall{ID: "c1", Name: name, Args: json.RawMessage(`{"x":1}`)}
}

func onlyDraft(t *testing.T, out ToolOutcome) event.Payload {
	t.Helper()
	if len(out.Drafts) != 1 {
		t.Fatalf("drafts = %v, want exactly one", kinds(out.Drafts))
	}
	return out.Drafts[0].Payload
}

// 成功结果保持原始 JSON。
func TestExecuteToolSucceeds(t *testing.T) {
	res := testResolved(t, nil, tool.Spec{
		Name: "echo", Description: "回声",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Binding: tool.InlineBinding{Execute: func(_ context.Context, in json.RawMessage, tc tool.Context) (any, error) {
			if tc.ToolCallID != "c1" || tc.SessionID != "ses_1" {
				t.Errorf("tool context = %+v", tc)
			}
			if tc.IdempotencyKey != ToolInvocationKey("rnd_1", "c1") {
				t.Errorf("idempotency key = %q", tc.IdempotencyKey)
			}
			return map[string]json.RawMessage{"echo": in}, nil
		}},
	})

	out, err := ExecuteTool(context.Background(), res, "rnd_1", call("echo"),
		ToolEnv{SessionPublicID: "ses_1"})
	if err != nil {
		t.Fatal(err)
	}
	ok, isOK := onlyDraft(t, out).(event.ToolExecSucceeded)
	if !isOK || ok.ToolCallID != "c1" {
		t.Fatalf("payload = %+v", out.Drafts[0].Payload)
	}
	if string(ok.Result) != `{"echo":{"x":1}}` {
		t.Fatalf("result = %s", ok.Result)
	}
}

// 工具错误转换为 ToolExecFailed，不作为 ExecuteTool 错误返回。
func TestExecuteToolBusinessErrorClosesTheCall(t *testing.T) {
	res := testResolved(t, nil, tool.Spec{
		Name: "boom", Description: "炸",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Binding: tool.InlineBinding{Execute: func(context.Context, json.RawMessage, tool.Context) (any, error) {
			return nil, errors.New("上游拒绝")
		}},
	})

	out, err := ExecuteTool(context.Background(), res, "rnd_1", call("boom"), ToolEnv{})
	if err != nil {
		t.Fatalf("a tool error must not fail the round: %v", err)
	}
	failed, ok := onlyDraft(t, out).(event.ToolExecFailed)
	if !ok || failed.Error != "上游拒绝" || failed.ToolCallID != "c1" {
		t.Fatalf("payload = %+v", out.Drafts[0].Payload)
	}
}

// 未知工具生成 ToolExecFailed。
func TestExecuteToolUnknownToolFails(t *testing.T) {
	out, err := ExecuteTool(context.Background(), testResolved(t, nil), "rnd_1",
		call("nope"), ToolEnv{})
	if err != nil {
		t.Fatal(err)
	}
	failed, ok := onlyDraft(t, out).(event.ToolExecFailed)
	if !ok || !strings.Contains(failed.Error, "nope") {
		t.Fatalf("payload = %+v", out.Drafts[0].Payload)
	}
}

// 工具超时生成 ToolExecFailed。
func TestExecuteToolTimeoutClosesTheCall(t *testing.T) {
	res := testResolved(t, nil, tool.Spec{
		Name: "slow", Description: "慢", Timeout: 20 * time.Millisecond,
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Binding: tool.InlineBinding{Execute: func(ctx context.Context, _ json.RawMessage, _ tool.Context) (any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}},
	})

	out, err := ExecuteTool(context.Background(), res, "rnd_1", call("slow"), ToolEnv{})
	if err != nil {
		t.Fatal(err)
	}
	failed, ok := onlyDraft(t, out).(event.ToolExecFailed)
	if !ok || !strings.Contains(failed.Error, "timed out") {
		t.Fatalf("payload = %+v", out.Drafts[0].Payload)
	}
}

// 父 context 取消直接返回，不生成工具事件。
func TestExecuteToolPropagatesCancellation(t *testing.T) {
	res := testResolved(t, nil, tool.Spec{
		Name: "slow", Description: "慢",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Binding: tool.InlineBinding{Execute: func(ctx context.Context, _ json.RawMessage, _ tool.Context) (any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}},
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	if _, err := ExecuteTool(ctx, res, "rnd_1", call("slow"), ToolEnv{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// ActivityBinding 被误路由到 ExecuteTool 时生成 ToolExecFailed。
func TestExecuteToolRejectsActivityBinding(t *testing.T) {
	res := testResolved(t, nil, tool.Spec{
		Name: "gen", Description: "生成",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Binding: tool.ActivityBinding{
			ActivityName: "test.capability.gen",
		},
	})

	out, err := ExecuteTool(context.Background(), res, "rnd_1", call("gen"),
		ToolEnv{SessionPublicID: "ses_1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := onlyDraft(t, out).(event.ToolExecFailed); !ok {
		t.Fatalf("payload = %+v", out.Drafts[0].Payload)
	}
}

// result.artifacts 被转换为成功事件中的资源。
func TestExecuteToolRecordsResultArtifacts(t *testing.T) {
	res := testResolved(t, nil, tool.Spec{
		Name: "draw", Description: "画",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Binding: tool.InlineBinding{Execute: func(context.Context, json.RawMessage, tool.Context) (any, error) {
			return map[string]any{
				"artifacts": []map[string]any{{
					"type":        "image",
					"uri":         "https://cdn.example.com/a.png",
					"description": "使用 draw 生成的猫咪图片",
					"model":       "image-model",
					"params":      map[string]string{"size": "1024x1024"},
				}, {
					"type": "image",
					"uri":  "https://cdn.example.com/b.png",
				}},
			}, nil
		}},
	})

	out, err := ExecuteTool(context.Background(), res, "rnd_1", call("draw"), ToolEnv{})
	if err != nil {
		t.Fatal(err)
	}
	ok := onlyDraft(t, out).(event.ToolExecSucceeded)
	if len(ok.Artifacts) != 2 {
		t.Fatalf("artifacts = %+v", ok.Artifacts)
	}
	art := ok.Artifacts[0]
	if art.URI != "https://cdn.example.com/a.png" || art.Type != "image" ||
		art.Description != "使用 draw 生成的猫咪图片" ||
		art.Model != "image-model" {
		t.Fatalf("artifact = %+v", art)
	}
	if string(art.Params) != `{"size":"1024x1024"}` {
		t.Fatalf("artifact params = %s", art.Params)
	}
	if ok.Artifacts[1].URI != "https://cdn.example.com/b.png" {
		t.Fatalf("second artifact = %+v", ok.Artifacts[1])
	}
}

// 缺少 artifacts 不影响工具成功。
func TestExecuteToolWithoutArtifactIsNotFailure(t *testing.T) {
	res := testResolved(t, nil, tool.Spec{
		Name: "draw", Description: "画",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Binding: tool.InlineBinding{Execute: func(context.Context, json.RawMessage, tool.Context) (any, error) {
			return map[string]string{"note": "换了个形状"}, nil
		}},
	})

	out, err := ExecuteTool(context.Background(), res, "rnd_1", call("draw"), ToolEnv{})
	if err != nil {
		t.Fatal(err)
	}
	ok := onlyDraft(t, out).(event.ToolExecSucceeded)
	if len(ok.Artifacts) != 0 {
		t.Fatalf("artifacts = %+v", ok.Artifacts)
	}
}

func TestExecuteToolRequiresRegistry(t *testing.T) {
	if _, err := ExecuteTool(context.Background(), Resolved{}, "rnd_1",
		call("echo"), ToolEnv{}); err == nil {
		t.Fatal("an unresolved registry must fail")
	}
}
