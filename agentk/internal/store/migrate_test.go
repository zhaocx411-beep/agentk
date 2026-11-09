package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
)

func TestExpectedSchemaVersion(t *testing.T) {
	// 从内嵌 FS 推导；加了新迁移脚本这里的期望值要跟着变
	if got := ExpectedSchemaVersion(); got != 1 {
		t.Fatalf("ExpectedSchemaVersion() = %d, want 1", got)
	}
}

// schemaTables 是 000001 应当建出的全部业务表。写死一份而不是从库里读，是为了
// 让"迁移建了什么"和"代码以为有什么"两处必须对上——重建环境时的 schema 漂移只有
// 这样才拦得住。
var schemaTables = []string{
	"ak_api_keys", "ak_events", "ak_identities", "ak_memberships", "ak_orgs",
	"ak_service_principals", "ak_sessions", "ak_users",
}

// TestMigrateRebuildsSchemaFromEmpty 走一遍删库重建：down 拆到空，up 再建回来，
// 建出的表必须与代码期望的一字不差。
func TestMigrateRebuildsSchemaFromEmpty(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	if err := migrateDown(db); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if left := businessTables(ctx, t, db); len(left) != 0 {
		t.Fatalf("down left tables behind: %v", left)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate up on empty schema: %v", err)
	}
	if got := businessTables(ctx, t, db); !slices.Equal(got, schemaTables) {
		t.Fatalf("tables after rebuild = %v, want %v", got, schemaTables)
	}
	for _, column := range []string{"event_id", "execution_id"} {
		if nullable := columnNullable(ctx, t, db, "ak_events", column); nullable != "NO" {
			t.Fatalf("ak_events.%s nullable = %s, want NO", column, nullable)
		}
	}
	for _, column := range []struct {
		table string
		name  string
	}{{"ak_sessions", "last_event_seq"}, {"ak_events", "seq"}} {
		if columnExists(ctx, t, db, column.table, column.name) {
			t.Fatalf("%s.%s still exists", column.table, column.name)
		}
	}
	for name, want := range map[string]string{
		"uniq_events_event_id":    "event_id",
		"idx_events_session_id":   "session_id,id",
		"idx_events_execution_id": "execution_id,id",
		"uniq_events_batch":       "session_id,batch_id,batch_idx",
	} {
		if got := indexColumns(ctx, t, db, "ak_events", name); got != want {
			t.Fatalf("index %s columns = %q, want %q", name, got, want)
		}
	}
	if err := VerifySchemaVersion(ctx, db); err != nil {
		t.Fatalf("rebuilt schema: %v", err)
	}
}

func columnExists(ctx context.Context, t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
		table, column).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count == 1
}

func columnNullable(ctx context.Context, t *testing.T, db *sql.DB, table, column string) string {
	t.Helper()
	var nullable string
	if err := db.QueryRowContext(ctx, `SELECT is_nullable FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
		table, column).Scan(&nullable); err != nil {
		t.Fatal(err)
	}
	return nullable
}

func indexColumns(ctx context.Context, t *testing.T, db *sql.DB, table, index string) string {
	t.Helper()
	var columns string
	if err := db.QueryRowContext(ctx, `SELECT GROUP_CONCAT(column_name ORDER BY seq_in_index)
		FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?`,
		table, index).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	return columns
}

// migrateDown 把 schema 拆回空。只有测试需要它：线上从不回滚（数据可弃时直接
// 重建库），但 down 脚本必须有人跑过，否则它就是一段没验证过的死代码。
func migrateDown(db *sql.DB) error {
	return withMigrator(db, func(m *migrate.Migrate) error {
		if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return err
		}
		return nil
	})
}

// businessTables 列出当前库里的 ak_ 表（升序）；schema_migrations 是 migrate
// 自己的迁移记录表，不算业务表。
func businessTables(ctx context.Context, t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT table_name FROM information_schema.tables
		WHERE table_schema = DATABASE() AND table_name LIKE 'ak\_%' ORDER BY table_name`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list tables: %v", err)
	}
	return out
}

func TestVerifySchemaVersion(t *testing.T) {
	db := testDB(t) // 已 Migrate 到最新
	ctx := context.Background()

	if err := VerifySchemaVersion(ctx, db); err != nil {
		t.Fatalf("up-to-date schema: %v", err)
	}

	// 版本落后：拒绝
	if _, err := db.ExecContext(ctx, `UPDATE schema_migrations SET version = version - 1`); err != nil {
		t.Fatal(err)
	}
	err := VerifySchemaVersion(ctx, db)
	if err == nil || !strings.Contains(err.Error(), "code expects") {
		t.Fatalf("stale schema err = %v, want version mismatch", err)
	}

	// dirty：拒绝
	if _, err := db.ExecContext(ctx, `UPDATE schema_migrations SET version = version + 1, dirty = 1`); err != nil {
		t.Fatal(err)
	}
	err = VerifySchemaVersion(ctx, db)
	if err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("dirty schema err = %v, want dirty rejection", err)
	}

	// 复原，避免影响同库其它用例（testDB 每次 Migrate 是幂等的，但显式复原更干净）
	if _, err := db.ExecContext(ctx, `UPDATE schema_migrations SET dirty = 0`); err != nil {
		t.Fatal(err)
	}
}
