package chat

import (
	"fmt"
	"sync"
	"time"
)

type MemoryChatStore struct {
	mu    sync.RWMutex
	chats map[string]Chat
	order []string
}

func NewMemoryChatStore() *MemoryChatStore {
	return &MemoryChatStore{chats: make(map[string]Chat)}
}

func (s *MemoryChatStore) Create(chat Chat) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.chats[chat.ID]; exists {
		return fmt.Errorf("chat %q already exists", chat.ID)
	}
	s.chats[chat.ID] = cloneChat(chat)
	s.order = append(s.order, chat.ID)
	return nil
}

func (s *MemoryChatStore) Get(id string) (Chat, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	chat, ok := s.chats[id]
	return cloneChat(chat), ok
}

func (s *MemoryChatStore) List() []Chat {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Chat, 0, len(s.order))
	for index := len(s.order) - 1; index >= 0; index-- {
		result = append(result, cloneChat(s.chats[s.order[index]]))
	}
	return result
}

func (s *MemoryChatStore) UpdateState(
	id string,
	state ChatState,
	updatedAt time.Time,
) (Chat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	chat, ok := s.chats[id]
	if !ok {
		return Chat{}, fmt.Errorf("chat %q not found", id)
	}
	if state.LastEventID < chat.State.LastEventID {
		return cloneChat(chat), nil
	}
	chat.State = state.Clone()
	chat.Status = state.Status
	chat.UpdatedAt = updatedAt
	s.chats[id] = chat
	return cloneChat(chat), nil
}

func cloneChat(chat Chat) Chat {
	chat.State = chat.State.Clone()
	return chat
}

func (s ChatState) Clone() ChatState {
	clone := s
	clone.Messages = cloneChatMessages(s.Messages)
	clone.ToolLocations = make(map[string]toolLocation, len(s.ToolLocations))
	for callID, location := range s.ToolLocations {
		clone.ToolLocations[callID] = location
	}
	clone.OpenJobs = make(map[string]struct{}, len(s.OpenJobs))
	for jobID := range s.OpenJobs {
		clone.OpenJobs[jobID] = struct{}{}
	}
	clone.SeenArtifacts = make(map[string]struct{}, len(s.SeenArtifacts))
	for key := range s.SeenArtifacts {
		clone.SeenArtifacts[key] = struct{}{}
	}
	clone.PendingAsks = clonePendingAsks(s.PendingAsks)
	return clone
}

func clonePendingAsks(asks []ChatPendingAsk) []ChatPendingAsk {
	if asks == nil {
		return nil
	}
	clone := make([]ChatPendingAsk, len(asks))
	for index, ask := range asks {
		clone[index] = ask
		clone[index].Questions = make([]ChatQuestion, len(ask.Questions))
		for questionIndex, question := range ask.Questions {
			clone[index].Questions[questionIndex] = question
			clone[index].Questions[questionIndex].Options =
				append([]string(nil), question.Options...)
		}
	}
	return clone
}

func cloneChatMessages(messages []ChatMessage) []ChatMessage {
	if messages == nil {
		return nil
	}
	clone := make([]ChatMessage, len(messages))
	for index, message := range messages {
		clone[index] = message
		clone[index].Tools = make([]ChatToolRun, len(message.Tools))
		for toolIndex, tool := range message.Tools {
			clone[index].Tools[toolIndex] = tool
			clone[index].Tools[toolIndex].Arguments = append(
				[]byte(nil),
				tool.Arguments...,
			)
			clone[index].Tools[toolIndex].Result = append([]byte(nil), tool.Result...)
		}
		clone[index].Artifacts = append([]ChatArtifact(nil), message.Artifacts...)
	}
	return clone
}
