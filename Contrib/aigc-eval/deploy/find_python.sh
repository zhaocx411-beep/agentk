#!/bin/bash
# 解释器发现。被 run_*.sh 共同 source，导出 $PY。
#
# report_html.py 用了 PEP 701 f-string（嵌套同类引号），3.12 以下直接 SyntaxError，
# 而 macOS 自带的 python3 还是 3.9，所以不能简单用 `python3`。
# 优先级：调用方显式指定的 $PY > 常见的 3.12+ 具名解释器 > 版本达标的 python3。

_py_ok() {
  [ -x "$1" ] && "$1" -c 'import sys; sys.exit(0 if sys.version_info >= (3, 12) else 1)' 2>/dev/null
}

# ${PY:-} 而不是 $PY：本脚本被 run_*.sh 以 `set -u` source，而 PY 未设置才是
# 常态（调用方显式指定只是可选项），直接引用会以 unbound variable 退出。
if ! _py_ok "${PY:-}"; then
  PY=""
  for _candidate in \
    python3.14 python3.13 python3.12 \
    /opt/homebrew/bin/python3.14 /opt/homebrew/bin/python3.13 /opt/homebrew/bin/python3.12 \
    /usr/local/bin/python3.12 python3
  do
    _resolved="$(command -v "$_candidate" 2>/dev/null || echo "$_candidate")"
    if _py_ok "$_resolved"; then
      PY="$_resolved"
      break
    fi
  done
fi

if [ -z "${PY:-}" ]; then
  echo "找不到 Python 3.12+（report_html.py 依赖 PEP 701 f-string）。" >&2
  echo "请安装后重试，或显式指定：PY=/path/to/python3.12 $0" >&2
  exit 1
fi

export PY
