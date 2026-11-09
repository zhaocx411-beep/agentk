#!/bin/sh
set -eu

: "${AUTO_PORT0:?AUTO_PORT0 is required}"
export MCP_PORT="${AUTO_PORT0}"

cd /home/web_server/storyboard-example/python/mcp-server
exec /opt/venv/bin/python main.py
