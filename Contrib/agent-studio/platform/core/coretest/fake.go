// Package coretest 提供各 Agent 服务测试共用的 agent-k 内存仿真与
// HTTP 断言工具。
package coretest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"

	"studio/platform/core"
)

// FakeAgentK 在内存里模拟 agent-k 的会话、pipeline run 与账本。
type FakeAgentK struct {
	Mu       sync.Mutex
	Sessions map[string][]core.LedgerEvent
	Profiles map[string]json.RawMessage
	// Runs execID → 快照；StartArgs 记录启动参数供断言。
	Runs      map[string]core.PipelineSnapshot
	StartArgs map[string]core.DramaArgs
	NextID    int
	// Agent message execution 与 WatchEvents 的测试流。
	NextExecution    int
	ActiveExecutions map[string]string
	StreamFrames     map[string][]core.AgentKStreamFrame

	FailCreate  bool
	FailStart   bool
	FailRun     bool
	FailEvents  bool
	FailMessage bool
	FailAnswer  bool
	FailWatch   bool

	// OnPostMessage 在 PostMessage 追加 user.input 之后（锁外）被调用，
	// 测试用它模拟 agent 对消息的响应（Seed 后续事件）。
	OnPostMessage func(sessionID, text string)
	// OnAnswerQuestions 在 AnswerQuestions 追加 ask_user.responded 之后
	// （锁外）被调用，测试用它模拟 agent 恢复执行。
	OnAnswerQuestions func(sessionID, toolCallID string)
}

func NewFakeAgentK() *FakeAgentK {
	return &FakeAgentK{
		Sessions:         make(map[string][]core.LedgerEvent),
		Profiles:         make(map[string]json.RawMessage),
		Runs:             make(map[string]core.PipelineSnapshot),
		StartArgs:        make(map[string]core.DramaArgs),
		ActiveExecutions: make(map[string]string),
		StreamFrames:     make(map[string][]core.AgentKStreamFrame),
	}
}

func (f *FakeAgentK) CreateSession(
	_ context.Context,
	_ string,
	profile json.RawMessage,
) (string, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	if f.FailCreate {
		return "", fmt.Errorf("agent-k unavailable")
	}
	f.NextID++
	id := fmt.Sprintf("ses_%04d", f.NextID)
	f.Sessions[id] = nil
	f.Profiles[id] = append(json.RawMessage(nil), profile...)
	return id, nil
}

func (f *FakeAgentK) StartPipeline(
	_ context.Context,
	sessionID string,
	args core.DramaArgs,
) (string, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	if f.FailStart {
		return "", fmt.Errorf("agent-k unavailable")
	}
	if _, ok := f.Sessions[sessionID]; !ok {
		return "", fmt.Errorf("session %q not found", sessionID)
	}
	execID := fmt.Sprintf("exe_%04d", len(f.Runs)+1)
	f.Runs[execID] = core.PipelineSnapshot{Status: "running"}
	f.StartArgs[execID] = args
	return execID, nil
}

func (f *FakeAgentK) PostMessage(ctx context.Context, sessionID, text string) error {
	_, err := f.StartMessage(ctx, sessionID, text)
	return err
}

func (f *FakeAgentK) StartMessage(
	_ context.Context,
	sessionID string,
	text string,
) (string, error) {
	f.Mu.Lock()
	if f.FailMessage {
		f.Mu.Unlock()
		return "", fmt.Errorf("agent-k unavailable")
	}
	if _, ok := f.Sessions[sessionID]; !ok {
		f.Mu.Unlock()
		return "", fmt.Errorf("session %q not found", sessionID)
	}
	f.NextExecution++
	executionID := fmt.Sprintf("exe_agent_%04d", f.NextExecution)
	f.ActiveExecutions[sessionID] = executionID
	f.StreamFrames[executionID] = nil
	data, _ := json.Marshal(map[string]string{"text": text})
	f.appendExecutionLocked(sessionID, executionID, "user.input", data)
	hook := f.OnPostMessage
	f.Mu.Unlock()
	if hook != nil {
		hook(sessionID, text)
	}
	return executionID, nil
}

func (f *FakeAgentK) AnswerQuestions(
	_ context.Context,
	sessionID string,
	toolCallID string,
	answers []core.ChatAnswer,
) error {
	f.Mu.Lock()
	if f.FailAnswer {
		f.Mu.Unlock()
		return fmt.Errorf("agent-k unavailable")
	}
	if _, ok := f.Sessions[sessionID]; !ok {
		f.Mu.Unlock()
		return fmt.Errorf("session %q not found", sessionID)
	}
	data, _ := json.Marshal(map[string]any{
		"toolCallId": toolCallID,
		"answers":    answers,
	})
	f.appendLocked(sessionID, "ask_user.responded", data)
	hook := f.OnAnswerQuestions
	f.Mu.Unlock()
	if hook != nil {
		hook(sessionID, toolCallID)
	}
	return nil
}

func (f *FakeAgentK) appendLocked(sessionID, kind string, data json.RawMessage) {
	f.appendExecutionLocked(sessionID, f.ActiveExecutions[sessionID], kind, data)
}

func (f *FakeAgentK) appendExecutionLocked(
	sessionID, executionID, kind string,
	data json.RawMessage,
) {
	// 事件 ID 模拟 agent-k 的 evt_ UUIDv7：单调递增且字典序 == 追加序。
	id := fmt.Sprintf("evt_%08d", len(f.Sessions[sessionID])+1)
	event := core.LedgerEvent{
		ID:          id,
		ExecutionID: executionID,
		Kind:        kind,
		Data:        data,
	}
	f.Sessions[sessionID] = append(f.Sessions[sessionID], event)
	if executionID != "" {
		copy := event
		f.StreamFrames[executionID] = append(
			f.StreamFrames[executionID], core.AgentKStreamFrame{Event: &copy},
		)
	}
}

// SeedDelta 在当前 Agent execution 的实时流中追加一段未提交文本。
func (f *FakeAgentK) SeedDelta(
	t *testing.T,
	sessionID string,
	generation string,
	index int32,
	kind string,
	text string,
) {
	t.Helper()
	f.Mu.Lock()
	defer f.Mu.Unlock()
	executionID := f.ActiveExecutions[sessionID]
	if executionID == "" {
		t.Fatalf("session %q has no active execution", sessionID)
	}
	f.StreamFrames[executionID] = append(f.StreamFrames[executionID], core.AgentKStreamFrame{
		Delta: &core.AgentKDelta{
			ExecutionID: executionID, Generation: generation, Index: index,
			Kind: kind, Text: text,
		},
	})
}

// Seed 向 fake 账本追加一条事件（测试推进剧情用）。
func (f *FakeAgentK) Seed(t *testing.T, sessionID, kind string, data any) {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("encode seed event: %v", err)
	}
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.appendLocked(sessionID, kind, raw)
}

// SeedExecution 向 fake 账本追加一条带 executionId 的事件。
func (f *FakeAgentK) SeedExecution(
	t *testing.T,
	sessionID, executionID, kind string,
	data any,
) {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("encode seed execution event: %v", err)
	}
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.appendExecutionLocked(sessionID, executionID, kind, raw)
}

func (f *FakeAgentK) GetPipelineRun(
	_ context.Context,
	_ string,
	execID string,
) (core.PipelineSnapshot, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	if f.FailRun {
		return core.PipelineSnapshot{}, fmt.Errorf("agent-k unavailable")
	}
	snapshot, ok := f.Runs[execID]
	if !ok {
		return core.PipelineSnapshot{}, fmt.Errorf("pipeline run %q not found", execID)
	}
	return snapshot, nil
}

func (f *FakeAgentK) ListEvents(
	_ context.Context,
	sessionID string,
) ([]core.LedgerEvent, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	if f.FailEvents {
		return nil, fmt.Errorf("agent-k unavailable")
	}
	all, ok := f.Sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("session %q not found", sessionID)
	}
	return append([]core.LedgerEvent(nil), all...), nil
}

func (f *FakeAgentK) WatchEvents(
	_ context.Context,
	_ string,
	executionID string,
	onFrame func(core.AgentKStreamFrame) error,
) error {
	f.Mu.Lock()
	if f.FailWatch {
		f.Mu.Unlock()
		return fmt.Errorf("agent-k stream unavailable")
	}
	frames, ok := f.StreamFrames[executionID]
	if !ok {
		f.Mu.Unlock()
		return fmt.Errorf("execution stream %q not found", executionID)
	}
	frames = append([]core.AgentKStreamFrame(nil), frames...)
	f.Mu.Unlock()
	for _, frame := range frames {
		if onFrame != nil {
			if err := onFrame(frame); err != nil {
				return err
			}
		}
	}
	return nil
}

// SetRun 设置一次 pipeline run 的快照（测试推进剧情用）。
func (f *FakeAgentK) SetRun(execID string, snapshot core.PipelineSnapshot) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.Runs[execID] = snapshot
}

// SeedProgress 向 fake 账本追加一条 pipeline invocation.progress 事件。
func (f *FakeAgentK) SeedProgress(t *testing.T, sessionID, execID, stepID, message string) {
	t.Helper()
	raw, err := json.Marshal(map[string]string{
		"invocationId":   "inv_pipeline",
		"invocationKind": "pipeline",
		"invocationName": "comic_drama_film",
		"stepId":         stepID,
		"message":        message,
	})
	if err != nil {
		t.Fatalf("encode progress event: %v", err)
	}
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.appendExecutionLocked(sessionID, execID, "invocation.progress", raw)
}

// DoJSON 发送 JSON 请求并断言状态码，可选解码响应体。
func DoJSON(
	t *testing.T,
	method, url string,
	body any,
	wantStatus int,
	response any,
) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode request: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("status = %d, want %d, body: %s", resp.StatusCode, wantStatus, raw)
	}
	if response != nil {
		if err := json.Unmarshal(raw, response); err != nil {
			t.Fatalf("decode response %s: %v", raw, err)
		}
	}
}
