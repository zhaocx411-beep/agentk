package api

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"agentk/internal/capcatalog"
	"agentk/internal/event"
	"agentk/internal/types"
)

type sessionDTO struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenantId"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
}

func toSessionDTO(s types.Session) sessionDTO {
	return sessionDTO{ID: s.PublicID, TenantID: s.TenantID, Title: s.Title, CreatedAt: s.CreatedAt}
}

// encodeEvent 编码 event 包定义的 JSON envelope；事件列表和 SSE 共用该 wire 格式。
// 编码失败时仍保留事件身份和归属。
func encodeEvent(ev event.Event) json.RawMessage {
	raw, err := json.Marshal(ev)
	if err == nil {
		return raw
	}
	log.Printf("api: encode event %s (%s): %v", ev.ID, ev.Payload.Kind(), err)
	fallback, _ := json.Marshal(struct {
		EventID     string    `json:"eventId"`
		ExecutionID string    `json:"executionId"`
		RoundID     string    `json:"roundId,omitempty"`
		Kind        string    `json:"kind"`
		Data        any       `json:"data"`
		CreatedAt   time.Time `json:"createdAt"`
	}{string(ev.ID), string(ev.ExecutionID), string(ev.RoundID),
		string(ev.Payload.Kind()), nil, ev.CreatedAt})
	return fallback
}

func encodeEvents(events []event.Event) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(events))
	for _, ev := range events {
		out = append(out, encodeEvent(ev))
	}
	return out
}

type pipelineExecDTO struct {
	ID           string `json:"id"`
	SessionID    string `json:"sessionId"`
	Name         string `json:"name"`
	CapabilityID string `json:"capabilityId"`
	Status       string `json:"status"`
	// Args 是原始调用参数；Output 仅在执行完成后设置。
	Args   json.RawMessage `json:"args,omitempty"`
	Output json.RawMessage `json:"output,omitempty"`
	// ProfileVersion 是启动执行时使用的 Profile 版本。
	ProfileVersion string     `json:"profileVersion,omitempty"`
	Error          string     `json:"error,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	FinishedAt     *time.Time `json:"finishedAt,omitempty"`
}

// capabilityDTO 只暴露能力契约，不暴露 Temporal type 和 task queue，避免调用方依赖
// 会随 Worker 部署变化的内部路由。
type capabilityDTO struct {
	ID             string             `json:"id"`
	Source         string             `json:"source"`
	Kind           string             `json:"kind"`
	Description    string             `json:"description"`
	InputSchema    json.RawMessage    `json:"inputSchema"`
	OutputSchema   json.RawMessage    `json:"outputSchema"`
	TimeoutSeconds int64              `json:"timeoutSeconds"`
	LastReportedAt *time.Time         `json:"lastReportedAt,omitempty"`
	Tool           *capabilityToolDTO `json:"tool,omitempty"`
}

type capabilityToolDTO struct {
	Description string `json:"description"`
	Mode        string `json:"mode"`
	MaxAttempts int32  `json:"maxAttempts,omitempty"`
}

func toCapabilityDTO(r capcatalog.ResolvedCapability) capabilityDTO {
	def := r.Capability
	dto := capabilityDTO{
		ID:             def.ID,
		Source:         "core",
		Kind:           string(def.Kind),
		Description:    def.Description,
		InputSchema:    def.InputSchema,
		OutputSchema:   def.OutputSchema,
		TimeoutSeconds: def.TimeoutSeconds,
	}
	if strings.HasPrefix(def.ID, "contrib.") {
		dto.Source = "contrib"
		if !r.LastReportedAt.IsZero() {
			reportedAt := r.LastReportedAt
			dto.LastReportedAt = &reportedAt
		}
	}
	if def.Tool != nil {
		dto.Tool = &capabilityToolDTO{
			Description: def.Tool.Description,
			Mode:        string(def.Tool.Mode),
			MaxAttempts: def.Tool.MaxAttempts,
		}
	}
	return dto
}

func toPipelineExecDTO(e types.Execution) pipelineExecDTO {
	pipeline := e.Pipeline
	if pipeline == nil {
		pipeline = &types.PipelineExecution{}
	}
	return pipelineExecDTO{
		ID:             string(e.ID),
		SessionID:      e.SessionPublicID,
		Name:           pipeline.Name,
		CapabilityID:   pipeline.CapabilityID,
		Status:         string(e.Status),
		Args:           pipeline.Args,
		Output:         pipeline.Output,
		ProfileVersion: e.ProfileVersion,
		Error:          e.Error,
		CreatedAt:      e.CreatedAt,
		FinishedAt:     e.FinishedAt,
	}
}
