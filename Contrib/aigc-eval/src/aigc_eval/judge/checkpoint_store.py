"""检查点的存储层：把单次模型调用的结果按请求哈希存到可跨进程复用的地方。

为什么需要它：质检整体是**一个** Temporal activity，Temporal 只能在 activity 边界
上续跑，所以 worker 崩溃、容器重启、被驱逐时，跑了一小时的质检没有任何中间点可续。
整份报告的缓存（``result_cache``）在流程末尾才写，崩在中途等于什么都没留下，重跑要
把四到五次模型调用的钱再花一遍。检查点填的就是这个洞：模型调用的结果一产出就落盘，
重跑时命中即跳过。

两个后端，按环境自动选：

* **blob**（配了 ``AIGC_EVAL_BLOB_ENDPOINT`` / ``AIGC_EVAL_BLOB_BUCKET``）——容器里
  唯一真正跨重启的存储。容器本地盘随实例销毁，落在那儿的检查点扛不住重启，而重启
  恰好是检查点要解决的场景，所以有 blob 就优先用 blob。
* **本地目录**（默认 ``runtime/checkpoints``）——开发机与 studio 本地跑用。

这一层的铁律是**永不抛异常**。检查点是旁路优化：存储挂了、权限不对、JSON 坏了，
代价只是这次白跑一遍模型，绝不能让一次已经花掉模型开销的质检因为缓存写失败而失败。
所以所有出口都吞异常并降级为"未命中"。
"""

from __future__ import annotations

import json
import logging
import os
import time
from pathlib import Path
from typing import Any, Protocol

from aigc_eval.paths import RUNTIME_ROOT

logger = logging.getLogger(__name__)

# 键里带版本号：改了键的构成或存进去的结构，撞版本即整体失效，
# 免得新代码读到按旧口径算出来的检查点。
STORE_VERSION = "ck1"

_BLOB_PREFIX = "aigc-eval/checkpoints"


def checkpoints_enabled() -> bool:
    """``JUDGE_CHECKPOINT=0`` 显式关掉；默认开。"""
    return os.environ.get("JUDGE_CHECKPOINT", "1").strip().lower() not in ("0", "false", "no")


class CheckpointStore(Protocol):
    """读写都不抛：未命中与失败一律返回 None / 静默。"""

    kind: str

    def get_json(self, key: str) -> dict[str, Any] | None: ...

    def put_json(self, key: str, payload: dict[str, Any]) -> None: ...


class LocalCheckpointStore:
    """落本地目录。开发机与 studio 用；容器里扛不住重启，仅作为无 blob 时的退路。"""

    kind = "local"

    def __init__(self, root: Path):
        self.root = root

    def _path(self, key: str) -> Path:
        # 两级分桶：一个数据集跑下来检查点数量可观，平铺会让目录列举变慢。
        return self.root / STORE_VERSION / key[:2] / f"{key}.json"

    def get_json(self, key: str) -> dict[str, Any] | None:
        path = self._path(key)
        try:
            if not path.exists():
                return None
            payload = json.loads(path.read_text(encoding="utf-8"))
        except Exception:  # noqa: BLE001 - 坏文件等同于没有
            logger.warning("检查点读取失败，按未命中处理：%s", path, exc_info=True)
            return None
        return payload if isinstance(payload, dict) else None

    def put_json(self, key: str, payload: dict[str, Any]) -> None:
        path = self._path(key)
        try:
            path.parent.mkdir(parents=True, exist_ok=True)
            # 先写临时文件再原子改名：批量模式下多个 case 并发，
            # 直接写目标文件会让并发的读方看到半截 JSON。
            tmp = path.with_suffix(f".{os.getpid()}.tmp")
            tmp.write_text(json.dumps(payload, ensure_ascii=False), encoding="utf-8")
            os.replace(tmp, path)
        except Exception:  # noqa: BLE001 - 写失败只是少一次复用
            logger.warning("检查点写入失败，已跳过：%s", path, exc_info=True)


class BlobCheckpointStore:
    """落对象存储。容器里唯一跨重启的选择。"""

    kind = "blob"

    def __init__(self, store: Any):
        self._store = store

    def _key(self, key: str) -> str:
        return f"{_BLOB_PREFIX}/{STORE_VERSION}/{key[:2]}/{key}.json"

    def get_json(self, key: str) -> dict[str, Any] | None:
        try:
            data = self._store.get_bytes(self._key(key))
        except Exception:  # noqa: BLE001 - 存储不可用等同于未命中
            logger.warning("检查点读取失败（对象存储），按未命中处理", exc_info=True)
            return None
        if not data:
            return None
        try:
            payload = json.loads(data.decode("utf-8"))
        except Exception:  # noqa: BLE001
            logger.warning("检查点内容损坏，按未命中处理", exc_info=True)
            return None
        return payload if isinstance(payload, dict) else None

    def put_json(self, key: str, payload: dict[str, Any]) -> None:
        try:
            body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
            self._store.put_at(self._key(key), body, content_type="application/json")
        except Exception:  # noqa: BLE001
            logger.warning("检查点写入失败（对象存储），已跳过", exc_info=True)


def shared_checkpoint_store() -> CheckpointStore | None:
    """按环境挑后端；关掉或都不可用时返回 None。"""
    if not checkpoints_enabled():
        return None

    from aigc_eval.providers.blob_store import BlobStore, load_blob_config

    if load_blob_config().configured:
        try:
            return BlobCheckpointStore(BlobStore())
        except Exception:  # noqa: BLE001 - 配了但建不起来，退回本地
            logger.warning("对象存储不可用，检查点退回本地目录", exc_info=True)

    raw = os.environ.get("AIGC_EVAL_CHECKPOINT_DIR", "").strip()
    root = Path(raw) if raw else RUNTIME_ROOT / "checkpoints"
    return LocalCheckpointStore(root)


def wrap_payload(payload: dict[str, Any], *, meta: dict[str, Any]) -> dict[str, Any]:
    """存进去的信封。

    ``result`` 之外还存元信息，因为检查点是跨进程、跨版本被读到的：出问题时要能回答
    "这条是谁在什么时候按哪个 stage 写的"，只存结果会让排查无从下手。
    """
    return {"stored_at": int(time.time()), "meta": meta, "result": payload}


def unwrap_payload(envelope: dict[str, Any]) -> dict[str, Any] | None:
    result = envelope.get("result")
    return result if isinstance(result, dict) else None
