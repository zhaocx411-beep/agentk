package deltabuf

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"git.corp.kuaishou.com/infra/infra-framework-go.git/kedis"

	"agentk/internal/types"
)

type Kedis struct {
	kd *kedis.Kedis
}

const (
	kedisKeyPrefix    = "agentk:delta:"
	kedisHeader       = `{"type":"begin"}`
	kedisPollInterval = 150 * time.Millisecond
)

func NewKedis(resourceName string) (*Kedis, error) {
	if resourceName == "" {
		return nil, fmt.Errorf("deltabuf: kedis resource name is required")
	}
	kd, err := kedis.NewKedisWithOptions(resourceName, kedis.Options{
		DialTimeout:        3 * time.Second,
		ReadTimeout:        2 * time.Second,
		IdleTimeout:        time.Minute,
		IdleCheckFrequency: time.Minute,
		MinIdleConns:       2,
		PoolSize:           10,
	})
	if err != nil {
		return nil, fmt.Errorf("deltabuf: init kedis resource %q: %w", resourceName, err)
	}
	return &Kedis{kd: kd}, nil
}

var _ Buffer = (*Kedis)(nil)

func (b *Kedis) key(executionID types.ExecutionID) string {
	return kedisKeyPrefix + string(executionID)
}

func (b *Kedis) Begin(_ context.Context, executionID types.ExecutionID) error {
	key := b.key(executionID)
	if err := b.kd.Del(key).Err(); err != nil {
		return fmt.Errorf("deltabuf: clear %s: %w", key, err)
	}
	if err := b.kd.RPush(key, kedisHeader).Err(); err != nil {
		return fmt.Errorf("deltabuf: begin %s: %w", key, err)
	}
	return b.expire(key)
}

func (b *Kedis) Append(_ context.Context, executionID types.ExecutionID, item Item) error {
	key := b.key(executionID)
	n, err := b.kd.LLen(key).Result()
	if err != nil {
		return fmt.Errorf("deltabuf: llen %s: %w", key, err)
	}
	if n == 0 {
		return nil
	}
	raw, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("deltabuf: marshal item: %w", err)
	}
	if err := b.kd.RPush(key, raw).Err(); err != nil {
		return fmt.Errorf("deltabuf: append %s: %w", key, err)
	}
	return b.expire(key)
}

func (b *Kedis) End(_ context.Context, executionID types.ExecutionID) error {
	key := b.key(executionID)
	last, err := b.kd.LRange(key, -1, -1).Result()
	if err != nil {
		return fmt.Errorf("deltabuf: inspect %s before end: %w", key, err)
	}
	if len(last) == 0 {
		return nil
	}
	if last[0] != kedisHeader {
		var existing Item
		if err := json.Unmarshal([]byte(last[0]), &existing); err != nil {
			return fmt.Errorf("deltabuf: decode tail for %s: %w", key, err)
		}
		if existing.Kind == StreamEndKind {
			return b.expire(key)
		}
	}
	item, err := json.Marshal(Item{Kind: StreamEndKind, Data: json.RawMessage(`{}`)})
	if err != nil {
		return err
	}
	if err := b.kd.RPush(key, item).Err(); err != nil {
		return fmt.Errorf("deltabuf: end %s: %w", key, err)
	}
	return b.expire(key)
}

func (b *Kedis) Open(_ context.Context, executionID types.ExecutionID) (Stream, error) {
	key := b.key(executionID)
	first, err := b.kd.LRange(key, 0, 0).Result()
	if err != nil {
		return nil, fmt.Errorf("deltabuf: open %s: %w", key, err)
	}
	if len(first) == 0 {
		return nil, ErrNotFound
	}
	if first[0] != kedisHeader {
		return nil, fmt.Errorf("deltabuf: invalid header for %s", key)
	}
	return &kedisStream{buffer: b, key: key, offset: 1, done: make(chan struct{})}, nil
}

func (b *Kedis) expire(key string) error {
	if err := b.kd.Expire(key, defaultTTL).Err(); err != nil {
		return fmt.Errorf("deltabuf: expire %s: %w", key, err)
	}
	return nil
}

type kedisStream struct {
	buffer    *Kedis
	key       string
	offset    int64
	done      chan struct{}
	closeOnce sync.Once
	ended     bool
}

func (s *kedisStream) Recv(ctx context.Context) (Item, error) {
	if s.ended {
		return Item{}, io.EOF
	}
	ticker := time.NewTicker(kedisPollInterval)
	defer ticker.Stop()
	for {
		raw, err := s.buffer.kd.LRange(s.key, s.offset, s.offset).Result()
		if err != nil {
			return Item{}, fmt.Errorf("deltabuf: read %s: %w", s.key, err)
		}
		if len(raw) > 0 {
			var item Item
			if err := json.Unmarshal([]byte(raw[0]), &item); err != nil {
				return Item{}, fmt.Errorf("deltabuf: decode item: %w", err)
			}
			s.offset++
			s.ended = item.Kind == StreamEndKind
			return item, nil
		}
		select {
		case <-ctx.Done():
			return Item{}, ctx.Err()
		case <-s.done:
			return Item{}, io.EOF
		case <-ticker.C:
		}
	}
}

func (s *kedisStream) Close() error {
	s.closeOnce.Do(func() { close(s.done) })
	return nil
}
