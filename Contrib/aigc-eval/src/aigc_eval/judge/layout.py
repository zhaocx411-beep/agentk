"""判定链用到的目录锚点，以及中间产物的落盘布局。

只是给 `aigc_eval.paths` 里那几个根目录起判定链内部的别名，外加 `work_dir`
这一条命名约定。单独成文件是因为 cases / media_parts / zoom_inspector 都要用，
若留在编排里会让它们反向依赖编排、进而成环。
"""
from __future__ import annotations

from pathlib import Path

from aigc_eval import paths


PROJECT_ROOT = paths.WORKSPACE_ROOT


RUNTIME_ROOT = paths.RUNTIME_ROOT


RESULTS_ROOT = paths.RESULTS_ROOT


# 抽帧/裁图等中间产物的根。以前这些目录直接平铺在 runtime/ 顶层
# （`{slug}__{source}_zoom` 之类，14 种后缀 × 每条视频 = 上千个目录），
# 现在统一收进 runtime/work/<slug>__<source>/ 下，一条视频一个目录。
WORK_ROOT = paths.WORK_ROOT


def work_dir(slug: str, source: str, name: str, run_id: str = "") -> Path:
    """中间产物目录：runtime/work/<slug>__<source>/[<run_id>/]<name>。

    带 run_id 时按 run 再分一层 —— `extract_native_frames` 会清空目标目录里的
    `zoom_*.jpg`，同一条视频的并发 run 若共用一个目录，后启动的会把先启动的
    帧删掉，导致 `scan_unified` 抽不到帧、静默回退到 37 次调用的旧路径。
    """
    base = WORK_ROOT / f"{slug}__{source}"
    return (base / run_id / name) if run_id else (base / name)
