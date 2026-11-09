package comicdrama

import (
	"testing"
	"time"

	"studio/platform/core"
)

func TestRestoreAgentSessions(t *testing.T) {
	store := NewMemoryDramaStore()
	createdAt := time.Date(2026, 8, 13, 8, 24, 34, 0, time.UTC)

	restored := RestoreAgentSessions(store, []core.SessionSummary{
		{ID: "ses_abc123", Title: "漫剧: 中毒3", CreatedAt: createdAt},
		{ID: "ses_chat", Title: "普通聊天", CreatedAt: createdAt},
	})
	if restored != 1 {
		t.Fatalf("restored = %d, want 1", restored)
	}
	drama, ok := store.Get("drm_abc123")
	if !ok {
		t.Fatal("restored drama not found")
	}
	if drama.SessionID != "ses_abc123" || drama.Title != "中毒3" {
		t.Fatalf("restored drama = %+v", drama)
	}
	if drama.State.Converged {
		t.Fatal("restored drama must refresh its ledger before converging")
	}
}
