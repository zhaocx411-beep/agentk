package api

// agent-k 对话桥接：把浏览器友好的 REST + SSE 契约翻译成 agent-k 的
// Connect RPC（StartExecution / ListEvents / WatchEvents）。
//
// 对外契约（与前端 agentcut-web/src/remote 对齐）：
//
//	POST /messages {text}                      → 202 {executionId}
//	POST /answers  {toolCallId, answers[]}     → 202 {executionId}
//	GET  /events?pageToken=&limit=             → 200 {events[], nextPageToken}（新到旧）
//	GET  /stream?executionId=                  → SSE；无进行中的 Execution 时 204
//
// 事件 envelope：{eventId, executionId, roundId, kind, data, createdAt}。
// kind 是 payload oneof 的点分名（user.input / assistant.text /
// tool.exec_succeeded ...），data 是 payload 的 protojson（camelCase），
// 与旧 agent-k REST 事件的 data 字段形状兼容。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"connectrpc.com/connect"
	apiv1 "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1"
	"go.temporal.io/sdk/client"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"agentcut/entity"
)

// requireAgentSession 校验项目归属且已绑定 agent-k 会话。
func (s *Server) requireAgentSession(w http.ResponseWriter, r *http.Request) (entity.ProjectRef, bool) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return entity.ProjectRef{}, false
	}
	if s.AgentK == nil || ref.SessionID == "" {
		writeError(w, http.StatusServiceUnavailable, "agent chat is not available for this project")
		return entity.ProjectRef{}, false
	}
	return ref, true
}

func (s *Server) bindTimelineAgentKSession(ctx context.Context, projectID, sessionID string) error {
	handle, err := s.Temporal.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   entity.TimelineWorkflowID(projectID),
		UpdateName:   entity.UpdateBindAgentKSession,
		UpdateID:     "bind-agentk-session:" + sessionID,
		WaitForStage: client.WorkflowUpdateStageCompleted,
		Args:         []any{entity.BindAgentKSessionRequest{SessionID: sessionID}},
	})
	if err != nil {
		return err
	}
	return handle.Get(ctx, nil)
}

const (
	editorContextStart = "[AGENTCUT_SYSTEM_CONTEXT_V1]"
	editorContextEnd   = "[/AGENTCUT_SYSTEM_CONTEXT_V1]"
	userMessageStart   = "[AGENTCUT_USER_MESSAGE]"
	userMessageEnd     = "[/AGENTCUT_USER_MESSAGE]"
)

type agentEditorContext struct {
	TimelineRevision      uint64   `json:"timelineRevision"`
	SelectedItemIDs       []string `json:"selectedItemIds"`
	PlayheadFrame         int64    `json:"playheadFrame"`
	SelectedMediaID       string   `json:"selectedMediaId,omitempty"`
	SelectedCaptionItemID string   `json:"selectedCaptionItemId,omitempty"`
}

// messageWithEditorContext adapts structured browser state to the current Agent
// SDK, whose AgentMessageExecutionInput only exposes a text field. The explicit
// system-context envelope prevents the metadata from masquerading as user prose.
func messageWithEditorContext(text string, editorContext *agentEditorContext) (string, error) {
	if editorContext == nil {
		return text, nil
	}
	contextJSON, err := json.Marshal(editorContext)
	if err != nil {
		return "", fmt.Errorf("marshal editor context: %w", err)
	}
	return editorContextStart + "\n" + string(contextJSON) + "\n" +
		editorContextEnd + "\n" + userMessageStart + "\n" + text + "\n" +
		userMessageEnd, nil
}

func (s *Server) handleAgentMessage(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireAgentSession(w, r)
	if !ok {
		return
	}
	var req struct {
		Text          string              `json:"text"`
		ProfileID     string              `json:"profileId,omitempty"`
		MediaIDs      []string            `json:"mediaIds,omitempty"`
		EditorContext *agentEditorContext `json:"editorContext,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	if req.ProfileID != "" {
		selected, exists := s.resolveProfile(req.ProfileID)
		if !exists {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "unknown profileId", "code": "PROFILE_NOT_FOUND",
			})
			return
		}
		projectProfileID := ref.ProfileID
		if projectProfileID == "" {
			if defaultProfile, ok := s.resolveProfile(""); ok {
				projectProfileID = defaultProfile.ProfileID
			}
		}
		if selected.ProfileID != projectProfileID {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": "selected profile does not match the project's agent session; create a project with that profile",
				"code":  "PROFILE_SESSION_MISMATCH",
			})
			return
		}
	}
	if req.EditorContext != nil && req.EditorContext.PlayheadFrame < 0 {
		writeError(w, http.StatusBadRequest, "editorContext.playheadFrame must be non-negative")
		return
	}
	message, err := messageWithEditorContext(req.Text, req.EditorContext)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid editorContext")
		return
	}
	if err := s.bindTimelineAgentKSession(r.Context(), ref.ProjectID, ref.SessionID); err != nil {
		writeError(w, http.StatusBadGateway, "failed to verify project agent session")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	exec, err := s.AgentK.StartMessage(ctx, ref.SessionID, message)
	if err != nil {
		writeAgentKError(w, "start execution", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"executionId": exec.GetId()})
}

func (s *Server) handleAgentAnswers(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireAgentSession(w, r)
	if !ok {
		return
	}
	var req struct {
		ToolCallID string `json:"toolCallId"`
		Answers    []struct {
			QuestionID string   `json:"questionId"`
			Selected   []string `json:"selected"`
		} `json:"answers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ToolCallID == "" {
		writeError(w, http.StatusBadRequest, "toolCallId is required")
		return
	}
	if err := s.bindTimelineAgentKSession(r.Context(), ref.ProjectID, ref.SessionID); err != nil {
		writeError(w, http.StatusBadGateway, "failed to verify project agent session")
		return
	}
	answers := make([]*apiv1.AnswerInput, 0, len(req.Answers))
	for _, a := range req.Answers {
		answers = append(answers, &apiv1.AnswerInput{
			QuestionId: a.QuestionID,
			Selected:   a.Selected,
		})
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	exec, err := s.AgentK.StartAnswers(ctx, ref.SessionID, req.ToolCallID, answers)
	if err != nil {
		writeAgentKError(w, "start execution", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"executionId": exec.GetId()})
}

func (s *Server) handleAgentEvents(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireAgentSession(w, r)
	if !ok {
		return
	}
	pageSize := int32(0)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		pageSize = int32(n)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	resp, err := s.AgentK.ListEvents(ctx, ref.SessionID, pageSize, r.URL.Query().Get("pageToken"))
	if err != nil {
		writeAgentKError(w, "list events", err)
		return
	}
	events := make([]json.RawMessage, 0, len(resp.GetEvents()))
	for _, ev := range resp.GetEvents() {
		env, err := eventEnvelope(ev)
		if err != nil {
			log.Printf("skip event %s: %v", ev.GetEventId(), err)
			continue
		}
		events = append(events, env)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"events":        events,
		"nextPageToken": resp.GetNextPageToken(),
	})
}

func (s *Server) handleAgentStream(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireAgentSession(w, r)
	if !ok {
		return
	}
	executionID := r.URL.Query().Get("executionId")
	if executionID == "" {
		// 与旧契约对齐：没有进行中的 Execution 时返回 204，前端短暂重试。
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		latest, err := s.AgentK.LatestExecution(ctx, ref.SessionID)
		cancel()
		if err != nil {
			writeAgentKError(w, "list executions", err)
			return
		}
		if latest == nil || latest.GetStatus() != apiv1.ExecutionStatus_EXECUTION_STATUS_RUNNING {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		executionID = latest.GetId()
	}

	stream, err := s.AgentK.WatchEvents(r.Context(), ref.SessionID, executionID)
	if err != nil {
		writeAgentKError(w, "watch events", err)
		return
	}
	defer stream.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	writeFrame := func(event string, data []byte) bool {
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
			return false
		}
		if flusher != nil {
			flusher.Flush()
		}
		return true
	}

	for stream.Receive() {
		msg := stream.Msg()
		switch {
		case msg.GetCommittedEvent() != nil:
			ev := msg.GetCommittedEvent()
			env, err := eventEnvelope(ev)
			if err != nil {
				log.Printf("skip stream event %s: %v", ev.GetEventId(), err)
				continue
			}
			if !writeFrame(eventKind(ev), env) {
				return
			}
		case msg.GetDelta() != nil:
			raw, err := protoJSON.Marshal(msg.GetDelta())
			if err != nil {
				continue
			}
			if !writeFrame("delta", raw) {
				return
			}
		case msg.GetStreamEnd() != nil:
			raw, _ := json.Marshal(map[string]string{
				"executionId": msg.GetStreamEnd().GetExecutionId(),
			})
			writeFrame("stream.end", raw)
			return
		}
	}
	// 上游断开（错误或 EOF）：SSE 无法补发状态码，直接结束；前端用
	// listEvents 校准。客户端主动断开时 r.Context() 取消，同样走到这里。
	if err := stream.Err(); err != nil && r.Context().Err() == nil {
		log.Printf("agent-k stream ended with error: %v", err)
	}
}

// ---- 事件 envelope ----

var protoJSON = protojson.MarshalOptions{}

// eventKind 返回 payload oneof 的点分事件名；未知 payload 返回空串。
func eventKind(ev *apiv1.Event) string {
	kind, _ := kindAndPayload(ev)
	return kind
}

func kindAndPayload(ev *apiv1.Event) (string, proto.Message) {
	switch p := ev.GetPayload().(type) {
	case *apiv1.Event_UserInput:
		return "user.input", p.UserInput
	case *apiv1.Event_RoundStarted:
		return "round.started", p.RoundStarted
	case *apiv1.Event_RoundCompleted:
		return "round.completed", p.RoundCompleted
	case *apiv1.Event_AssistantText:
		return "assistant.text", p.AssistantText
	case *apiv1.Event_AssistantReasoning:
		return "assistant.reasoning", p.AssistantReasoning
	case *apiv1.Event_AssistantToolCall:
		return "assistant.tool_call", p.AssistantToolCall
	case *apiv1.Event_ToolExecStarted:
		return "tool.exec_started", p.ToolExecStarted
	case *apiv1.Event_ToolExecSucceeded:
		return "tool.exec_succeeded", p.ToolExecSucceeded
	case *apiv1.Event_ToolExecFailed:
		return "tool.exec_failed", p.ToolExecFailed
	case *apiv1.Event_AskUserRequested:
		return "ask_user.requested", p.AskUserRequested
	case *apiv1.Event_AskUserResponded:
		return "ask_user.responded", p.AskUserResponded
	case *apiv1.Event_ExecutionStarted:
		return "execution.started", p.ExecutionStarted
	case *apiv1.Event_ExecutionFinished:
		return "execution.finished", p.ExecutionFinished
	case *apiv1.Event_InvocationProgress:
		return "invocation.progress", p.InvocationProgress
	}
	return "", nil
}

// eventEnvelope 把 SDK Event 转成浏览器侧的 JSON envelope。
func eventEnvelope(ev *apiv1.Event) (json.RawMessage, error) {
	kind, payload := kindAndPayload(ev)
	if kind == "" {
		return nil, errors.New("unknown event payload")
	}
	data, err := protoJSON.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal %s payload: %w", kind, err)
	}
	return json.Marshal(map[string]any{
		"eventId":     ev.GetEventId(),
		"executionId": ev.GetExecutionId(),
		"roundId":     ev.GetRoundId(),
		"kind":        kind,
		"data":        json.RawMessage(data),
		"createdAt":   ev.GetCreatedAt().AsTime().Format(time.RFC3339Nano),
	})
}

// writeAgentKError 把 Connect 错误翻译成对浏览器合理的 HTTP 状态码。
func writeAgentKError(w http.ResponseWriter, action string, err error) {
	status := http.StatusBadGateway
	var cerr *connect.Error
	if errors.As(err, &cerr) {
		switch cerr.Code() {
		case connect.CodeNotFound:
			status = http.StatusNotFound
		case connect.CodeInvalidArgument:
			status = http.StatusBadRequest
		case connect.CodeFailedPrecondition, connect.CodeAlreadyExists:
			status = http.StatusConflict
		case connect.CodeResourceExhausted:
			status = http.StatusTooManyRequests
		}
	}
	writeError(w, status, "agent-k "+action+" failed: "+err.Error())
}
