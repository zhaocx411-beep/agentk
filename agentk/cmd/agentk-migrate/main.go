// agentk-migrate 使用 AGENTK_MYSQL_DSN 执行仓库内嵌的数据库迁移。
// 用法：AGENTK_MYSQL_DSN="user:pass@tcp(host:3306)/db?parseTime=true&multiStatements=true" agentk-migrate
//
// KDB 的服务账号没有 DDL 权限且不暴露管理 DSN；发布前须通过数据库变更流程
// 执行同一组迁移。API、Worker 和管理命令启动时只验证 schema 版本，不执行 DDL。
package main

import (
	"database/sql"
	"log"
	"os"

	_ "github.com/go-sql-driver/mysql"

	"agentk/internal/store"
)

func main() {
	dsn := os.Getenv("AGENTK_MYSQL_DSN")
	if dsn == "" {
		log.Fatal("AGENTK_MYSQL_DSN is required")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := store.Migrate(db); err != nil {
		log.Fatal(err)
	}
	log.Println("schema is up to date")
}
