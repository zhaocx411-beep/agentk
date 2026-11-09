#!/usr/bin/env bash
# 生成公司 DB 平台工单用的 SQL（kdb 部署形态：服务账号无 DDL，schema 全靠工单手动执行）。
#
# 用法：
#   ./gen-ticket.sh <db名>              # 全量建库：1..最新，末尾 CREATE schema_migrations + INSERT
#   ./gen-ticket.sh <db名> <from>       # 增量工单：from..最新，末尾 UPDATE schema_migrations
#   ./gen-ticket.sh <db名> <from> <to>  # 增量工单：from..to，末尾 UPDATE schema_migrations
#
# 输出 = USE <db> + 区间内 schema（internal/store/migrations/*.up.sql 按序号顺序拼接，单一事实源，
# 已适配平台 lint：每列 COMMENT、显式 COLLATE、唯一索引 uniq_ 前缀、加列不带 AFTER、
# UPDATE 带 WHERE）+ golang-migrate 版本记录（服务启动的 schema 版本探测依赖它，
# 漏了会报 "schema_migrations is empty"）。
#
# 已知 lint 盲区：平台要求同一张表的多个 ALTER 合并成一条。区间内多个迁移文件动同一张表时
# 脚本不做自动合并，提工单前需手工合并。
#
# 增量模式（from > 1）的前提：线上库当前 version 恰好等于 from-1，且线上 DDL 与该版本
# 一致（无 drift）。脚本无法校验这一点，生成的 SQL 开头会以注释写明该假设，工单执行前
# 请人工确认 SELECT version FROM schema_migrations 与之相符。
set -euo pipefail

db="${1:?用法: gen-ticket.sh <db名> [from [to]]}"
from="${2:-1}"
to="${3:-}"
migrations_dir="$(dirname "$0")/../../internal/store/migrations"

echo "USE \`${db}\`;"
if [ "$from" -gt 1 ]; then
    echo "-- 增量工单：假设当前 schema_migrations.version = $((from - 1))，执行前请人工确认。"
fi

version=0
matched=0
for f in "$migrations_dir"/*.up.sql; do
    seq="$(basename "$f" | cut -d_ -f1 | sed 's/^0*//')"
    if [ "$seq" -lt "$from" ]; then
        continue
    fi
    if [ -n "$to" ] && [ "$seq" -gt "$to" ]; then
        continue
    fi
    echo
    cat "$f"
    version="$seq"
    matched=$((matched + 1))
done

if [ "$matched" -eq 0 ]; then
    echo "错误：区间 [${from}, ${to:-最新}] 内没有匹配的迁移文件" >&2
    exit 1
fi

if [ "$from" -gt 1 ]; then
    cat <<EOF

-- golang-migrate 版本记录：agentk 启动时探测 schema 版本（store.VerifySchemaVersion），
-- 版本不符或 dirty 一律拒启。工单执行本文件后版本即为 ${version}。
-- WHERE 同时兜底前置假设：线上版本不是 $((from - 1)) 时此句影响 0 行，请回查。
UPDATE schema_migrations SET version = ${version}, dirty = 0 WHERE version = $((from - 1));
EOF
else
    cat <<EOF

-- golang-migrate 版本记录：agentk 启动时探测 schema 版本（store.VerifySchemaVersion），
-- 版本不符或 dirty 一律拒启。工单执行本文件后版本即为 ${version}。
CREATE TABLE schema_migrations (
    version BIGINT  NOT NULL COMMENT 'schema 版本号（= 代码内嵌 migrations 的最大序号）',
    dirty   BOOLEAN NOT NULL COMMENT '是否有迁移半途失败',
    PRIMARY KEY (version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='golang-migrate 版本记录';

INSERT INTO schema_migrations (version, dirty) VALUES (${version}, 0);
EOF
fi
