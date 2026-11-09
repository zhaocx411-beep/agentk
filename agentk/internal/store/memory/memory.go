// Package memory 是会话与事件日志的内存实现：零依赖启动（AGENTK_STORE=mem）
// 与单元测试用。
//
// 它刻意保持最小——语义由 internal/store/storetest 的共用契约测试与 MySQL 实现
// 对齐，只有真库才能验的东西（行锁下的并发分配、唯一键冲突、JSON 列往返）留在
// store 侧的 DSN 门控测试里。
package memory

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"agentk/internal/event"
	"agentk/internal/types"
)

// ErrNotFound 与 store 共用同一个哨兵（API 层译为 404）。
var ErrNotFound = types.ErrNotFound

type Store struct {
	mu       sync.Mutex
	nextID   int64
	sessions map[int64]*session
	byPublic map[string]int64
	// now 可替换，供测试钉死时间戳
	now func() time.Time
}

type session struct {
	meta   types.Session
	events []event.Event
	// batches 记录已提交的批（batch id → 该批事件的下标），承担 Activity
	// 重试幂等——镜像 ak_events 的 (session_id, batch_id, batch_idx) 唯一键。
	batches map[event.BatchID][]int
}

func New() *Store {
	return &Store{
		sessions: make(map[int64]*session),
		byPublic: make(map[string]int64),
		now:      time.Now,
	}
}

/* ---------- sessions ---------- */

func (s *Store) CreateSession(_ context.Context, tenantID, title string, profile []byte) (types.Session, error) {
	trimmed := bytes.TrimSpace(profile)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return types.Session{}, fmt.Errorf("create session: profile must be a non-empty JSON object")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	meta := types.Session{
		ID:        s.nextID,
		PublicID:  newPublicID("ses_"),
		TenantID:  tenantID,
		Title:     title,
		Profile:   append([]byte(nil), trimmed...),
		CreatedAt: s.now(),
	}
	s.sessions[meta.ID] = &session{meta: meta, batches: make(map[event.BatchID][]int)}
	s.byPublic[meta.PublicID] = meta.ID
	return meta, nil
}

func (s *Store) GetSession(_ context.Context, id int64) (types.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return types.Session{}, fmt.Errorf("get session %d: %w", id, ErrNotFound)
	}
	return sess.meta, nil
}

// GetSessionByPublicID 带租户约束："不存在"与"存在但不属于该租户"同一律
// ErrNotFound，不暴露资源存在性（语义与 internal/store 一致）。
func (s *Store) GetSessionByPublicID(_ context.Context, tenantID, publicID string) (types.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byPublic[publicID]
	if !ok {
		return types.Session{}, fmt.Errorf("get session %q: %w", publicID, ErrNotFound)
	}
	sess := s.sessions[id]
	if sess.meta.TenantID != tenantID {
		return types.Session{}, fmt.Errorf("get session %q: %w", publicID, ErrNotFound)
	}
	return sess.meta, nil
}

// ListSessions 返回租户内的会话，创建序倒排（新在前）；beforeID > 0 时只返回
// id < beforeID 的行（keyset 翻页，语义与 internal/store 一致）。
func (s *Store) ListSessions(_ context.Context, tenantID string, beforeID int64, limit int) ([]types.Session, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("list sessions: limit must be positive, got %d", limit)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []types.Session
	for id := s.nextID; id >= 1 && len(out) < limit; id-- {
		sess, ok := s.sessions[id]
		if !ok || sess.meta.TenantID != tenantID {
			continue
		}
		if beforeID > 0 && id >= beforeID {
			continue
		}
		out = append(out, sess.meta)
	}
	return out, nil
}

/* ---------- 事件日志 ---------- */

// AppendEvents 追加一批事件并分配 EventID；该 batch 已提交过时读回，不重复写入。
func (s *Store) AppendEvents(
	_ context.Context,
	sessionID int64,
	executionID types.ExecutionID,
	batchID event.BatchID,
	drafts []event.Draft,
) ([]event.Event, error) {
	if executionID == "" {
		return nil, fmt.Errorf("append events: execution id is required")
	}
	if batchID == "" {
		return nil, fmt.Errorf("append events: batch id is required")
	}
	if len(drafts) == 0 {
		return nil, fmt.Errorf("append events: batch is empty")
	}
	for i, d := range drafts {
		if d.Payload == nil {
			return nil, fmt.Errorf("append events: draft[%d]: nil payload", i)
		}
		// 走一遍编解码：内存实现也必须拒绝 MySQL 的 JSON 列会拒绝的载荷，
		// 否则两个实现的"能写进去什么"就分叉了。
		raw, err := event.MarshalPayload(d.Payload)
		if err != nil {
			return nil, fmt.Errorf("append events: draft[%d]: %w", i, err)
		}
		if _, err := event.UnmarshalPayload(d.Payload.Kind(), raw); err != nil {
			return nil, fmt.Errorf("append events: draft[%d]: %w", i, err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("append events: session %d: %w", sessionID, ErrNotFound)
	}
	if idx, ok := sess.batches[batchID]; ok {
		return sess.pick(idx), nil
	}
	at := s.now()
	idx := make([]int, 0, len(drafts))
	for _, d := range drafts {
		idx = append(idx, len(sess.events))
		sess.events = append(sess.events, event.Event{
			ID:          event.NewEventID(),
			ExecutionID: executionID,
			RoundID:     d.RoundID,
			Payload:     d.Payload,
			CreatedAt:   at,
		})
	}
	sess.batches[batchID] = idx
	return sess.pick(idx), nil
}

// LoadSessionEvents 按追加顺序返回 Session 的全部事件。
func (s *Store) LoadSessionEvents(
	_ context.Context,
	sessionID int64,
) ([]event.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("load session events: session %d: %w", sessionID, ErrNotFound)
	}
	out := append([]event.Event(nil), sess.events...)
	return out, nil
}

func (s *session) pick(idx []int) []event.Event {
	out := make([]event.Event, 0, len(idx))
	for _, i := range idx {
		out = append(out, s.events[i])
	}
	return out
}

func newPublicID(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand 失败说明环境已不可用
	}
	return prefix + hex.EncodeToString(b[:])
}
