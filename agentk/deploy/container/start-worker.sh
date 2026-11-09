#!/bin/sh
# 容器云 Worker 启动脚本：把进程的通用 ready 信号翻译成平台日志约定。
set -eu

APP_HOME=/home/web_server/agentk
# 平台提供的 log 软链指向当前 Pod 的日志采集目录。
LOG_DIR=/home/web_server/kuaishou-runner/log
STARTUP_LOG=${LOG_DIR}/startup.log

# 清掉同一容器内 supervisor 重启遗留的成功状态。
: > "${STARTUP_LOG}"

worker_pid=$$
(
    ready() {
        printf '%s\n' 'agentk-worker,status=success' > "${STARTUP_LOG}"
        exit 0
    }
    trap ready USR1

    # Worker 在发出 ready 前退出时，监听进程也随之结束。
    while kill -0 "${worker_pid}" 2>/dev/null; do
        sleep 1
    done
) &

export AGENTK_READY_SIGNAL_PID=$!
LOG_FILE="${LOG_DIR}/agentk-worker.stdout_$(date +%Y%m%d).log"
exec "${APP_HOME}/bin/agentk-worker" >> "${LOG_FILE}" 2>&1
