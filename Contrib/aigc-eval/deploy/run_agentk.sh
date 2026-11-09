#!/bin/bash
# AgentK 通道启动脚本。
#
# 所有模型调用走 AgentK 的 agentk.understand_media / agentk.generate_text 能力，
# 不直连 Gemini。需要两组配置，缺一不可：
#   1. AgentK 身份：KWS_SERVICE_NAME（天琴服务名，唯一机制。旧的 AGENTK_API_KEY
#      平台已下线，官方 Client SDK 也不再支持发 Bearer）。
#   2. 对象存储：AIGC_EVAL_BLOB_ENDPOINT + AIGC_EVAL_BLOB_BUCKET
#      understand_media 只收 HTTP URL，本地文件必须先落到 blob 上。
# 配置是否齐全可以直接看 GET /api/health 的 providers.agentk。
set -euo pipefail
# 脚本住在 deploy/，但要在仓库根跑：runtime/ 等产物目录以仓库根为锚点。
cd "$(dirname "$0")/.."

export VIDEO_JUDGE_PROVIDER=agentk
# 本地默认连 staging。只需要这一个变量：网关地址由 stage 推导（STAGING/CANDIDATE
# → staging 网关，其余 → 生产），所以不必再配 AGENTK_BASE_URL。要打到别的环境
# 时才 export AGENTK_BASE_URL 覆盖它。
export KWS_SERVICE_STAGE="${KWS_SERVICE_STAGE:-STAGING}"
export AGENTK_UNDERSTAND_MODEL="${AGENTK_UNDERSTAND_MODEL:-gemini-3.1-pro-preview}"

# 合并扫描默认送「整段视频 + 16 张高清帧」共 17 件，而 understand_media 的
# mediaUrls 上限是 10。不改这里也能跑（客户端会均匀降载并在报告里标注），
# 但显式压到 9 张帧 + 1 段视频正好贴合上限，可以避免任何素材被丢弃。
export JUDGE_UNIFIED_SCAN_FRAMES="${JUDGE_UNIFIED_SCAN_FRAMES:-9}"

if [ -z "${KWS_SERVICE_NAME:-}" ]; then
  echo "AgentK 身份未配置，只需导出天琴服务名：" >&2
  echo "  export KWS_SERVICE_NAME=klingai-agentk-aigc-eval-api" >&2
  exit 1
fi
echo "AgentK 身份：${KWS_SERVICE_NAME}"
if [ -z "${AIGC_EVAL_BLOB_ENDPOINT:-}" ] || [ -z "${AIGC_EVAL_BLOB_BUCKET:-}" ]; then
  echo "对象存储未配置：需要 AIGC_EVAL_BLOB_ENDPOINT 与 AIGC_EVAL_BLOB_BUCKET。" >&2
  echo "staging 的 endpoint 是 https://bs3-hb1.staging.kuaishou.com，" >&2
  echo "bucket 用业务共享桶名（向平台侧索取）。URL 只需 AgentK 服务端可达，" >&2
  echo "内网域名即可，不需要公网 CDN。" >&2
  exit 1
fi

# src 布局：包没装进环境时也能直接跑。
export PYTHONPATH="$PWD/src${PYTHONPATH:+:$PYTHONPATH}"

# 这条通道要用官方 Client SDK（ks-agentk-client），它装在 uv 管的 .venv 里，
# 所以不走 find_python.sh 的系统解释器——run_offline.sh 才走那条零依赖路线。
# --no-sync：不想每次启动都去内部源解析一遍依赖（离线时会直接失败）。
if command -v uv >/dev/null 2>&1; then
  if [ "$#" -eq 0 ]; then
    exec uv run --no-sync python -m aigc_eval
  fi
  exec uv run --no-sync python "$@"
fi

# 没有 uv 时退回系统解释器：只要 SDK 已经装在那个环境里就同样能跑。
source "$PWD/deploy/find_python.sh"
if ! "$PY" -c 'import agentk.client' 2>/dev/null; then
  echo "找不到 ks-agentk-client（agentk 通道必需）。装 uv 后执行：" >&2
  echo "  uv sync --extra agentk" >&2
  exit 1
fi
if [ "$#" -eq 0 ]; then
  exec "$PY" -m aigc_eval
fi
exec "$PY" "$@"
