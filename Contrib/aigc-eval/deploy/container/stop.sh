#!/bin/sh
# 容器云停止钩子：kcsize 通过 STOP_COMMAND 调用。
# 先 SIGTERM 给 start.sh 收尾（它会把 worker 与 HTTP 一起停掉），超时再 SIGKILL。
# 留出 30 秒是为了让 worker 把在跑的质检交还队列，而不是让调用方等到超时。
set -u

PID_FILE=${AIGC_EVAL_PID_FILE:-/tmp/aigc-eval.pid}
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
