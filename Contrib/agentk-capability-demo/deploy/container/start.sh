#!/bin/sh
# kcsize 通过 START_COMMAND 调用；HTTP 端口必须使用平台分配的 AUTO_PORT0。
set -eu

APP_HOME=/home/web_server/agentk-capability-demo
LOG_DIR=/home/web_server/kuaishou-runner/logs
PID_FILE=/tmp/agentk-capability-demo.pid
mkdir -p "${LOG_DIR}" "${APP_HOME}/uploads"

PORT=${AUTO_PORT0:-}
if [ -z "${PORT}" ] && [ -f /home/web_server/PORT_INFO.json ]; then
    PORT=$(python3 -c 'import json; print(json.load(open("/home/web_server/PORT_INFO.json")).get("AUTO_PORT0", ""))')
fi
: "${PORT:?kcsize did not allocate AUTO_PORT0}"

export AGENTK_CAPABILITY_DEMO_HOST=0.0.0.0
export AGENTK_CAPABILITY_DEMO_PORT="${PORT}"

cd "${APP_HOME}"
echo "$$" > "${PID_FILE}"

WORKER_PID=
WEB_PID=

shutdown() {
    trap - TERM INT
    [ -z "${WEB_PID}" ] || kill -TERM "${WEB_PID}" 2>/dev/null || true
    [ -z "${WORKER_PID}" ] || kill -TERM "${WORKER_PID}" 2>/dev/null || true
    [ -z "${WEB_PID}" ] || wait "${WEB_PID}" 2>/dev/null || true
    [ -z "${WORKER_PID}" ] || wait "${WORKER_PID}" 2>/dev/null || true
    rm -f "${PID_FILE}"
}

trap 'shutdown; exit 0' TERM INT
trap 'rm -f "${PID_FILE}"' EXIT

# kcsize 启动子进程时不会转发其 stdout/stderr。PID 1 就是 kcsize，
# 直接写入它的文件描述符，确保平台日志能展示 uvicorn 的 traceback。
if [ -w /proc/1/fd/1 ]; then
    exec 1>/proc/1/fd/1 2>&1
fi
echo "[agentk-capability-demo] starting Narrato worker"
python3 -m agentk_capability_demo.agent100.worker_cli narrato &
WORKER_PID=$!

echo "[agentk-capability-demo] starting uvicorn on port ${PORT}"
python3 -m uvicorn agentk_capability_demo.app:create_app \
    --factory \
    --host 0.0.0.0 \
    --port "${PORT}" &
WEB_PID=$!

# /bin/sh 没有可移植的 wait -n；同时监控两个子进程，任意一个退出都关闭另一个，
# 让容器平台重启完整的 Web + Worker 单元。
while kill -0 "${WORKER_PID}" 2>/dev/null && kill -0 "${WEB_PID}" 2>/dev/null; do
    sleep 1
done

echo "[agentk-capability-demo] worker or web process exited unexpectedly" >&2
shutdown
exit 1
