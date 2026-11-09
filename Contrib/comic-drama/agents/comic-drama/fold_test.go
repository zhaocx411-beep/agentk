package comicdrama

import (
	"encoding/json"
	"testing"
	"time"

	"studio/platform/core"
)

func progressEvent(t *testing.T, seq int64, execID, stepID, message string) core.LedgerEvent {
	t.Helper()
	raw, err := json.Marshal(map[string]string{
		"invocationId":   "inv_pipeline",
		"invocationKind": "pipeline",
		"invocationName": "comic_drama_film",
		"stepId":         stepID,
		"message":        message,
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return core.LedgerEvent{
		ID:          testEventID(seq),
		ExecutionID: execID,
		Kind:        "invocation.progress",
		Data:        raw,
		CreatedAt:   time.Date(2026, 8, 10, 0, 0, int(seq), 0, time.UTC),
	}
}

func TestFoldProgress(t *testing.T) {
	events := []core.LedgerEvent{
		{ID: testEventID(1), Kind: "execution.started", ExecutionID: "exe_1", Data: json.RawMessage(`{}`)},
		progressEvent(t, 2, "exe_1", "script", "剧本完成"),
		// 别的 exec 的进度不掺进来。
		progressEvent(t, 3, "exe_other", "script", "别人的剧本"),
		// 非进度事件跳过。
		{ID: testEventID(4), Kind: "user.input", Data: json.RawMessage(`{"text":"hi"}`)},
		// 坏 payload 跳过。
		{ID: testEventID(5), ExecutionID: "exe_1", Kind: "invocation.progress", Data: json.RawMessage(`not-json`)},
		progressEvent(t, 6, "exe_1", "shot/0", "镜头 1 完成"),
	}

	progress := foldProgress(nil, "exe_1", events)
	if len(progress) != 2 {
		t.Fatalf("progress = %+v", progress)
	}
	if progress[0].StepID != "script" || progress[0].Message != "剧本完成" {
		t.Fatalf("first = %+v", progress[0])
	}
	if progress[1].StepID != "shot/0" {
		t.Fatalf("second = %+v", progress[1])
	}
	if progress[0].At.IsZero() {
		t.Fatalf("At should be taken from event createdAt")
	}

	// 增量折叠：在已有进度上追加。
	more := foldProgress(progress, "exe_1", []core.LedgerEvent{
		progressEvent(t, 7, "exe_1", "film", "成片完成"),
	})
	if len(more) != 3 || more[2].StepID != "film" {
		t.Fatalf("more = %+v", more)
	}
}
