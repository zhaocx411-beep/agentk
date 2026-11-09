#!/bin/sh
# 容器云启动脚本：kcsize supervisor 模式经 supervisord 调起（SUPERVISOR_COMMAND0）。
# 纯 Temporal Worker，无 HTTP 监听，不使用 AUTO_PORT0。
#
# 必须保持 POSIX sh 兼容：运行镜像的 sh 可能是 dash/busybox，
# 不支持 pipefail 和进程替换。
set -eu

# 二进制与本脚本同目录（bin/），按脚本位置定位，兼容独立 worker 镜像
# 与 apiserver 合并镜像两种布局。
BIN_DIR=$(cd "$(dirname "$0")" && pwd)
# 挂载到宿主机的日志目录，磁盘满前会自动清理 .log 结尾文件
LOG_DIR=/home/web_server/kuaishou-runner/logs
mkdir -p "${LOG_DIR}"

# Temporal 与 AgentK 都由 SDK 经 KESS 自动发现；不要手工设置 AGENTK_BASE_URL /
# KWS_SERVICE_CREDENTIAL / KWS_SERVICE_STAGE。平台身份未绑定时相关 RPC 会失败。

# 业务日志写入宿主机挂载目录；文件名以 .log 结尾，供平台磁盘清理策略识别。
# 进程替换由 exec 完成，supervisord 看到的 PID、信号和退出码不变。
LOG_FILE="${LOG_DIR}/comic-drama-worker.stdout_$(date +%Y%m%d).log"
exec "${BIN_DIR}/comic-drama-worker" >> "${LOG_FILE}" 2>&1
