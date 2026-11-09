// Package storeboot 是持久化存储的共用装配：按 AGENTK_STORE 的取值打开 mysql
// 或 kdb 后端并做 schema 就绪检查。服务进程（internal/app）与运维 CLI
// （cmd/agentk-admin）共用这一份，环境变量约定与失败口径只此一处。
//
// mem 后端不在这里：它不是持久化存储，由服务进程自行构造（agentk-admin 管理
// 的是账户与凭证，必须落库）。
package storeboot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	_ "github.com/go-sql-driver/mysql"

	"agentk/internal/kdbstore"
	"agentk/internal/store"
)

// Open 装配持久化 store，返回实例与关闭函数（失败时资源已清理，无须再关）。
//
//   - mysql：AGENTK_MYSQL_DSN 直连，DSN 需带 parseTime=true&multiStatements=true。
//     migrate=true 时进程内迁移到最新（服务进程的本地/自管库形态）；false 时只探测
//     schema 版本，不符拒启（agentk-admin——迁移专职归 cmd/agentk-migrate）。
//   - kdb：公司 go-kdb datasource，AGENTK_KDB_BIZDEF 是业务组、
//     AGENTK_KDB_CLUSTER 是集群资源名，两者均由 DBA 分配，且需本机 Kess Agent 在岗。
//     服务账号无 DDL 权限，恒只探测版本，migrate 参数不适用。
func Open(ctx context.Context, kind string, migrate bool) (*store.Store, func(), error) {
	switch kind {
	case "mysql":
		dsn := os.Getenv("AGENTK_MYSQL_DSN")
		if dsn == "" {
			return nil, nil, errors.New("env AGENTK_MYSQL_DSN is required")
		}
		db, err := sql.Open("mysql", dsn)
		if err != nil {
			return nil, nil, fmt.Errorf("open mysql: %w", err)
		}
		closeDB := func() { _ = db.Close() }
		if err := db.PingContext(ctx); err != nil {
			closeDB()
			return nil, nil, fmt.Errorf("ping mysql: %w", err)
		}
		if migrate {
			if err := store.Migrate(db); err != nil {
				closeDB()
				return nil, nil, fmt.Errorf("migrate mysql: %w", err)
			}
		} else if err := store.VerifySchemaVersion(ctx, db); err != nil {
			closeDB()
			return nil, nil, fmt.Errorf("verify mysql schema: %w", err)
		}
		return store.New(db), closeDB, nil
	case "kdb":
		bizDef, cluster := os.Getenv("AGENTK_KDB_BIZDEF"), os.Getenv("AGENTK_KDB_CLUSTER")
		if bizDef == "" || cluster == "" {
			return nil, nil, errors.New("env AGENTK_KDB_BIZDEF and AGENTK_KDB_CLUSTER are required")
		}
		db, err := kdbstore.New(bizDef, cluster)
		if err != nil {
			return nil, nil, err
		}
		if err := store.VerifySchemaVersion(ctx, db); err != nil {
			return nil, nil, fmt.Errorf("verify kdb schema: %w", err)
		}
		return store.New(db), func() {}, nil
	default:
		return nil, nil, fmt.Errorf("unknown AGENTK_STORE %q (want mysql or kdb)", kind)
	}
}
