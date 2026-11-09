package store

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"agentk/internal/event"
	"agentk/internal/store/storetest"
	"agentk/internal/types"
)

func TestStoreContract(t *testing.T) {
	storetest.RunStoreContract(t, func(t *testing.T) storetest.Store {
		return New(testDB(t))
	})
}

func TestAppendEventsConcurrent(t *testing.T) {
	db := testDB(t)
	db.SetMaxOpenConns(16)
	s := New(db)
	sess, err := s.CreateSession(context.Background(), "org_test", "t", testProfile())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	const writers = 8
	executionID := types.NewExecutionID()
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.AppendEvents(context.Background(), sess.ID, executionID,
				event.BatchID(fmt.Sprintf("batch-%d", i)),
				[]event.Draft{
					{Payload: event.UserInput{Text: fmt.Sprintf("msg-%d", i)}},
					{Payload: event.UserInput{Text: fmt.Sprintf("msg-%d-b", i)}},
				})
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent append: %v", err)
	}

	events, err := s.LoadSessionEvents(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(events) != writers*2 {
		t.Fatalf("loaded %d events, want %d", len(events), writers*2)
	}
	seen := make(map[event.EventID]bool, len(events))
	for _, ev := range events {
		if ev.ID == "" || seen[ev.ID] || ev.ExecutionID != executionID {
			t.Fatalf("invalid concurrent event identity: %+v", ev)
		}
		seen[ev.ID] = true
	}
}

// TestAppendEventsBatchUniqueKey 直接验唯一键：行锁之外还有这道防线，绕过
// AppendEvents 的读回分支也插不进重复批。
func TestAppendEventsBatchUniqueKey(t *testing.T) {
	db := testDB(t)
	s := New(db)
	sess, err := s.CreateSession(context.Background(), "org_test", "t", testProfile())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	executionID := types.NewExecutionID()
	if _, err := s.AppendEvents(context.Background(), sess.ID, executionID, "round-1", []event.Draft{
		{RoundID: "rnd-1", Payload: event.AssistantText{Text: "hi"}},
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO ak_events (event_id, session_id, execution_id, round_id, batch_id, batch_idx, kind, payload)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		event.NewEventID(), sess.ID, executionID, "rnd-1", "round-1", 0,
		string(event.KindAssistantText), `{"block":0,"text":"dup"}`)
	if err == nil {
		t.Fatal("expected duplicate (session_id, batch_id, batch_idx) to be rejected")
	}
}

func TestLoadSessionEventsSkipsUnknownKindAndRejectsMalformedKnownPayload(t *testing.T) {
	db := testDB(t)
	s := New(db)
	ctx := context.Background()
	sess, err := s.CreateSession(ctx, "org_test", "t", testProfile())
	if err != nil {
		t.Fatal(err)
	}
	executionID := types.NewExecutionID()
	if _, err := s.AppendEvents(ctx, sess.ID, executionID, "known-1", []event.Draft{{
		Payload: event.UserInput{Text: "first"},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO ak_events
		 (event_id, session_id, execution_id, round_id, batch_id, batch_idx, kind, payload)
		 VALUES (?, ?, ?, '', 'unknown', 0, 'future.kind', JSON_OBJECT('value', 1))`,
		event.NewEventID(), sess.ID, executionID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendEvents(ctx, sess.ID, executionID, "known-2", []event.Draft{{
		Payload: event.AssistantText{Text: "second"},
	}}); err != nil {
		t.Fatal(err)
	}

	events, err := s.LoadSessionEvents(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Payload.Kind() != event.KindUserInput ||
		events[1].Payload.Kind() != event.KindAssistantText {
		t.Fatalf("known events = %+v", events)
	}

	if _, err := db.ExecContext(ctx,
		`INSERT INTO ak_events
		 (event_id, session_id, execution_id, round_id, batch_id, batch_idx, kind, payload)
		 VALUES (?, ?, ?, '', 'malformed', 0, 'user.input', JSON_ARRAY())`,
		event.NewEventID(), sess.ID, executionID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadSessionEvents(ctx, sess.ID); err == nil {
		t.Fatal("malformed payload for known kind was accepted")
	}
}
