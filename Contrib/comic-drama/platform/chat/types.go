package chat

import (
	"encoding/json"
	"time"

	"studio/platform/core"
)

type ChatStatus string

const (
	ChatRunning ChatStatus = "running"
	ChatIdle    ChatStatus = "idle"
	ChatError   ChatStatus = "error"
)

type Chat struct {
	ID        string
	AgentID   string
	SessionID string
	Title     string
	Status    ChatStatus
	State     ChatState
	CreatedAt time.Time
	UpdatedAt time.Time
}

type ChatMessage struct {
	ID        string         `json:"id"`
	Role      string         `json:"role"`
	Text      string         `json:"text,omitempty"`
	Reasoning string         `json:"reasoning,omitempty"`
	Status    string         `json:"status"`
	Tools     []ChatToolRun  `json:"tools,omitempty"`
	Artifacts []ChatArtifact `json:"artifacts,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

type ChatToolRun struct {
	CallID    string          `json:"callId"`
	Name      string          `json:"name"`
	Status    string          `json:"status"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
}

type ChatQuestion struct {
	ID          string   `json:"id"`
	Label       string   `json:"label,omitempty"`
	Question    string   `json:"question"`
	Options     []string `json:"options"`
	MultiSelect bool     `json:"multiSelect,omitempty"`
}

type ChatPendingAsk struct {
	ToolCallID string         `json:"toolCallId"`
	Questions  []ChatQuestion `json:"questions"`
}

type ChatAnswer = core.ChatAnswer

type ChatArtifact struct {
	Type        string `json:"type"`
	URI         string `json:"uri"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source,omitempty"`
}

func (a *ChatArtifact) UnmarshalJSON(data []byte) error {
	var wire struct {
		Type        string `json:"type"`
		URI         string `json:"uri"`
		URL         string `json:"url"`
		ImageURL    string `json:"imageUrl"`
		VideoURL    string `json:"videoUrl"`
		AudioURL    string `json:"audioUrl"`
		Description string `json:"description"`
		Source      string `json:"source"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	a.Type = wire.Type
	a.URI = firstNonEmpty(wire.URI, wire.URL)
	a.Description = wire.Description
	a.Source = wire.Source
	if a.URI == "" {
		switch {
		case wire.ImageURL != "":
			a.Type, a.URI = "image", wire.ImageURL
		case wire.VideoURL != "":
			a.Type, a.URI = "video", wire.VideoURL
		case wire.AudioURL != "":
			a.Type, a.URI = "audio", wire.AudioURL
		}
	}
	return nil
}

type ChatState struct {
	// LastEventID 已折叠到的最后一个事件 ID（evt_ UUIDv7，字典序 == 时间序）。
	LastEventID      string
	Status           ChatStatus
	Messages         []ChatMessage
	CurrentAssistant int
	ToolLocations    map[string]toolLocation
	OpenJobs         map[string]struct{}
	SeenArtifacts    map[string]struct{}
	PendingAsks      []ChatPendingAsk
	AwaitingResponse bool
	ErrorMessage     string
	// LastFinishReason 最近一个 round.completed 的 finishReason。
	// "tool_calls" 说明本 round 收在同步工具调用上、Agent Loop 马上会开
	// 下一个 round：此时即使折叠状态短暂 Idle，本轮也没有真正结束。
	LastFinishReason string
}

type toolLocation struct {
	Message int
	Tool    int
}

func NewChatState() ChatState {
	return ChatState{
		Status:           ChatIdle,
		CurrentAssistant: -1,
		ToolLocations:    make(map[string]toolLocation),
		OpenJobs:         make(map[string]struct{}),
		SeenArtifacts:    make(map[string]struct{}),
	}
}

type chatDTO struct {
	ID          string           `json:"id"`
	AgentID     string           `json:"agentId"`
	Title       string           `json:"title"`
	Status      ChatStatus       `json:"status"`
	Messages    []ChatMessage    `json:"messages,omitempty"`
	PendingAsks []ChatPendingAsk `json:"pendingAsks,omitempty"`
	LastEventID string           `json:"lastEventId"`
	Error       string           `json:"error,omitempty"`
	CreatedAt   time.Time        `json:"createdAt"`
	UpdatedAt   time.Time        `json:"updatedAt"`
}

func toChatDTO(chat Chat, includeMessages bool) chatDTO {
	dto := chatDTO{
		ID:          chat.ID,
		AgentID:     chat.AgentID,
		Title:       chat.Title,
		Status:      chat.State.Status,
		LastEventID: chat.State.LastEventID,
		Error:       chat.State.ErrorMessage,
		CreatedAt:   chat.CreatedAt,
		UpdatedAt:   chat.UpdatedAt,
	}
	if includeMessages {
		dto.Messages = cloneChatMessages(chat.State.Messages)
		dto.PendingAsks = clonePendingAsks(chat.State.PendingAsks)
	}
	return dto
}
