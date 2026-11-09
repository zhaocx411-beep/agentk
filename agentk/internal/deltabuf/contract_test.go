package deltabuf

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"agentk/internal/types"
)

func runBufferContract(t *testing.T, factory func(t *testing.T) Buffer) {
	t.Helper()
	ctx := context.Background()
	item := func(kind, text string) Item {
		return Item{Kind: kind, Data: json.RawMessage(`"` + text + `"`)}
	}
	mustRecv := func(t *testing.T, stream Stream) Item {
		t.Helper()
		recvCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		got, err := stream.Recv(recvCtx)
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		return got
	}

	t.Run("Missing", func(t *testing.T) {
		_, err := factory(t).Open(ctx, newExecutionID())
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("open missing: %v", err)
		}
	})

	t.Run("ReplayThenLive", func(t *testing.T) {
		b := factory(t)
		id := newExecutionID()
		if err := b.Begin(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := b.Append(ctx, id, item(DeltaKind, "a")); err != nil {
			t.Fatal(err)
		}
		stream, err := b.Open(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = stream.Close() }()
		if got := mustRecv(t, stream); got.Kind != DeltaKind {
			t.Fatalf("replay = %+v", got)
		}
		if err := b.Append(ctx, id, item("assistant.text", "ab")); err != nil {
			t.Fatal(err)
		}
		if got := mustRecv(t, stream); got.Kind != "assistant.text" {
			t.Fatalf("live = %+v", got)
		}
	})

	t.Run("EndIsReplayedAndRetained", func(t *testing.T) {
		b := factory(t)
		id := newExecutionID()
		if err := b.Begin(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := b.Append(ctx, id, item(DeltaKind, "tail")); err != nil {
			t.Fatal(err)
		}
		if err := b.End(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := b.End(ctx, id); err != nil {
			t.Fatal(err)
		}
		stream, err := b.Open(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = stream.Close() }()
		if got := mustRecv(t, stream); got.Kind != DeltaKind {
			t.Fatalf("tail = %+v", got)
		}
		if got := mustRecv(t, stream); got.Kind != StreamEndKind {
			t.Fatalf("end = %+v", got)
		}
		if _, err := stream.Recv(ctx); !errors.Is(err, io.EOF) {
			t.Fatalf("after end: %v", err)
		}
	})

	t.Run("EndMissingIsNoOp", func(t *testing.T) {
		if err := factory(t).End(ctx, newExecutionID()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ExecutionsAreIndependent", func(t *testing.T) {
		b := factory(t)
		first, second := newExecutionID(), newExecutionID()
		for _, id := range []types.ExecutionID{first, second} {
			if err := b.Begin(ctx, id); err != nil {
				t.Fatal(err)
			}
		}
		if err := b.Append(ctx, first, item(DeltaKind, "first")); err != nil {
			t.Fatal(err)
		}
		secondStream, err := b.Open(ctx, second)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = secondStream.Close() }()
		recvCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
		defer cancel()
		if _, err := secondStream.Recv(recvCtx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("second execution received first data: %v", err)
		}
	})
}

func newExecutionID() types.ExecutionID {
	return types.NewExecutionID()
}
