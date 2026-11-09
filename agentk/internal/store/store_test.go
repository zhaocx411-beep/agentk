package store

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

// 集成测试需要真实 MySQL：读 AGENTK_TEST_MYSQL_DSN，未设置时尝试本地默认实例，
// 连不上则 skip（显式设置了 DSN 却连不上则 fail，暴露环境问题）。
const (
	defaultTestDSN  = "root:agentk@tcp(127.0.0.1:3306)/agentk?parseTime=true&multiStatements=true"
	testProfileJSON = `{"system":"","tools":[]}`
)

func testProfile() []byte {
	return []byte(testProfileJSON)
}

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn, explicit := os.LookupEnv("AGENTK_TEST_MYSQL_DSN")
	if !explicit {
		dsn = defaultTestDSN
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open dsn: %v", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		if explicit {
			t.Fatalf("AGENTK_TEST_MYSQL_DSN set but unreachable: %v", err)
		}
		t.Skipf("no local MySQL (%v); set AGENTK_TEST_MYSQL_DSN to run store tests", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, table := range []string{"ak_api_keys", "ak_service_principals",
		"ak_events", "ak_sessions",
		"ak_memberships", "ak_identities", "ak_orgs", "ak_users"} {
		if _, err := db.Exec("TRUNCATE TABLE " + table); err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
	return db
}
