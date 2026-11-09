package fold

import (
	"encoding/json"
	"slices"
	"time"

	"agentk/internal/chat"
	"agentk/internal/event"
	"agentk/internal/tool"
)

// Artifacts 是 Session 事件的查询投影，取代独立 artifact 状态表。
// 它只处理相关 event kind；History 则必须穷尽所有 kind，避免静默丢失模型上下文。

// Status 描述是否需要继续模型调用或等待用户回答。
type Status struct {
	// NeedsRound 表示事件历史末尾需要模型响应。
	NeedsRound bool
	// AwaitingAnswer 表示存在未回答且未被新用户消息取代的 ask_user 请求。
	AwaitingAnswer bool
}

// LoopStatus 根据事件日志顺序判断后续运行状态，不读取 Temporal 状态。
// 先后接手同一 Session 的 Workflow 必须从相同事件得到相同结论。
func LoopStatus(events []event.Event, opts Options) Status {
	st := Status{}
	if len(events) == 0 {
		return st
	}
	// 复用 PendingAsks，保证等待状态与待回答列表一致。
	if len(PendingAsks(events)) > 0 {
		st.AwaitingAnswer = true
		return st
	}

	// History 已处理缺失的 tool result，因此只需检查最后一条消息的角色。
	msgs := History(events, opts)
	if len(msgs) == 0 {
		return st
	}
	st.NeedsRound = msgs[len(msgs)-1].Role != chat.RoleAssistant
	return st
}

// PendingAsk 是尚未回答的 ask_user 请求。
type PendingAsk struct {
	ToolCallID event.ToolCallID
	Questions  []event.Question
	AskedAt    time.Time
}

// PendingAsks 按提出顺序返回未回答的 ask_user 请求。等待期间的新 UserInput 表示用户
// 放弃原问题并接管对话，因此清除所有等待项；LoopStatus 复用同一判据。
func PendingAsks(events []event.Event) []PendingAsk {
	var out []PendingAsk
	for _, ev := range events {
		switch payload := ev.Payload.(type) {
		case event.AskUserRequested:
			out = append(out, PendingAsk{
				ToolCallID: payload.ToolCallID,
				Questions:  payload.Questions,
				AskedAt:    ev.CreatedAt,
			})
		case event.AskUserResponded:
			out = slices.DeleteFunc(out, func(a PendingAsk) bool {
				return a.ToolCallID == payload.ToolCallID
			})
		case event.UserInput:
			out = out[:0]
		}
	}
	return out
}

// Artifacts 按产出顺序投影资源，并关联对应工具调用的名称和参数。
// URI 是资源句柄，原始调用参数保留来源关系，无需额外 parent ID。
func Artifacts(events []event.Event) []tool.ArtifactInfo {
	type callInfo struct {
		name string
		args json.RawMessage
	}
	calls := make(map[event.ToolCallID]callInfo)
	var out []tool.ArtifactInfo
	for _, ev := range events {
		if payload, ok := ev.Payload.(event.AssistantToolCall); ok {
			calls[payload.ToolCallID] = callInfo{payload.ToolName, json.RawMessage(payload.Args)}
			continue
		}
		if payload, ok := ev.Payload.(event.ToolExecSucceeded); ok {
			call := calls[payload.ToolCallID]
			out = append(out, artifactInfos(payload.Artifacts, call.name, call.args, ev.CreatedAt)...)
			continue
		}
	}
	return out
}

func artifactInfos(
	artifacts []event.Artifact,
	toolName string,
	args json.RawMessage,
	at time.Time,
) []tool.ArtifactInfo {
	out := make([]tool.ArtifactInfo, 0, len(artifacts))
	for _, a := range artifacts {
		out = append(out, tool.ArtifactInfo{
			Type:        a.Type,
			URL:         a.URI,
			Description: a.Description,
			Tool:        toolName,
			Model:       a.Model,
			CreatedAt:   at.Format(time.RFC3339),
			Input:       args,
			Params:      json.RawMessage(a.Params),
		})
	}
	return out
}
