"""阶段产物的共享存储层：把落在本地盘上的帧图/证据图搬到对象存储，并能在另一台
实例上还原到同样的路径。

为什么需要它：质检拆成多个 Temporal activity 之后，Temporal **不保证**同一次质检的
各个 activity 落在同一台实例上。前一个阶段写在容器本地盘的 `frame_*.jpg`、
`zoom_*.jpg`，后一个阶段可能根本看不到。产物层负责的就是这段传递：

    阶段 A 产出文件 → put_files() 得到 manifest（跟着 activity 结果一起回 workflow）
    阶段 B 开工前  → materialize(manifest) 把文件还原到本地同一路径 → 照原样跑

关键取舍：**还原到原路径**，而不是把下游代码改成读 URL。整条 pipeline 里抽帧、拼图、
标框、HTML 内嵌全都在跟 `Path` 打交道，改成流式读会波及十几处；还原成文件让下游代码
一行不用动，代价只是一次本地写盘。

寻址方式是内容哈希，所以：
* 同一份字节在一次 run 里只上传一次，跨 run 也能命中已存在的对象（HEAD 判定）；
* manifest 里记的是"相对 RUNTIME_ROOT 的路径 + key"，因此换实例后还原出的相对布局
  与产出时完全一致。

没配对象存储时（开发机、离线回放）整层退化成空操作：`shared_artifact_store()` 返回
None，产出照旧留在本地盘，manifest 为空。单机跑法因此完全不受影响。
"""

from __future__ import annotations

import hashlib
import logging
import os
from collections.abc import Iterable
from pathlib import Path
from typing import Any

from aigc_eval.paths import RUNTIME_ROOT

logger = logging.getLogger(__name__)

# 键里带版本号：改了 manifest 结构或 key 的构成就整体失效，
# 免得新代码按新口径去读旧 manifest。
ARTIFACT_VERSION = "af1"

_BLOB_PREFIX = "aigc-eval/artifacts"


class ArtifactMissing(RuntimeError):
    """后续阶段需要的产物既不在本地、也拉不回来。

    这个异常必须能被上抛：模型判定要用的高清帧缺了，继续跑只会得到一份"没看见证据"
    的报告，而那比直接失败更难发现。
    """


def artifacts_enabled() -> bool:
    """``JUDGE_ARTIFACT_STORE=0`` 显式关掉；默认开。"""
    return os.environ.get("JUDGE_ARTIFACT_STORE", "1").strip().lower() not in (
        "0",
        "false",
        "no",
    )


def _relative_key_path(path: Path) -> str | None:
    """产物在 RUNTIME_ROOT 内的相对路径；不在其内返回 None。

    只收 RUNTIME_ROOT 以内的文件：那是本项目自己产出的中间物，路径布局由代码决定，
    换机器还原不会踩到别人的目录。数据集原片这类外部素材不走这里。
    """
    try:
        return path.resolve().relative_to(RUNTIME_ROOT.resolve()).as_posix()
    except ValueError:
        return None


class ArtifactStore:
    """内容寻址的产物上传与还原。"""

    kind = "blob"

    def __init__(self, store: Any):
        self._store = store
        # 本进程内已确认在对象存储上的 digest，避免一次 run 里反复 HEAD/PUT
        # 同一份字节（帧图在 4b/4c/证据三个阶段之间会被反复引用）。
        self._known: set[str] = set()

    def _key(self, digest: str, suffix: str) -> str:
        return f"{_BLOB_PREFIX}/{ARTIFACT_VERSION}/{digest[:2]}/{digest}{suffix.lower()}"

    def put_files(self, paths: Iterable[Path | str]) -> list[dict[str, Any]]:
        """上传一批文件，返回 manifest 条目。

        单个文件失败只记日志、不进 manifest，也不抛异常：真正需要它的是下游阶段，
        缺失会在 ``materialize(required=True)`` 那里以明确的错误暴露出来，比在
        产出阶段炸掉更容易定位。
        """
        manifest: list[dict[str, Any]] = []
        for raw in paths:
            path = Path(raw)
            rel = _relative_key_path(path)
            if rel is None:
                continue
            try:
                data = path.read_bytes()
            except OSError:
                logger.warning("产物读取失败，跳过上传：%s", path, exc_info=True)
                continue
            digest = hashlib.sha256(data).hexdigest()
            key = self._key(digest, path.suffix)
            try:
                if digest not in self._known:
                    if not self._store.exists(key):
                        self._store.put_at(
                            key, data, content_type=_content_type(path.suffix)
                        )
                    self._known.add(digest)
            except Exception:  # noqa: BLE001 - 上传失败不在这里中断
                logger.warning("产物上传失败，跳过：%s", path, exc_info=True)
                continue
            manifest.append(
                {"path": rel, "key": key, "sha256": digest, "size": len(data)}
            )
        return manifest

    def materialize(
        self, refs: Iterable[dict[str, Any]], *, required: bool = False
    ) -> dict[str, int]:
        """把 manifest 里的文件还原到 RUNTIME_ROOT 下的原相对路径。

        已在本地且大小一致的文件直接跳过——同机重跑（最常见的情况）因此不产生任何
        网络流量。``required=True`` 时拉不回来就抛 ``ArtifactMissing``。
        """
        stats = {"present": 0, "restored": 0, "missing": 0}
        for ref in refs:
            rel = str(ref.get("path") or "")
            key = str(ref.get("key") or "")
            if not rel or not key:
                continue
            target = RUNTIME_ROOT / rel
            size = ref.get("size")
            if target.exists() and (not isinstance(size, int) or target.stat().st_size == size):
                stats["present"] += 1
                continue
            data: bytes | None = None
            try:
                data = self._store.get_bytes(key)
            except Exception:  # noqa: BLE001 - 网络失败与不存在一并按缺失处理
                logger.warning("产物拉取失败：%s", key, exc_info=True)
            if not data:
                stats["missing"] += 1
                if required:
                    raise ArtifactMissing(
                        f"阶段所需产物缺失：{rel}（key={key}）。"
                        "该文件既不在本地盘，也无法从对象存储取回。"
                    )
                continue
            try:
                target.parent.mkdir(parents=True, exist_ok=True)
                # 先写临时文件再原子改名：同目录下可能有并发的读方
                # （批量模式多个 case 共享 runtime 目录）。
                tmp = target.with_suffix(f"{target.suffix}.{os.getpid()}.tmp")
                tmp.write_bytes(data)
                os.replace(tmp, target)
            except OSError:
                logger.warning("产物落盘失败：%s", target, exc_info=True)
                stats["missing"] += 1
                if required:
                    raise ArtifactMissing(f"产物落盘失败：{rel}") from None
                continue
            stats["restored"] += 1
        return stats


def _content_type(suffix: str) -> str:
    from aigc_eval.providers.blob_store import guess_mime

    return guess_mime(suffix)


def shared_artifact_store() -> ArtifactStore | None:
    """配了对象存储就返回实例，否则返回 None（单机跑法照旧用本地盘）。"""
    if not artifacts_enabled():
        return None

    from aigc_eval.providers.blob_store import BlobStore, load_blob_config

    if not load_blob_config().configured:
        return None
    try:
        return ArtifactStore(BlobStore())
    except Exception:  # noqa: BLE001 - 配了但建不起来，退化成本地盘
        logger.warning("对象存储不可用，产物层退化为仅本地盘", exc_info=True)
        return None


def put_files(
    store: ArtifactStore | None, paths: Iterable[Path | str]
) -> list[dict[str, Any]]:
    """没有 store 时返回空 manifest，让调用方不必到处判空。"""
    if store is None:
        return []
    return store.put_files(paths)


def materialize(
    store: ArtifactStore | None,
    refs: Iterable[dict[str, Any]] | None,
    *,
    required: bool = False,
) -> dict[str, int]:
    """没有 store 或 manifest 为空时是空操作（单机路径走的就是这条）。"""
    if store is None or not refs:
        return {"present": 0, "restored": 0, "missing": 0}
    return store.materialize(refs, required=required)
