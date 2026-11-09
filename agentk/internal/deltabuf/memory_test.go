package deltabuf

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryContract(t *testing.T) {
	runBufferContract(t, func(t *testing.T) Buffer { return NewMemory() })
}

func TestMemoryTTLStartsAtEnd(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	b := NewMemory()
	b.now = func() time.Time { return now }
	id := newExecutionID()
	if err := b.Begin(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := b.End(ctx, id); err != nil {
		t.Fatal(err)
	}
	now = now.Add(defaultTTL + time.Minute)
	if _, err := b.Open(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired buffer open: %v", err)
	}
}
