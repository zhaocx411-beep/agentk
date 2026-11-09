package storetest

import (
	"encoding/json"
	"reflect"
	"testing"

	"agentk/internal/event"
	"agentk/internal/types"
)

// samplePayloads 覆盖全部 kind，每个都填上非零字段——存储层丢字段只会在有值的
// 字段上暴露。顺序即写入顺序，契约测试按下标逐个比对。
//
// 新增 kind 时这里必须同步：testPayloadsSurviveRoundTrip 会核对覆盖面等于
// event.AllKinds。
var samplePayloads = []event.Payload{
	event.UserInput{Text: "给我做个猫的视频"},
	event.RoundStarted{
		Model:          "gpt-5.4",
		Dialect:        event.DialectOpenAI,
		ProfileVersion: "v7",
		PromptSHA256:   "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
		Tools:          []string{"kling_image_to_video", "ask_user"},
		WorkerID:       "worker-1",
	},
	event.RoundCompleted{
		Usage:        event.TokenUsage{InputTokens: 1200, OutputTokens: 64, CacheReadTokens: 900},
		ModelMillis:  1350,
		FinishReason: "tool_calls",
	},
	event.AssistantText{Block: 0, Text: "好，我先生成一版。"},
	event.AssistantReasoning{Block: 1, Text: "用户没给时长，按默认 5 秒。"},
	event.AssistantToolCall{
		ToolCallID: "call_1",
		ToolName:   "kling_image_to_video",
		Args:       event.RawJSON(`{"duration":5,"prompt":"a cat"}`),
	},
	event.ToolExecStarted{ToolCallID: "call_1"},
	event.ToolExecSucceeded{
		ToolCallID: "call_1",
		Result:     event.RawJSON(`{"videoUrl":"s3://bucket/org_1/v.mp4"}`),
		Artifacts: []event.Artifact{{
			Type:   "video",
			URI:    "s3://bucket/org_1/v.mp4",
			Model:  "kling-3.0",
			Params: event.RawJSON(`{"resolution":"1080p"}`),
		}},
		DurationMillis: 42000,
	},
	event.ToolExecFailed{ToolCallID: "call_2", Error: "upstream 503", DurationMillis: 800},
	event.AskUserRequested{
		ToolCallID: "call_3",
		Questions: []event.Question{{
			ID:          "q1",
			Label:       "画风",
			Question:    "想要哪种画风？",
			Options:     []string{"写实", "动画"},
			MultiSelect: true,
		}},
	},
	event.AskUserResponded{
		ToolCallID: "call_3",
		Answers:    []event.Answer{{QuestionID: "q1", Question: "想要哪种画风？", Selected: []string{"动画"}}},
	},
	event.ExecutionStarted{
		ExecutionKind:  types.ExecutionKindPipeline,
		ProfileVersion: "v1",
		Pipeline: &event.PipelineStarted{
			Name: "storyboard_film", CapabilityID: "contrib.storyboard.film",
			Args: event.RawJSON(`{"topic":"猫"}`),
		},
	},
	event.InvocationProgress{
		InvocationID:   "exe_0191f38d-99f2-7d91-9f2b-712ab18e0a00",
		InvocationKind: "pipeline",
		InvocationName: "storyboard_film",
		StepID:         "clip/0",
		Message:        "镜头 0 片段完成",
		Data:           event.RawJSON(`{"videoUrl":"s3://bucket/org_1/clip0.mp4"}`),
	},
	event.ExecutionFinished{
		Status: types.ExecutionStatusCompleted,
		Pipeline: &event.PipelineFinished{
			Output: event.RawJSON(`{"film":"s3://bucket/org_1/film.mp4","clipCount":3}`),
		},
	},
}

// assertSamePayload 比对语义而非字节：MySQL 的 JSON 列会规范化存进去的文本
// （去空白、键序不保证），字节级比对会把无害的规范化报成丢数据。
func assertSamePayload(t *testing.T, idx int, want, got event.Payload) {
	t.Helper()
	wantJSON, err := event.MarshalPayload(want)
	if err != nil {
		t.Fatalf("marshal want[%d]: %v", idx, err)
	}
	gotJSON, err := event.MarshalPayload(got)
	if err != nil {
		t.Fatalf("marshal got[%d]: %v", idx, err)
	}
	var wantAny, gotAny any
	if err := json.Unmarshal(wantJSON, &wantAny); err != nil {
		t.Fatalf("decode want[%d]: %v", idx, err)
	}
	if err := json.Unmarshal(gotJSON, &gotAny); err != nil {
		t.Fatalf("decode got[%d]: %v", idx, err)
	}
	if !reflect.DeepEqual(wantAny, gotAny) {
		t.Errorf("event %d (%s) round-trip mismatch:\n want %s\n  got %s",
			idx, want.Kind(), wantJSON, gotJSON)
	}
}

// assertSamplesCoverAllKinds 守住覆盖面：漏一个 kind 就等于那个 kind 的存储往返
// 从未被验证过。
func assertSamplesCoverAllKinds(t *testing.T) {
	t.Helper()
	covered := make(map[event.Kind]bool, len(samplePayloads))
	for _, p := range samplePayloads {
		covered[p.Kind()] = true
	}
	for _, k := range event.AllKinds {
		if !covered[k] {
			t.Errorf("samplePayloads is missing kind %q", k)
		}
	}
	if len(covered) != len(event.AllKinds) {
		t.Errorf("samplePayloads covers %d kinds, event.AllKinds has %d",
			len(covered), len(event.AllKinds))
	}
}
