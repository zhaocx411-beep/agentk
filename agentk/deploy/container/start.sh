#!/bin/sh
# 容器云启动脚本：kcsize supervisor 模式经 supervisord 调起（SUPERVISOR_COMMAND0）。
# 宿主机网络禁用固定端口，HTTP 监听端口用容器云分配的 ${AUTO_PORT0}；
# kcsize 对该端口做 /healthz 检查（SERVER_TYPE=http，见 Dockerfile）。
#
# 必须保持 POSIX sh 兼容：Ubuntu 的 sh 是 dash，
# 不支持 pipefail 和进程替换。
set -eu

APP_HOME=/home/web_server/agentk
# 平台提供的 log 软链指向当前 Pod 的日志采集目录。
LOG_DIR=/home/web_server/kuaishou-runner/log

: "${AUTO_PORT0:?kcsize did not set AUTO_PORT0}"
export AGENTK_LISTEN=":${AUTO_PORT0}"

# 其余配置（AGENTK_MODE / AGENTK_STORE / AGENTK_AUTH 等）由容器云环境变量下发，
# 缺失或非法时进程自己拒启，脚本不重复校验。

# 业务日志写入宿主机挂载目录；文件名以 .log 结尾，供平台磁盘清理策略识别。
# 进程替换由 exec 完成，supervisord 看到的 PID、信号和退出码不变。
LOG_FILE="${LOG_DIR}/agentk-api.stdout_$(date +%Y%m%d).log"
exec "${APP_HOME}/bin/agentk-api" >> "${LOG_FILE}" 2>&1
