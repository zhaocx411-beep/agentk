"""全项目唯一的路径锚点。

归拢目录前，`Path(__file__).resolve().parent / "runtime"` 这一句在 5 个模块里
各写了一份，`parent.parent` 的层数也散落在 judge/dataset 两处。文件一旦换层级，
这些推算会**静默**指向别的地方——不报错，只是读写到错误的目录。所以路径只在
这里算一次，其余模块一律从这里取。

三个层级的含义：
- PACKAGE_ROOT：包目录本身，随包内代码走（静态资源属于包，一起分发）。
- REPO_ROOT   ：仓库根，运行时产物（runtime/）放这里，不进包也不进镜像层。
- WORKSPACE_ROOT：仓库的上一级。数据集（I2VA*/T2VA* 目录、mm-datasets）按约定
  与仓库平级存放，不纳入版本管理，因此需要往上跳一层。
"""
from __future__ import annotations

from pathlib import Path

PACKAGE_ROOT = Path(__file__).resolve().parent
REPO_ROOT = PACKAGE_ROOT.parents[1]
WORKSPACE_ROOT = REPO_ROOT.parent

STATIC_ROOT = PACKAGE_ROOT / "static"

RUNTIME_ROOT = REPO_ROOT / "runtime"
RESULTS_ROOT = RUNTIME_ROOT / "results"
WORK_ROOT = RUNTIME_ROOT / "work"
REPORT_ROOT = RUNTIME_ROOT / "reports"
TRACE_ROOT = RUNTIME_ROOT / "traces"
UPLOAD_ROOT = RUNTIME_ROOT / "uploads"
THUMB_ROOT = RUNTIME_ROOT / "dataset_thumbs"

DEFAULT_DATASET_CACHE = WORKSPACE_ROOT / "mm-datasets" / "0811-dataset" / "cache"
