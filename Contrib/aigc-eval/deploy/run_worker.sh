#!/bin/bash
# 本机启动 Temporal Worker（开发联调用）。
#
# 这个脚本存在的唯一理由是一个变量冲突：
#
#   * 质检链路调 understand_media 要靠 KWS_SERVICE_STAGE 推导 AgentK 网关，
#     不设它会打到生产网关，所以 worker 进程必须有 STAGING；
#   * 而 Worker SDK 的泳道规则是「有 KWS_SERVICE_STAGE、无 KWS_LANE_ID」==
#     基线实例，此时 worker 轮询基础队列 contrib.aigc-eval，会接走 staging 上
#     真实的基线流量——在开发机上这是明确的事故。
#
# 两者都落在同一个变量上，所以本机联调必须显式给 KWS_LANE_ID：stage 留着供网关
# 推导，泳道把流量隔开，worker 实际轮询 contrib.aigc-eval@<lane>。
#
# 容器部署不要用这个脚本：天琴注入 KWS_SERVICE_STAGE 且不给 KWS_LANE_ID，基线
# 身份正是那里想要的，见 deploy/container/start.sh。
set -euo pipefail
cd "$(dirname "$0")/.."

if [ -z "${KWS_SERVICE_NAME:-}" ]; then
  echo "未配置服务身份：export KWS_SERVICE_NAME=klingai-agentk-aigc-eval-api" >&2
  exit 1
fi

export KWS_SERVICE_STAGE="${KWS_SERVICE_STAGE:-STAGING}"

# 默认用系统用户名派生一个稳定的个人泳道。同一人同机每次都一样，便于反复联调时
# 用同一个泳道发请求；换人换机自然不冲突。
export KWS_LANE_ID="${KWS_LANE_ID:-dev/$(id -un)}"

echo "服务身份 ${KWS_SERVICE_NAME}，环境 ${KWS_SERVICE_STAGE}，泳道 ${KWS_LANE_ID}"
echo "（启动后看 agentk.worker.worker 那行 route 日志确认 task_queue 带上了泳道）"

# worker 的依赖装在 uv 管的 .venv 里；worker.py 用相对 import，必须在 worker/ 下跑。
export PYTHONPATH="$PWD/src${PYTHONPATH:+:$PYTHONPATH}"
cd worker
exec uv run --no-sync --project .. python worker.py
