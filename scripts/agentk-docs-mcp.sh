#!/bin/bash
# Wrapper for agentk-docs MCP: clears stale OAuth callback listeners before starting.
set -euo pipefail

SERVER_URL="${AGENTK_DOCS_MCP_URL:-https://agentk.staging.kuaishou.com/mcp}"
CALLBACK_PORT="${AGENTK_DOCS_CALLBACK_PORT:-41467}"

if command -v lsof >/dev/null 2>&1; then
  PIDS=$(lsof -tiTCP:"${CALLBACK_PORT}" -sTCP:LISTEN 2>/dev/null || true)
  if [ -n "${PIDS}" ]; then
    kill ${PIDS} 2>/dev/null || true
    sleep 0.5
  fi
fi

exec npx -y mcp-remote "${SERVER_URL}" "${CALLBACK_PORT}"
