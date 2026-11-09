package chat

import (
	"testing"
	"time"
)

func TestMemoryChatStoreCopiesAndOrdersChats(t *testing.T) {
	store := NewMemoryChatStore()
	now := time.Now().UTC()
	for _, chat := range []Chat{
		{
			ID: "cht_1", AgentID: "creative-studio", Title: "第一条",
			State: NewChatState(), CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "cht_2", AgentID: "web-research", Title: "第二条",
			State: NewChatState(), CreatedAt: now, UpdatedAt: now,
		},
	} {
		if err := store.Create(chat); err != nil {
			t.Fatalf("create chat: %v", err)
		}
	}
	if got := store.List(); len(got) != 2 || got[0].ID != "cht_2" {
		t.Fatalf("list = %+v", got)
	}
	chat, ok := store.Get("cht_1")
	if !ok {
		t.Fatal("chat is missing")
	}
	chat.State.Messages = append(chat.State.Messages, ChatMessage{Text: "mutated"})
	again, _ := store.Get("cht_1")
	if len(again.State.Messages) != 0 {
		t.Fatal("store leaked mutable chat state")
	}
}

func TestMemoryChatStoreRejectsOlderState(t *testing.T) {
	store := NewMemoryChatStore()
	now := time.Now().UTC()
	state := NewChatState()
	state.LastEventID = "evt_00000005"
	if err := store.Create(Chat{
		ID: "cht_1", State: state, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	older := NewChatState()
	older.LastEventID = "evt_00000004"
	updated, err := store.UpdateState("cht_1", older, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if updated.State.LastEventID != "evt_00000005" {
		t.Fatalf("last event id = %q, want evt_00000005", updated.State.LastEventID)
	}
}
