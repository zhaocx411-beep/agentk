#!/bin/bash
# 离线脚本启动器：只配 PYTHONPATH，不碰任何凭据。
#
# 给那些只读 runtime/ 下已有结果的分析脚本用（batch/eval_vs_human.py、
# report/vs_human.py 等）。它们不发模型调用，因此不需要 AgentK 身份、对象存储，
# 也不需要装官方 Client SDK——用系统里任意 3.12+ 解释器即可。
#
# 要真的跑质检（会发模型调用）用 deploy/run_agentk.sh。
#
# 此前这个位置是 run_vertex.sh。它本职是启动已删除的 Gemini 直连通道，
# 但被当成通用启动器写进了上述脚本的用法说明；通道下线后那个名字只会误导。
set -euo pipefail
# 脚本住在 deploy/，但要在仓库根跑：runtime/ 等产物目录以仓库根为锚点。
cd "$(dirname "$0")/.."

source "$PWD/deploy/find_python.sh"
export PYTHONPATH="$PWD/src${PYTHONPATH:+:$PYTHONPATH}"

if [ "$#" -eq 0 ]; then
  echo "用法：./deploy/run_offline.sh <script.py|-m module> [args...]" >&2
  exit 2
fi
exec "$PY" "$@"
