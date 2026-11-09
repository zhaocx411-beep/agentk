"""包入口：`python -m aigc_eval` 启动质检服务。

单独一个入口文件而不是直接把 server/app.py 当脚本跑，是为了让
`python -m aigc_eval` 和 pyproject 里的 `aigc-eval` 命令走同一条路径。
"""
from __future__ import annotations

from aigc_eval.server.app import main

if __name__ == "__main__":
    main()
