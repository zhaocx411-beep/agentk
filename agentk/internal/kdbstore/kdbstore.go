// Package kdbstore 用公司 go-kdb datasource 实现 store.DB 接口，
// 是 AGENTK_STORE=kdb 模式下 internal/store 的底座。
//
// 两条纪律，改之前先想清楚：
//
//  1. 全部操作走 writer 连接（含只读查询）。kdb 自动读写分离意味着主从延迟，
//     而 agentk 的核心流是强 read-after-write（CreateSession 后立即回读；
//     AppendItem 依赖 session 行锁 + LAST_INSERT_ID，必须同事务同连接）。
//     切 reader 前必须逐查询证明可容忍复制延迟，不要"顺手优化"。
//
//  2. 连接生命周期收在本包内。kdb 的 Conn 是从实例池借出的独占 *sql.Conn，
//     用完必须 Close() 归还；业务代码（internal/store）只见 database/sql
//     标准类型，接触不到裸连接。
package kdbstore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"

	"git.corp.kuaishou.com/infra/infra-framework-go.git/kdb"

	"agentk/internal/store"
)

// DB 满足 store.DB；每次调用从 datasource 借一条 writer 连接，用完归还。
type DB struct {
	ds *kdb.DataSource
}

var _ store.DB = (*DB)(nil)

// New 建 kdb datasource（配置存 kconf，需本机 Kess Agent 在岗）。
// bizDef 是业务组、cluster 是集群资源名，均由 DBA 分配。
func New(bizDef, cluster string) (*DB, error) {
	ds, err := kdb.GetMySQLDataSourceV2(kdb.Option{BizDef: bizDef, Cluster: cluster})
	if err != nil {
		return nil, fmt.Errorf("kdbstore: get datasource (bizDef=%s, cluster=%s): %w", bizDef, cluster, err)
	}
	return &DB{ds: ds}, nil
}

// conn 借一条 writer 连接。非分库分表，shard 恒传 ""。
func (d *DB) conn() (*kdb.Conn, error) {
	c, err := d.ds.GetWriterConnection("")
	if err != nil {
		return nil, fmt.Errorf("kdbstore: get writer connection: %w", err)
	}
	return c, nil
}

// releaseWhenDone 后台归还连接。sql.Conn.Close 会阻塞到该连接上未释放的
// Rows/Row/Tx 全部结束（database/sql 的 closemu 语义），所以对 Query/
// QueryRow/BeginTx 这类"返回后资源仍在用"的操作，同步 Close 会死锁——
// 放到 goroutine 里等它自然结束后入池。调用方（internal/store）总是
// 及时 Close rows / Scan row / 结束事务，不会积压。
func releaseWhenDone(c *kdb.Conn) {
	go func() { _ = c.Close() }()
}

func (d *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	c, err := d.conn()
	if err != nil {
		return nil, err
	}
	defer func() { _ = c.Close() }()          // exec 返回即完成，可同步归还
	return c.ExecContext(ctx, query, args...) // 走 kdb 包装，带 perf 打点
}

func (d *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	c, err := d.conn()
	if err != nil {
		return nil, err
	}
	// 走 kdb 包装拿 perf 打点，但向上返回内嵌的 *sql.Rows——
	// store.DB 的契约是标准类型，kdb.Rows 的计数打点随之放弃（可接受）。
	rows, err := c.QueryContext(ctx, query, args...)
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	releaseWhenDone(c) // 等调用方 rows.Close() 后归还
	return rows.Rows, nil
}

func (d *DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	c, err := d.conn()
	if err != nil {
		// *sql.Row 无法在包外构造错误值；用一条必失败的查询把错误递给 Scan。
		// 走到这里说明连 writer 都借不到，服务已处于不可用状态，多一次
		// 注定失败的往返无关紧要。
		return errRow(ctx, err)
	}
	row := c.QueryRowContext(ctx, query, args...) // 内嵌 *sql.Conn 的方法，查询已即时发出
	releaseWhenDone(c)                            // 等调用方 Scan 后归还
	return row
}

func (d *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	c, err := d.conn()
	if err != nil {
		return nil, err
	}
	tx, err := c.BeginTx(ctx, opts)
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	releaseWhenDone(c) // 等 Commit/Rollback 后归还
	return tx, nil
}

// errRow 构造一个 Scan 必返回 err 的 *sql.Row。
// *sql.Row 在包外无法直接携带错误构造，绕道 sql.OpenDB + 一个
// Connect 恒失败（返回原始错误）的 connector，错误原样透传给 Scan。
func errRow(ctx context.Context, err error) *sql.Row {
	db := sql.OpenDB(errConnector{err: err})
	defer func() { _ = db.Close() }()
	return db.QueryRowContext(ctx, "SELECT 1")
}

type errConnector struct{ err error }

func (c errConnector) Connect(context.Context) (driver.Conn, error) { return nil, c.err }
func (c errConnector) Driver() driver.Driver                        { return nil }
