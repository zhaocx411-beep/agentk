package deltabuf

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	"agentk/internal/types"
)

const defaultTTL = 2 * time.Hour

type Memory struct {
	mu  sync.Mutex
	ttl time.Duration
	now func() time.Time
	m   map[types.ExecutionID]*memEntry
}

type memEntry struct {
	items   []Item
	notify  chan struct{}
	touched time.Time
	endedAt *time.Time
}

func NewMemory() *Memory {
	return &Memory{ttl: defaultTTL, now: time.Now, m: make(map[types.ExecutionID]*memEntry)}
}

var _ Buffer = (*Memory)(nil)

func (b *Memory) Begin(_ context.Context, executionID types.ExecutionID) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweep()
	b.m[executionID] = &memEntry{notify: make(chan struct{}), touched: b.now()}
	return nil
}

func (b *Memory) Append(_ context.Context, executionID types.ExecutionID, item Item) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.m[executionID]
	if !ok || e.endedAt != nil {
		return nil
	}
	e.items = append(e.items, item)
	e.touched = b.now()
	e.signal()
	return nil
}

func (b *Memory) End(_ context.Context, executionID types.ExecutionID) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.m[executionID]
	if !ok || e.endedAt != nil {
		return nil
	}
	e.items = append(e.items, Item{Kind: StreamEndKind, Data: json.RawMessage(`{}`)})
	now := b.now()
	e.touched = now
	e.endedAt = &now
	e.signal()
	return nil
}

func (b *Memory) Open(_ context.Context, executionID types.ExecutionID) (Stream, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweep()
	if _, ok := b.m[executionID]; !ok {
		return nil, ErrNotFound
	}
	return &memoryStream{buffer: b, executionID: executionID, done: make(chan struct{})}, nil
}

func (e *memEntry) signal() {
	close(e.notify)
	e.notify = make(chan struct{})
}

func (b *Memory) sweep() {
	now := b.now()
	for id, e := range b.m {
		base := e.touched
		if e.endedAt != nil {
			base = *e.endedAt
		}
		if now.Sub(base) >= b.ttl {
			delete(b.m, id)
		}
	}
}

type memoryStream struct {
	buffer      *Memory
	executionID types.ExecutionID
	offset      int
	done        chan struct{}
	closeOnce   sync.Once
}

func (s *memoryStream) Recv(ctx context.Context) (Item, error) {
	for {
		s.buffer.mu.Lock()
		e, ok := s.buffer.m[s.executionID]
		if !ok {
			s.buffer.mu.Unlock()
			return Item{}, io.EOF
		}
		if s.offset < len(e.items) {
			item := e.items[s.offset]
			s.offset++
			s.buffer.mu.Unlock()
			return item, nil
		}
		if e.endedAt != nil {
			s.buffer.mu.Unlock()
			return Item{}, io.EOF
		}
		notify := e.notify
		s.buffer.mu.Unlock()

		select {
		case <-ctx.Done():
			return Item{}, ctx.Err()
		case <-s.done:
			return Item{}, io.EOF
		case <-notify:
		}
	}
}

func (s *memoryStream) Close() error {
	s.closeOnce.Do(func() { close(s.done) })
	return nil
}
