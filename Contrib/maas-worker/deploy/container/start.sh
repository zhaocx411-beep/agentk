#!/bin/sh
# 容器云启动脚本：kcsize supervisor 模式经 supervisord 调起（SUPERVISOR_COMMAND0）。
# 同一镜像支持两个部署角色；不允许缺省，避免把 Worker 部署到错误环境。
set -eu

APP_HOME=/home/web_server/maas-worker
LOG_DIR=/home/web_server/kuaishou-runner/log

: "${MAAS_ROLE:?set MAAS_ROLE to gateway or worker}"

case "${MAAS_ROLE}" in
  gateway)
    : "${AUTO_PORT0:?gateway requires kcsize AUTO_PORT0}"
    export MAAS_HTTP_ADDR=":${AUTO_PORT0}"
    PROGRAM=maas-gateway
    ;;
  worker)
    : "${AUTO_PORT0:?worker requires kcsize AUTO_PORT0 for /healthz}"
    export MAAS_HTTP_ADDR=":${AUTO_PORT0}"
    PROGRAM=maas-worker
    ;;
  *)
    echo "invalid MAAS_ROLE: ${MAAS_ROLE}; expected gateway or worker" >&2
    exit 64
    ;;
esac

LOG_FILE="${LOG_DIR}/${PROGRAM}.stdout_$(date +%Y%m%d).log"
exec "${APP_HOME}/bin/${PROGRAM}" >> "${LOG_FILE}" 2>&1
