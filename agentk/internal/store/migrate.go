package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate 将 schema 升级到最新版本；已是最新时为 no-op（幂等）。
// DSN 需带 multiStatements=true。
func Migrate(db *sql.DB) error {
	return withMigrator(db, func(m *migrate.Migrate) error {
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return fmt.Errorf("migrate up: %w", err)
		}
		return nil
	})
}

// withMigrator 借一条连接建好 migrator 交给 fn，用完归还。
func withMigrator(db *sql.DB, fn func(*migrate.Migrate) error) error {
	ctx := context.Background()
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("migrate: load embedded migrations: %w", err)
	}
	// embed.FS 没有 Closer，这个 Close 实际是 no-op；显式关掉只为资源规范
	defer func() { _ = src.Close() }()

	// 用 WithConnection 而非 WithInstance：自己借连接、用完归还。
	// WithInstance 会独占一条池连接且无法安全归还——它的 Close 会连带关掉调用方的 db。
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("migrate: acquire conn: %w", err)
	}
	defer func() { _ = conn.Close() }()

	driver, err := migratemysql.WithConnection(ctx, conn, &migratemysql.Config{})
	if err != nil {
		return fmt.Errorf("migrate: init driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "mysql", driver)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return fn(m)
}

// ExpectedSchemaVersion 返回代码期望的 schema 版本：内嵌 migrations 的最大序号。
// 从文件推导而非手写常量，加迁移脚本时无需记得同步改这里。
func ExpectedSchemaVersion() uint {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		panic(fmt.Sprintf("store: read embedded migrations: %v", err)) // 内嵌资源损坏属编译产物问题
	}
	var max uint64
	for _, e := range entries {
		// 文件名形如 000001_init.up.sql，序号在第一个下划线前
		name := e.Name()
		idx := strings.IndexByte(name, '_')
		if idx <= 0 {
			continue
		}
		v, err := strconv.ParseUint(name[:idx], 10, 64)
		if err != nil {
			continue
		}
		if v > max {
			max = v
		}
	}
	if max == 0 {
		panic("store: no embedded migrations found")
	}
	return uint(max)
}

// VerifySchemaVersion 探测数据库 schema 版本是否与代码期望一致。
// kdb 模式启动时调用：服务账号无 DDL 权限、迁移带外执行（cmd/agentk-migrate），
// 版本不符或迁移半途（dirty）一律拒绝启动，防止代码与 schema 错配。
func VerifySchemaVersion(ctx context.Context, db DB) error {
	want := ExpectedSchemaVersion()
	var version uint
	var dirty bool
	err := db.QueryRowContext(ctx, `SELECT version, dirty FROM schema_migrations`).Scan(&version, &dirty)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("store: schema_migrations is empty (database never migrated?), want version %d", want)
	case err != nil:
		return fmt.Errorf("store: probe schema version (run cmd/agentk-migrate first?): %w", err)
	}
	if dirty {
		return fmt.Errorf("store: schema version %d is dirty (a migration failed midway), manual repair required", version)
	}
	if version != want {
		return fmt.Errorf("store: schema version = %d, code expects %d (run cmd/agentk-migrate)", version, want)
	}
	return nil
}
