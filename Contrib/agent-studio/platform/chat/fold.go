package chat

import (
	"encoding/json"
	"fmt"
	"strings"

	"studio/platform/core"
)

func (s *ChatState) Apply(ev core.LedgerEvent) {
	// eventId 是 UUIDv7，字典序即时间序；Connect 适配层每次返回全量正序日志，
	// 靠这道闸把已折叠过的前缀跳掉。
	if ev.ID <= s.LastEventID {
		return
	}
	s.LastEventID = ev.ID
	switch ev.Kind {
	case "user.input":
		for _, ask := range s.PendingAsks {
			s.finishAsk(ask.ToolCallID, map[string]any{"status": "unanswered"})
		}
		s.PendingAsks = nil
		var data struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(ev.Data, &data) == nil && strings.TrimSpace(data.Text) != "" {
			s.Messages = append(s.Messages, ChatMessage{
				ID:        eventID("usr", ev),
				Role:      "user",
				Text:      data.Text,
				Status:    "completed",
				CreatedAt: ev.CreatedAt,
			})
			s.CurrentAssistant = -1
			s.ErrorMessage = ""
			s.AwaitingResponse = true
			s.Status = ChatRunning
		}

	case "round.started":
		s.Messages = append(s.Messages, ChatMessage{
			ID:        eventID("asst", ev),
			Role:      "assistant",
			Status:    "streaming",
			CreatedAt: ev.CreatedAt,
		})
		s.CurrentAssistant = len(s.Messages) - 1
		s.AwaitingResponse = true
		s.Status = ChatRunning

	case "assistant.text":
		var data struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(ev.Data, &data) == nil {
			message := s.ensureAssistant(ev)
			message.Text += data.Text
		}

	case "assistant.reasoning":
		var data struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(ev.Data, &data) == nil {
			message := s.ensureAssistant(ev)
			message.Reasoning += data.Text
		}

	case "assistant.tool_call":
		var data struct {
			ToolCallID string          `json:"toolCallId"`
			ToolName   string          `json:"toolName"`
			Args       json.RawMessage `json:"args"`
		}
		if json.Unmarshal(ev.Data, &data) == nil && data.ToolCallID != "" {
			message := s.ensureAssistant(ev)
			message.Tools = append(message.Tools, ChatToolRun{
				CallID:    data.ToolCallID,
				Name:      data.ToolName,
				Status:    "pending",
				Arguments: append(json.RawMessage(nil), data.Args...),
			})
			s.ToolLocations[data.ToolCallID] = toolLocation{
				Message: s.CurrentAssistant,
				Tool:    len(message.Tools) - 1,
			}
		}

	case "tool.exec_started":
		var data struct {
			ToolCallID string `json:"toolCallId"`
		}
		if json.Unmarshal(ev.Data, &data) == nil {
			if tool := s.findTool(data.ToolCallID); tool != nil {
				tool.Status = "running"
			}
		}

	case "tool.exec_succeeded":
		var data struct {
			ToolCallID string          `json:"toolCallId"`
			Result     json.RawMessage `json:"result"`
			Artifacts  []ChatArtifact  `json:"artifacts"`
		}
		if json.Unmarshal(ev.Data, &data) == nil {
			if tool := s.findTool(data.ToolCallID); tool != nil {
				tool.Status = "completed"
				tool.Result = append(json.RawMessage(nil), data.Result...)
			}
			source := "tool:" + data.ToolCallID
			s.addArtifacts(source, data.Artifacts)
			s.addArtifacts(source, resultArtifacts(data.Result))
		}

	case "tool.exec_failed":
		var data struct {
			ToolCallID string `json:"toolCallId"`
			Error      string `json:"error"`
			Message    string `json:"message"`
		}
		if json.Unmarshal(ev.Data, &data) == nil {
			if tool := s.findTool(data.ToolCallID); tool != nil {
				tool.Status = "failed"
				tool.Error = firstNonEmpty(data.Error, data.Message)
			}
		}

	case "ask_user.requested":
		var data ChatPendingAsk
		if json.Unmarshal(ev.Data, &data) == nil && data.ToolCallID != "" &&
			len(data.Questions) > 0 {
			s.PendingAsks = append(s.PendingAsks, data)
			if tool := s.findTool(data.ToolCallID); tool != nil {
				tool.Status = "running"
			}
			s.AwaitingResponse = false
		}

	case "ask_user.responded":
		var data struct {
			ToolCallID string `json:"toolCallId"`
			Answers    []struct {
				QuestionID string   `json:"questionId"`
				Question   string   `json:"question"`
				Selected   []string `json:"selected"`
			} `json:"answers"`
		}
		if json.Unmarshal(ev.Data, &data) == nil && data.ToolCallID != "" {
			s.finishAsk(data.ToolCallID, map[string]any{"answers": data.Answers})
			s.removePendingAsk(data.ToolCallID)
			s.AwaitingResponse = true
		}

	case "job.submitted":
		var data struct {
			JobID string `json:"jobId"`
		}
		if json.Unmarshal(ev.Data, &data) == nil && data.JobID != "" {
			s.OpenJobs[data.JobID] = struct{}{}
		}

	case "job.completed":
		var data struct {
			JobID      string         `json:"jobId"`
			WakePolicy string         `json:"wakePolicy"`
			Artifacts  []ChatArtifact `json:"artifacts"`
		}
		if json.Unmarshal(ev.Data, &data) == nil {
			delete(s.OpenJobs, data.JobID)
			s.addArtifacts("job:"+data.JobID, data.Artifacts)
			// resume 档位的 job 终态会唤醒 Agent Loop 再跑一个 round：
			// 在那个 round 到来之前本轮仍在进行，不能被判成 Idle。
			if data.WakePolicy == "resume" {
				s.AwaitingResponse = true
			}
		}

	case "job.failed":
		var data struct {
			JobID      string `json:"jobId"`
			WakePolicy string `json:"wakePolicy"`
		}
		if json.Unmarshal(ev.Data, &data) == nil {
			delete(s.OpenJobs, data.JobID)
			if data.WakePolicy == "resume" {
				s.AwaitingResponse = true
			}
		}

	case "round.completed":
		if s.CurrentAssistant >= 0 && s.CurrentAssistant < len(s.Messages) {
			s.Messages[s.CurrentAssistant].Status = "completed"
		}
		var data struct {
			FinishReason string `json:"finishReason"`
		}
		if json.Unmarshal(ev.Data, &data) == nil {
			s.LastFinishReason = data.FinishReason
		}
		s.AwaitingResponse = false

	case "execution.finished":
		// 新版 AgentK 用 execution.finished 统一宣告一次执行的终态。
		// 保留下方 error / interrupted 分支兼容旧账本。
		var data struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		}
		if json.Unmarshal(ev.Data, &data) != nil {
			break
		}
		s.AwaitingResponse = false
		switch data.Status {
		case "completed":
			if s.CurrentAssistant >= 0 && s.CurrentAssistant < len(s.Messages) &&
				s.Messages[s.CurrentAssistant].Status == "streaming" {
				s.Messages[s.CurrentAssistant].Status = "completed"
			}
		case "failed", "cancelled":
			s.ErrorMessage = strings.TrimSpace(data.Error)
			if s.ErrorMessage == "" {
				s.ErrorMessage = "execution " + data.Status
			}
			if s.CurrentAssistant >= 0 && s.CurrentAssistant < len(s.Messages) &&
				s.Messages[s.CurrentAssistant].Status == "streaming" {
				s.Messages[s.CurrentAssistant].Status = "failed"
			}
			// 执行已终止，不会再有闭合事件。把本次遗留的在途工作收口，
			// 避免用户下一次对话结束后仍被旧工具卡在 running。
			for messageIndex := range s.Messages {
				for toolIndex := range s.Messages[messageIndex].Tools {
					tool := &s.Messages[messageIndex].Tools[toolIndex]
					if tool.Status == "pending" || tool.Status == "running" {
						tool.Status = "failed"
						tool.Error = s.ErrorMessage
					}
				}
			}
			s.OpenJobs = make(map[string]struct{})
		}

	case "error", "interrupted":
		var data struct {
			Message string `json:"message"`
			Detail  string `json:"detail"`
			Reason  string `json:"reason"`
		}
		if json.Unmarshal(ev.Data, &data) == nil {
			s.ErrorMessage = firstNonEmpty(data.Message, data.Detail, data.Reason)
		}
		if s.CurrentAssistant >= 0 && s.CurrentAssistant < len(s.Messages) &&
			s.Messages[s.CurrentAssistant].Status == "streaming" {
			s.Messages[s.CurrentAssistant].Status = "failed"
		}
		s.AwaitingResponse = false
	}
	s.recomputeStatus()
}

func (s *ChatState) ensureAssistant(ev core.LedgerEvent) *ChatMessage {
	if s.CurrentAssistant >= 0 && s.CurrentAssistant < len(s.Messages) {
		return &s.Messages[s.CurrentAssistant]
	}
	s.Messages = append(s.Messages, ChatMessage{
		ID:        eventID("asst", ev),
		Role:      "assistant",
		Status:    "streaming",
		CreatedAt: ev.CreatedAt,
	})
	s.CurrentAssistant = len(s.Messages) - 1
	return &s.Messages[s.CurrentAssistant]
}

func (s *ChatState) findTool(callID string) *ChatToolRun {
	location, ok := s.ToolLocations[callID]
	if !ok || location.Message < 0 || location.Message >= len(s.Messages) {
		return nil
	}
	tools := s.Messages[location.Message].Tools
	if location.Tool < 0 || location.Tool >= len(tools) {
		return nil
	}
	return &s.Messages[location.Message].Tools[location.Tool]
}

func (s *ChatState) finishAsk(callID string, result any) {
	tool := s.findTool(callID)
	if tool == nil {
		return
	}
	tool.Status = "completed"
	if raw, err := json.Marshal(result); err == nil {
		tool.Result = raw
	}
}

func (s *ChatState) removePendingAsk(callID string) {
	for index, ask := range s.PendingAsks {
		if ask.ToolCallID != callID {
			continue
		}
		s.PendingAsks = append(s.PendingAsks[:index], s.PendingAsks[index+1:]...)
		return
	}
}

func (s *ChatState) addArtifacts(source string, artifacts []ChatArtifact) {
	if len(artifacts) == 0 {
		return
	}
	hadAssistant := s.CurrentAssistant >= 0 && s.CurrentAssistant < len(s.Messages)
	message := s.ensureAssistant(core.LedgerEvent{ID: s.LastEventID})
	if !hadAssistant {
		message.Status = "completed"
	}
	for _, artifact := range artifacts {
		artifact.Type = strings.TrimSpace(artifact.Type)
		artifact.URI = strings.TrimSpace(artifact.URI)
		if artifact.URI == "" {
			continue
		}
		// 同一来源会在事件顶层和 result.artifacts 各出现一次；按工具调用或
		// Job 去重，避免吞掉同一消息内其他调用错误复用同一 URI 的证据。
		key := message.ID + "\x00" + source + "\x00" +
			artifact.Type + "\x00" + artifact.URI
		if _, exists := s.SeenArtifacts[key]; exists {
			continue
		}
		s.SeenArtifacts[key] = struct{}{}
		artifact.Source = source
		message.Artifacts = append(message.Artifacts, artifact)
	}
}

func (s *ChatState) recomputeStatus() {
	if s.ErrorMessage != "" {
		s.Status = ChatError
		return
	}
	if s.AwaitingResponse || len(s.OpenJobs) > 0 {
		s.Status = ChatRunning
		return
	}
	for _, message := range s.Messages {
		if message.Status == "streaming" {
			s.Status = ChatRunning
			return
		}
		for _, tool := range message.Tools {
			if tool.Status == "pending" || tool.Status == "running" {
				s.Status = ChatRunning
				return
			}
		}
	}
	s.Status = ChatIdle
}

func resultArtifacts(result json.RawMessage) []ChatArtifact {
	var payload struct {
		Artifacts []ChatArtifact `json:"artifacts"`
	}
	if json.Unmarshal(result, &payload) != nil {
		return nil
	}
	return payload.Artifacts
}

func eventID(prefix string, ev core.LedgerEvent) string {
	if ev.RoundID != "" {
		return fmt.Sprintf("%s_%s_%s", prefix, ev.RoundID, ev.ID)
	}
	return fmt.Sprintf("%s_%s", prefix, ev.ID)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
