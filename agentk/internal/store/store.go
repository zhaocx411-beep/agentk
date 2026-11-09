// Package store 集中管理 MySQL 访问：会话 + 事件日志 + 身份表，事务边界在此。
// 纪律：保守 MySQL 8.0 公共子集；事件日志只插不改；一致性靠应用层事务（无外键约束）。
package store

import (
	"agentk/internal/types"
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrNotFound 查询未命中；哨兵定义在 types，与内存实现共用（API 层译为 404）。
var ErrNotFound = types.ErrNotFound

// DB 是 Store 对底层数据库的全部依赖（*sql.DB 天然满足）。
// 收窄成接口是为 kdb 模式：internal/kdbstore 用公司 go-kdb datasource
// 实现同一接口，store 的 SQL 与事务逻辑对两种接入方式零感知。
type DB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

type Store struct {
	db DB
}

func New(db DB) *Store {
	return &Store{db: db}
}

func newPublicID(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand 失败说明环境已不可用
	}
	return prefix + hex.EncodeToString(b[:])
}

// withTx 执行事务；fn 返回错误则回滚。
func (s *Store) withTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

/* ---------- sessions ---------- */

func (s *Store) CreateSession(ctx context.Context, tenantID, title string, profile []byte) (types.Session, error) {
	publicID := newPublicID("ses_")
	trimmedProfile := bytes.TrimSpace(profile)
	if len(trimmedProfile) == 0 || trimmedProfile[0] != '{' || !json.Valid(trimmedProfile) {
		return types.Session{}, fmt.Errorf("create session: profile must be a non-empty JSON object")
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO ak_sessions (public_id, tenant_id, title, profile) VALUES (?, ?, ?, ?)`,
		publicID, tenantID, title, trimmedProfile)
	if err != nil {
		return types.Session{}, fmt.Errorf("create session: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return types.Session{}, err
	}
	return s.GetSession(ctx, id)
}

func (s *Store) GetSession(ctx context.Context, id int64) (types.Session, error) {
	sess, err := s.querySession(ctx, `id = ?`, id)
	if err != nil {
		return types.Session{}, fmt.Errorf("get session %d: %w", id, err)
	}
	return sess, nil
}

// GetSessionByPublicID 面向用户的解析入口：public_id 来自外部输入，必须带租户约束。
// "不存在"与"存在但不属于该租户"同一律 ErrNotFound，不暴露资源存在性。
func (s *Store) GetSessionByPublicID(ctx context.Context, tenantID, publicID string) (types.Session, error) {
	sess, err := s.querySession(ctx, `public_id = ? AND tenant_id = ?`, publicID, tenantID)
	if err != nil {
		return types.Session{}, fmt.Errorf("get session %q: %w", publicID, err)
	}
	return sess, nil
}

func (s *Store) querySession(ctx context.Context, where string, args ...any) (types.Session, error) {
	var sess types.Session
	err := s.db.QueryRowContext(ctx,
		`SELECT id, public_id, tenant_id, title, profile, created_at FROM ak_sessions WHERE `+where, args...).
		Scan(&sess.ID, &sess.PublicID, &sess.TenantID, &sess.Title, &sess.Profile, &sess.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return types.Session{}, ErrNotFound
	}
	if err != nil {
		return types.Session{}, err
	}
	return sess, nil
}

// ListSessions 返回租户内的会话，按创建序倒排（新在前）。
// beforeID > 0 时只返回 id < beforeID 的行（keyset 游标翻页，beforeID 来自上一页
// 末尾会话的带租户解析结果）；limit 必须为正。
// 索引走 idx_sessions_tenant（InnoDB 二级索引隐含追加主键，等效 (tenant_id, id)）。
func (s *Store) ListSessions(ctx context.Context, tenantID string, beforeID int64, limit int) ([]types.Session, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("list sessions: limit must be positive, got %d", limit)
	}
	where := `tenant_id = ?`
	args := []any{tenantID}
	if beforeID > 0 {
		where += ` AND id < ?`
		args = append(args, beforeID)
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, public_id, tenant_id, title, profile, created_at
		 FROM ak_sessions WHERE `+where+` ORDER BY id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var sessions []types.Session
	for rows.Next() {
		var sess types.Session
		if err := rows.Scan(&sess.ID, &sess.PublicID, &sess.TenantID, &sess.Title, &sess.Profile, &sess.CreatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}
