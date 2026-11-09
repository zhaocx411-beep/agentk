#!/bin/sh
# 容器云启动脚本：kcsize supervisor 模式经 supervisord 调起（SUPERVISOR_COMMAND0）。
# 宿主机网络禁用固定端口，HTTP 监听端口用容器云分配的 ${AUTO_PORT0}；
# kcsize 对该端口做 /healthz 检查（SERVER_TYPE=http，见 Dockerfile）。
#
# 必须保持 POSIX sh 兼容：运行镜像的 sh 可能是 dash/busybox，
# 不支持 pipefail 和进程替换。
set -eu

APP_HOME=/home/web_server/comic-drama
# 挂载到宿主机的日志目录，磁盘满前会自动清理 .log 结尾文件
LOG_DIR=/home/web_server/kuaishou-runner/logs
mkdir -p "${LOG_DIR}"

: "${AUTO_PORT0:?kcsize did not set AUTO_PORT0}"
export API_HOST=0.0.0.0
export API_PORT="${AUTO_PORT0}"

# 漫剧缺省 profile 已内嵌进二进制（按 DRAMA_MODE 选 agent / pipeline 版），
# 需要热修 prompt 时用 PROFILE_FILE 指向盘上文件。其余配置（DRAMA_MODE /
# AGENTK_BASE_URL / AGENTK_API_KEY 等）由容器云环境变量下发，缺失或非法时
# 进程自己拒启，脚本不重复校验。

# 业务日志写入宿主机挂载目录；文件名以 .log 结尾，供平台磁盘清理策略识别。
# 进程替换由 exec 完成，supervisord 看到的 PID、信号和退出码不变。
LOG_FILE="${LOG_DIR}/comic-drama-apiserver.stdout_$(date +%Y%m%d).log"
exec "${APP_HOME}/bin/comic-drama-apiserver" >> "${LOG_FILE}" 2>&1
