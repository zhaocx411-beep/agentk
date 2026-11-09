package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agentk/internal/authz"
	"agentk/internal/deltabuf"
	"agentk/internal/event"
	"agentk/internal/service"
	"agentk/internal/store/memory"
	"agentk/internal/types"
)

type sseFrame struct {
	id   string
	kind string
	data string
}

type loadCountingStore struct {
	service.Store
	loads int
}

func (s *loadCountingStore) LoadSessionEvents(
	ctx context.Context, sessionID int64,
) ([]event.Event, error) {
	s.loads++
	return s.Store.LoadSessionEvents(ctx, sessionID)
}

func readFrames(t *testing.T, body io.Reader, want int) []sseFrame {
	t.Helper()
	scanner := bufio.NewScanner(body)
	var frames []sseFrame
	var cur sseFrame
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			if cur.kind != "" {
				frames = append(frames, cur)
				if len(frames) == want {
					return frames
				}
			}
			cur = sseFrame{}
		case strings.HasPrefix(line, ":"):
		case strings.HasPrefix(line, "id: "):
			cur.id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			cur.kind = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			cur.data = strings.TrimPrefix(line, "data: ")
		}
	}
	t.Fatalf("stream ended after %d frames, want %d: %+v", len(frames), want, frames)
	return nil
}

func openStream(t *testing.T, url string) *http.Response {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func deltaItem(t *testing.T, gen string, idx int, text string) deltabuf.Item {
	t.Helper()
	raw, err := json.Marshal(deltabuf.Frame{
		RoundID: "rnd_1", Gen: gen, Idx: idx, Block: 0, Kind: "text", Text: text,
	})
	if err != nil {
		t.Fatal(err)
	}
	return deltabuf.Item{Kind: deltabuf.DeltaKind, Data: raw}
}

func eventItem(t *testing.T, executionID types.ExecutionID, eventID event.EventID, payload event.Payload) deltabuf.Item {
	t.Helper()
	ev := event.Event{
		ID: eventID, ExecutionID: executionID, RoundID: "rnd_1", Payload: payload,
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	return deltabuf.Item{Kind: string(payload.Kind()), EventID: eventID, Data: raw}
}

func (h *testHarness) beginExecution(t *testing.T, sess sessionDTO) types.ExecutionID {
	t.Helper()
	ctx := context.Background()
	stored, err := h.store.GetSessionByPublicID(ctx, "org_dev", sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	executionID := types.NewExecutionID()
	if _, err := h.store.AppendEvents(ctx, stored.ID, executionID,
		event.BatchID(executionID+"/started"), []event.Draft{{
			Payload: event.ExecutionStarted{
				ExecutionKind: types.ExecutionKindAgentLoop, ProfileVersion: "v1",
			},
		}}); err != nil {
		t.Fatal(err)
	}
	if err := h.deltas.Begin(ctx, executionID); err != nil {
		t.Fatal(err)
	}
	return executionID
}

func TestStreamReturns204WithoutActiveExecution(t *testing.T) {
	h := newHarness(t)
	sess := h.newSession(t, "idle")
	resp := openStream(t, h.url+"/v1/sessions/"+sess.ID+"/stream")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct == "text/event-stream" {
		t.Fatalf("inactive response unexpectedly has SSE content type")
	}
}

func TestStreamResolvesActiveExecutionFromEventLog(t *testing.T) {
	base := memory.New()
	sess, err := base.CreateSession(context.Background(), "org_dev", "stream",
		[]byte(`{"version":"v1"}`))
	if err != nil {
		t.Fatal(err)
	}
	executionID := types.NewExecutionID()
	if _, err := base.AppendEvents(context.Background(), sess.ID, executionID,
		event.BatchID(executionID+"/started"), []event.Draft{{Payload: event.ExecutionStarted{
			ExecutionKind: types.ExecutionKindAgentLoop, ProfileVersion: "v1",
		}}}); err != nil {
		t.Fatal(err)
	}
	store := &loadCountingStore{Store: base}
	deltas := deltabuf.NewMemory()
	if err := deltas.Begin(context.Background(), executionID); err != nil {
		t.Fatal(err)
	}
	agentLoop := &fakeAgentLoop{store: base}
	appService := service.New(service.Config{
		Store: store, AgentLoop: agentLoop, Executions: agentLoop, ProfileRuntime: testRuntime(t),
	})
	srv := httptest.NewServer(authz.WithDevPrincipal(NewServer(ServerConfig{
		Service: appService,
		Deltas:  deltas,
	}).Handler()))
	t.Cleanup(srv.Close)

	resp := openStream(t, srv.URL+"/v1/sessions/"+sess.PublicID+"/stream")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	if store.loads != 1 {
		t.Fatalf("stream loaded event log %d times, want 1", store.loads)
	}
}

func TestStreamReplaysAndTailsExecutionBuffer(t *testing.T) {
	h := newHarness(t)
	sess := h.newSession(t, "active")
	executionID := h.beginExecution(t, sess)
	ctx := context.Background()
	for i, text := range []string{"你", "好"} {
		if err := h.deltas.Append(ctx, executionID, deltaItem(t, "g1", i, text)); err != nil {
			t.Fatal(err)
		}
	}

	resp := openStream(t, h.url+"/v1/sessions/"+sess.ID+"/stream")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status=%d content-type=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	replayed := readFrames(t, resp.Body, 2)
	for i, f := range replayed {
		if f.kind != deltabuf.DeltaKind || f.id != "" {
			t.Fatalf("delta %d = %+v", i, f)
		}
		var got deltabuf.Frame
		if err := json.Unmarshal([]byte(f.data), &got); err != nil {
			t.Fatal(err)
		}
		if got.Idx != i {
			t.Fatalf("delta idx = %d, want %d", got.Idx, i)
		}
	}

	eventID := event.NewEventID()
	if err := h.deltas.Append(ctx, executionID, eventItem(t, executionID, eventID,
		event.AssistantText{Block: 0, Text: "你好"})); err != nil {
		t.Fatal(err)
	}
	live := readFrames(t, resp.Body, 1)[0]
	if live.kind != string(event.KindAssistantText) || live.id != string(eventID) {
		t.Fatalf("live = %+v", live)
	}
}

func TestStreamEndsWhenExecutionBufferEnds(t *testing.T) {
	h := newHarness(t)
	sess := h.newSession(t, "ending")
	executionID := h.beginExecution(t, sess)
	ctx := context.Background()
	resp := openStream(t, h.url+"/v1/sessions/"+sess.ID+"/stream")

	if err := h.deltas.End(ctx, executionID); err != nil {
		t.Fatal(err)
	}
	last := readFrames(t, resp.Body, 1)[0]
	if last.kind != deltabuf.StreamEndKind || last.id != "" {
		t.Fatalf("last frame = %+v", last)
	}
}

func TestStreamReconnectReplaysCurrentExecution(t *testing.T) {
	h := newHarness(t)
	sess := h.newSession(t, "reconnect")
	executionID := h.beginExecution(t, sess)
	ctx := context.Background()
	if err := h.deltas.Append(ctx, executionID, deltaItem(t, "g1", 0, "半截")); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		resp := openStream(t, h.url+"/v1/sessions/"+sess.ID+"/stream")
		frame := readFrames(t, resp.Body, 1)[0]
		if frame.kind != deltabuf.DeltaKind || !strings.Contains(frame.data, `"idx":0`) {
			t.Fatalf("replay = %+v", frame)
		}
		_ = resp.Body.Close()
	}
}
