#!/bin/sh
# 先发送 SIGTERM 等待服务退出，超时后再发送 SIGKILL。
set -u

PID_FILE=${AGENTK_CAPABILITY_DEMO_PID_FILE:-/tmp/agentk-capability-demo.pid}
[ -f "${PID_FILE}" ] || exit 0

PID=$(cat "${PID_FILE}")
case "${PID}" in
    ''|*[!0-9]*)
        rm -f "${PID_FILE}"
        exit 0
        ;;
esac

if ! kill -0 "${PID}" 2>/dev/null; then
    rm -f "${PID_FILE}"
    exit 0
fi

kill -TERM "${PID}" 2>/dev/null || true
remaining=30
while [ "${remaining}" -gt 0 ]; do
    if ! kill -0 "${PID}" 2>/dev/null; then
        rm -f "${PID_FILE}"
        exit 0
    fi
    sleep 1
    remaining=$((remaining - 1))
done

kill -KILL "${PID}" 2>/dev/null || true
rm -f "${PID_FILE}"
exit 0
