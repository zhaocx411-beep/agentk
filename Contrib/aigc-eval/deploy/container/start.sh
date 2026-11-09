#!/bin/sh
# 容器云入口：kcsize 通过 START_COMMAND 调用本脚本，拉起 worker + HTTP 双进程。
#
# 两个进程共享同一份 runtime 目录，这是刻意的：worker 跑完把 HTML 报告写在
# runtime/reports 下，HTTP 侧的 studio 页面就能直接翻看历史，不必再同步一份。
#
# 任一进程退出即整体退出，交给平台重启。半死状态（worker 掉了但 HTTP 还在应答
# 健康检查）比整体挂掉更难发现——worker 一停，能力注册租约到期后就从目录里消失，
# 调用方只会看到「能力不存在」，而健康检查一路绿灯。所以不做单边重启。
set -eu

APP_HOME=/home/web_server/aigc-eval
LOG_DIR=/home/web_server/kuaishou-runner/logs
PID_FILE=/tmp/aigc-eval.pid
mkdir -p "${LOG_DIR}" "${APP_HOME}/runtime"

# HTTP 端口必须用平台分配的 AUTO_PORT0；kcsize 只会对这个端口做检查与 KNS 注册。
PORT=${AUTO_PORT0:-}
if [ -z "${PORT}" ] && [ -f /home/web_server/PORT_INFO.json ]; then
    PORT=$(python3 -c 'import json; print(json.load(open("/home/web_server/PORT_INFO.json")).get("AUTO_PORT0", ""))')
fi
: "${PORT:?kcsize 未分配 AUTO_PORT0}"
export VIDEO_JUDGE_PORT="${PORT}"

echo "$$" > "${PID_FILE}"

WORKER_PID=
WEB_PID=

shutdown() {
    trap - TERM INT
    # 先礼后兵：给两个进程收尾的机会，worker 需要它把在跑的质检交还队列。
    [ -z "${WEB_PID}" ] || kill -TERM "${WEB_PID}" 2>/dev/null || true
    [ -z "${WORKER_PID}" ] || kill -TERM "${WORKER_PID}" 2>/dev/null || true
    [ -z "${WEB_PID}" ] || wait "${WEB_PID}" 2>/dev/null || true
    [ -z "${WORKER_PID}" ] || wait "${WORKER_PID}" 2>/dev/null || true
    rm -f "${PID_FILE}"
}

trap 'shutdown; exit 0' TERM INT
trap 'rm -f "${PID_FILE}"' EXIT

# kcsize 启动子进程时不转发其 stdout/stderr。PID 1 就是 kcsize，直接写它的文件
# 描述符，才能在平台日志里看到 worker 的启动校验失败（缺 blob 配置会退出码 2）。
if [ -w /proc/1/fd/1 ]; then
    exec 1>/proc/1/fd/1 2>&1
fi

# worker 目录按仓库惯例用扁平导入（from abi import ...），与 storyboard-example、
# ffmpeg-worker 一致，所以 cd 进去直接跑脚本而不是 -m 模块。
echo "[aigc-eval] starting temporal worker"
(cd "${WORKER_DIR:-${APP_HOME}/worker}" && exec python3 worker.py) &
WORKER_PID=$!

echo "[aigc-eval] starting http server on port ${PORT}"
python3 -m aigc_eval &
WEB_PID=$!

# /bin/sh 没有可移植的 wait -n；轮询两个子进程，任一退出就收掉另一个。
while kill -0 "${WORKER_PID}" 2>/dev/null && kill -0 "${WEB_PID}" 2>/dev/null; do
    sleep 1
done

if ! kill -0 "${WORKER_PID}" 2>/dev/null; then
    echo "[aigc-eval] worker 进程已退出，容器整体收尾" >&2
else
    echo "[aigc-eval] http 进程已退出，容器整体收尾" >&2
fi
shutdown
exit 1
