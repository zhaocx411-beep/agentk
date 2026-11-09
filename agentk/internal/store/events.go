package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"agentk/internal/event"
	"agentk/internal/types"
)

// 会话事件日志的存储层。写入纪律：
//
//   - 一批事件一个事务。一个 round 的全部产出同批落库，中间态永不可见——
//     fold 看到的要么是完整一轮，要么根本没这一轮。
//   - 幂等靠 (session_id, batch_id, batch_idx) 唯一键。Activity 重试携带同一个
//     batch id 重放，读回首次提交的结果；模型输出的不确定性因此不会写出两份
//     互相矛盾的历史。
//   - 自增主键只负责内部排序；EventID 是对外稳定身份。

// AppendEvents 原子追加一批事件并分配 EventID。该 batch 已提交过时不重复写入，
// 直接读回首次提交的 EventID 和时间。
func (s *Store) AppendEvents(
	ctx context.Context,
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
	rows := make([]eventRow, len(drafts))
	for i, d := range drafts {
		if d.Payload == nil {
			return nil, fmt.Errorf("append events: draft[%d]: nil payload", i)
		}
		payload, err := event.MarshalPayload(d.Payload)
		if err != nil {
			return nil, fmt.Errorf("append events: draft[%d]: %w", i, err)
		}
		rows[i] = eventRow{roundID: d.RoundID, kind: d.Payload.Kind(), payload: payload}
	}

	var out []event.Event
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		// 会话行锁使同一 batch 的并发 Activity 重试只有一个 writer；唯一键仍是
		// 数据库侧的最终幂等约束。
		var id int64
		err := tx.QueryRowContext(ctx,
			`SELECT id FROM ak_sessions WHERE id = ? FOR UPDATE`, sessionID).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("session %d: %w", sessionID, ErrNotFound)
		}
		if err != nil {
			return err
		}

		committed, err := loadBatchTx(ctx, tx, sessionID, batchID)
		if err != nil {
			return err
		}
		if len(committed) > 0 {
			out = committed
			return nil
		}

		placeholders := make([]string, 0, len(rows))
		args := make([]any, 0, len(rows)*8)
		for i, r := range rows {
			placeholders = append(placeholders, `(?, ?, ?, ?, ?, ?, ?, ?)`)
			args = append(args, string(event.NewEventID()), sessionID, string(executionID), string(r.roundID),
				string(batchID), i, string(r.kind), []byte(r.payload))
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO ak_events (event_id, session_id, execution_id, round_id, batch_id, batch_idx, kind, payload) VALUES `+
				strings.Join(placeholders, ", "), args...); err != nil {
			return err
		}
		// 回读而不是就地拼装：created_at 是 DB 时钟写的，重试时返回的也该是
		// 首次提交那一刻。
		out, err = loadBatchTx(ctx, tx, sessionID, batchID)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("append events: %w", err)
	}
	return out, nil
}

// LoadSessionEvents 按数据库内部 id 正序返回 Session 的全部已识别事件。未知 kind
// 被忽略；已识别 kind 的损坏 payload 返回错误。
func (s *Store) LoadSessionEvents(
	ctx context.Context,
	sessionID int64,
) ([]event.Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, event_id, execution_id, round_id, kind, payload, created_at
		 FROM ak_events WHERE session_id = ? ORDER BY id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load session events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	events, err := scanEvents(rows)
	if err != nil {
		return nil, fmt.Errorf("load session events: %w", err)
	}
	// 空结果要区分“会话存在但还没有事件”与“会话不存在”。
	if len(events) == 0 {
		if _, err := s.GetSession(ctx, sessionID); err != nil {
			return nil, fmt.Errorf("load session events: %w", err)
		}
	}
	return events, nil
}

type eventRow struct {
	roundID event.RoundID
	kind    event.Kind
	payload []byte
}

func loadBatchTx(ctx context.Context, tx *sql.Tx, sessionID int64, batchID event.BatchID) ([]event.Event, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, event_id, execution_id, round_id, kind, payload, created_at
		 FROM ak_events WHERE session_id = ? AND batch_id = ? ORDER BY batch_idx`,
		sessionID, string(batchID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanEvents(rows)
}

func scanEvents(rows *sql.Rows) ([]event.Event, error) {
	var out []event.Event
	for rows.Next() {
		var (
			internalID  int64
			eventID     string
			executionID string
			roundID     string
			kind        string
			payload     []byte
			ev          event.Event
		)
		if err := rows.Scan(&internalID, &eventID, &executionID, &roundID, &kind, &payload, &ev.CreatedAt); err != nil {
			return nil, err
		}
		p, err := event.UnmarshalPayload(event.Kind(kind), payload)
		if err != nil {
			if errors.Is(err, event.ErrUnknownKind) {
				continue
			}
			return nil, fmt.Errorf("event id %d (%s): %w", internalID, eventID, err)
		}
		ev.ID = event.EventID(eventID)
		ev.ExecutionID = types.ExecutionID(executionID)
		ev.RoundID, ev.Payload = event.RoundID(roundID), p
		out = append(out, ev)
	}
	return out, rows.Err()
}
