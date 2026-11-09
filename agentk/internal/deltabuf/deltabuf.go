// Package deltabuf 保存 Execution 期间可回放的实时帧。
// 它是尽力而为的实时旁路，不是事实源：delta 用于预览，已提交事件用于最终校准。
// 每个 Stream 从 offset 0 重放该 Execution 的缓冲，然后等待实时追加。
package deltabuf

import (
	"context"
	"encoding/json"
	"errors"

	"agentk/internal/event"
	"agentk/internal/types"
)

const (
	DeltaKind     = "delta"
	StreamEndKind = "stream.end"
)

var ErrNotFound = errors.New("delta buffer not found")

// Frame 是 delta item 的 payload。Gen 区分同一 round 的不同 Activity 尝试，Idx 在
// Gen 内连续递增，Block 与最终 assistant 事件中的 BlockIndex 对齐。
type Frame struct {
	RoundID event.RoundID `json:"roundId"`
	Gen     string        `json:"gen"`
	Idx     int           `json:"idx"`
	Block   int           `json:"block"`
	Kind    string        `json:"kind"`
	Text    string        `json:"text"`
}

// Item 可编码为 SSE 帧。只有已持久化事件携带 EventID。
type Item struct {
	Kind    string          `json:"kind"`
	EventID event.EventID   `json:"eventId,omitempty"`
	Data    json.RawMessage `json:"data"`
}

type Stream interface {
	Recv(ctx context.Context) (Item, error)
	Close() error
}

// Buffer 是 Execution 级 append-only 缓冲。
type Buffer interface {
	Begin(ctx context.Context, executionID types.ExecutionID) error
	Append(ctx context.Context, executionID types.ExecutionID, item Item) error
	Open(ctx context.Context, executionID types.ExecutionID) (Stream, error)
	End(ctx context.Context, executionID types.ExecutionID) error
}
