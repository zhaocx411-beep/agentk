#!/usr/bin/env bash
# 本地联调三件套：AgentK standalone + Narrato Worker + Demo Web。
#
# 解决的几个易错点：
#   1. AgentK 内嵌 Temporal 每次启动端口随机，Worker 必须拿到新端口才连得上，
#      所以这里从启动日志里抓端口，而不是写死。
#   2. AgentK 必须先 ready 才能起 Worker（Worker 启动期要校验 Core 能力）。
#   3. ffmpeg 需要 libass 才能烧字幕，Homebrew 主 formula 不带，用 keg-only 的
#      ffmpeg-full 并只在本脚本拉起的进程里改 PATH，不污染全局。
#   4. KWS_LANE_ID 若从外部继承会把请求路由到别的 lane，必须显式清掉。
#
# 用法：
#   scripts/dev-stack.sh start    # 全新启动（先清干净）
#   scripts/dev-stack.sh stop     # 停掉三个进程
#   scripts/dev-stack.sh status   # 看当前状态
#   scripts/dev-stack.sh logs     # 跟踪三份日志
set -uo pipefail

AGENTK_REPO="${AGENTK_REPO:-$HOME/Desktop/项目/jl/AgentK/agent-k}"
DEMO_REPO="${DEMO_REPO:-$HOME/Desktop/项目/KlingAI-AgentK/Contrib/agentk-capability-demo}"
AGENTK_BIN="${AGENTK_BIN:-/tmp/agentk-standalone-bin}"
FFMPEG_FULL_BIN="/opt/homebrew/opt/ffmpeg-full/bin"

AGENTK_PORT=8081
WEB_PORT=8080

LOG_AGENTK=/tmp/agentk-standalone.log
LOG_WORKER=/tmp/narrato-worker.log
LOG_WEB=/tmp/demo-web.log

# 线上模型目录走 KConf；.internal 网关主机在办公网不可达，改写成 corp 入口。
KCONF_MODELS_KEY=klingai.agentk.models
GATEWAY_REWRITE=kigress-gateway.internal=kigress-gateway.corp.kuaishou.com

BLOB_BUCKET=klingcorp-agentk-engine
BLOB_ENDPOINT=https://bs3-hb1.staging.kuaishou.com

# 把命令放进独立 session 后台执行，stdout/stderr 重定向到日志。
# 只用 nohup/disown 不够：父 shell 所在的进程组被整体终止时（IDE 终端、CI、
# agent 工具等场景很常见）子进程会一起死。setsid 才能真正脱离，而 macOS 不自带
# setsid，故借 python 完成 setsid + exec。
run_detached() {
  local log="$1"; shift
  python3 -c '
import os, sys
log = sys.argv[1]
os.setsid()
fd = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o644)
os.dup2(fd, 1)
os.dup2(fd, 2)
os.close(os.open(os.devnull, os.O_RDONLY))
os.execvp(sys.argv[2], sys.argv[2:])
' "$log" "$@" &
}

say() { printf "\033[1;32m==>\033[0m %s\n" "$*"; }
warn() { printf "\033[1;33m[!]\033[0m %s\n" "$*"; }
die() { printf "\033[1;31m[x]\033[0m %s\n" "$*" >&2; exit 1; }

stop_all() {
  say "停止进程"
  pkill -f "$(basename "$AGENTK_BIN")" 2>/dev/null
  pkill -9 -f "agentk-capability-demo-worker" 2>/dev/null
  # Web 的进程名是 agentk-capability-demo，不能用会同时匹配到 worker 的模式。
  pkill -f "bin/agentk-capability-demo --host" 2>/dev/null
  sleep 2
}

# 清掉上次被强杀留下的产物临时目录（正常退出时进程会自己删）。
clean_state() {
  say "清理残留状态"
  rm -rf /var/folders/*/*/T/agentk-files-* 2>/dev/null
  rm -rf "${TMPDIR:-/tmp}"/agentk-files-* 2>/dev/null
  # 日志归档而不是截断：执行历史存在内存里，重启即失。日志往往是唯一能回溯
  # 上一轮跑了什么、产物 URL 是多少的线索，清掉会让问题无从查起。
  local stamp archive
  stamp=$(date +%Y%m%d-%H%M%S)
  archive="/tmp/agentk-dev-logs/$stamp"
  for f in "$LOG_AGENTK" "$LOG_WORKER" "$LOG_WEB"; do
    if [ -s "$f" ]; then
      mkdir -p "$archive"
      cp "$f" "$archive/$(basename "$f")"
    fi
    : > "$f"
  done
  [ -d "$archive" ] && say "上轮日志已归档到 $archive"
  # 只保留最近 10 份归档。
  ls -1dt /tmp/agentk-dev-logs/*/ 2>/dev/null | tail -n +11 | xargs -I{} rm -rf {} 2>/dev/null
  return 0
}

preflight() {
  [ -x "$AGENTK_BIN" ] || die "AgentK 二进制不存在：$AGENTK_BIN"
  [ -d "$AGENTK_REPO" ] || die "AgentK 仓库不存在：$AGENTK_REPO"
  [ -d "$DEMO_REPO" ] || die "Demo 仓库不存在：$DEMO_REPO"
  [ -x "$FFMPEG_FULL_BIN/ffmpeg" ] \
    || die "缺少带 libass 的 ffmpeg，请先 brew install ffmpeg-full"
  "$FFMPEG_FULL_BIN/ffmpeg" -hide_banner -filters 2>/dev/null \
    | awk '$2=="subtitles"{found=1} END{exit !found}' \
    || die "ffmpeg-full 没有 subtitles 滤镜（libass 缺失）"
  [ -f "$DEMO_REPO/config.narrato.yaml" ] \
    || die "缺少 $DEMO_REPO/config.narrato.yaml"
}

start_agentk() {
  say "启动 AgentK standalone (:$AGENTK_PORT)"
  ( cd "$AGENTK_REPO" && run_detached "$LOG_AGENTK" env -u KWS_LANE_ID \
      PATH="$FFMPEG_FULL_BIN:$PATH" \
      AGENTK_MODELS=./models.yaml \
      AGENTK_MODELS_KCONF_KEY="$KCONF_MODELS_KEY" \
      AGENTK_MODELS_HOST_REWRITE="$GATEWAY_REWRITE" \
      AGENTK_ARTIFACT_KCONF=1 \
      AGENTK_DEBUG_MEDIA_REQUEST="${AGENTK_DEBUG_MEDIA_REQUEST:-}" \
      KWS_SERVICE_STAGE=STAGING \
      "$AGENTK_BIN" --bind-address 127.0.0.1 --port "$AGENTK_PORT" )

  # KConf 首次拉取比较慢，给到 90 秒。
  # run_detached 要先起 python、setsid 再 exec，进程不会立刻出现在进程表里，
  # 所以前几轮不做存活判断，否则会把还没 exec 完的启动误判成崩溃。
  local i
  for i in $(seq 1 90); do
    if grep -q "agentk-standalone ready" "$LOG_AGENTK" 2>/dev/null; then return 0; fi
    if [ "$i" -gt 5 ] && ! pgrep -f "$(basename "$AGENTK_BIN")" >/dev/null; then
      warn "AgentK 进程已退出，日志尾部："
      grep -vE 'KCONF|slog before flag' "$LOG_AGENTK" | tail -15
      return 1
    fi
    sleep 1
  done
  warn "AgentK 启动超时"; return 1
}

start_worker() {
  local tp
  tp=$(grep -oE 'temporal=127\.0\.0\.1:[0-9]+' "$LOG_AGENTK" | tail -1 | cut -d= -f2)
  [ -n "$tp" ] || { warn "抓不到 Temporal 端口"; return 1; }
  say "启动 Narrato Worker (Temporal $tp)"
  ( cd "$DEMO_REPO" && run_detached "$LOG_WORKER" env -u KWS_LANE_ID \
      PATH="$FFMPEG_FULL_BIN:$PATH" \
      KWS_SERVICE_STAGE=STAGING \
      KWS_SERVICE_NAME=klingai-agentk-capability-demo \
      NARRATO_CONFIG="$DEMO_REPO/config.narrato.yaml" \
      NARRATO_AGENTK_BASE_URL="http://127.0.0.1:$AGENTK_PORT" \
      NARRATO_TEMPORAL_ADDRESS="$tp" \
      NARRATO_TEMPORAL_NAMESPACE=agentk \
      NARRATO_WORKER_TASK_QUEUE=demo-worker \
      NARRATO_CORE_TASK_QUEUE=agentk \
      uv run agentk-capability-demo-worker narrato )
  sleep 12
}

start_web() {
  say "启动 Demo Web (:$WEB_PORT)"
  ( cd "$DEMO_REPO" && run_detached "$LOG_WEB" env -u KWS_LANE_ID \
      AGENTK_ADDRESS="http://127.0.0.1:$AGENTK_PORT" \
      AGENTK_DEMO_BLOB_BUCKET="$BLOB_BUCKET" \
      AGENTK_DEMO_BLOB_ENDPOINT="$BLOB_ENDPOINT" \
      KWS_SERVICE_NAME=klingai-agentk-capability-demo \
      KWS_SERVICE_STAGE=STAGING \
      NARRATO_CONFIG="$DEMO_REPO/config.narrato.yaml" \
      "$DEMO_REPO/.venv/bin/agentk-capability-demo" --host 127.0.0.1 --port "$WEB_PORT" )
  sleep 10
}

status() {
  printf "\n"
  printf "  %-14s " "AgentK :$AGENTK_PORT"
  curl -sS -m 5 "http://127.0.0.1:$AGENTK_PORT/healthz" 2>/dev/null || printf "DOWN"
  printf "\n  %-14s " "Demo   :$WEB_PORT"
  curl -sS -m 8 "http://127.0.0.1:$WEB_PORT/api/connection" 2>/dev/null | head -c 30 || printf "DOWN"
  printf "\n  %-14s " "Worker"
  pgrep -f "agentk-capability-demo-worker narrato" >/dev/null && printf "running" || printf "DOWN"
  printf "\n\n"
  grep -E "改写|目录版本|ready" "$LOG_AGENTK" 2>/dev/null | tail -3
  printf "\n  Narrato 能力：\n"
  curl -sS -m 12 "http://127.0.0.1:$WEB_PORT/api/agent100/narrato/config" 2>/dev/null \
    | python3 -c "
import json,sys
try: d=json.load(sys.stdin)
except Exception: print('    (取不到)'); raise SystemExit
for c in (d.get('capabilities') or []):
    if 'narrato' in c.get('id',''):
        print('   ', c['id'], '->', 'PUBLISHED' if c.get('published') else 'MISSING')
" 2>/dev/null
  printf "\n  页面： http://127.0.0.1:%s\n\n" "$WEB_PORT"
}

case "${1:-start}" in
  start)
    preflight
    stop_all
    clean_state
    start_agentk || die "AgentK 启动失败，见 $LOG_AGENTK"
    start_worker || die "Worker 启动失败，见 $LOG_WORKER"
    start_web
    status
    ;;
  stop)   stop_all; say "已停止" ;;
  status) status ;;
  logs)   tail -f "$LOG_AGENTK" "$LOG_WORKER" "$LOG_WEB" ;;
  *)      die "用法：$0 {start|stop|status|logs}" ;;
esac
