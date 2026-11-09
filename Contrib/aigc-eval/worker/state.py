"""阶段状态在 activity 之间的传递方式。

质检拆成一串 Temporal activity 后，每个 activity 都要拿到"前面跑出了什么"。有两种
传法，这里选了后者：

* **随 activity 入出参走 Temporal 历史**。最省事，但 Temporal 单个 payload 有 2MB
  硬上限，而 4b 的事实底稿（28 条子判据 + 原子 issue + 帧记录）加上观察器事实，长素材
  上很容易顶到这个量级；一旦超限就是整次质检失败，而且失败点随素材长度漂移。
* **状态存对象存储，历史里只走一个 key**。多一次读写，换来 payload 恒定在几百字节，
  且状态大小不再是个隐藏的失败源。产物层本来就要求配对象存储，不引入新依赖。

key 的构成是 ``<run>/<stage>.json``：每个阶段**写自己的键、读上一阶段的键**，从不覆盖
别人的。这样重试是天然幂等的——某个 activity 重跑时读到的输入与第一次完全一致，
而它自己那份输出被原样重写。

没配对象存储时退到本地目录（``runtime/worker_state``），单机联调用；那种情况下各
activity 本来就在同一台机器上。
"""

from __future__ import annotations

import json
import logging
import os
from pathlib import Path
from typing import Any

logger = logging.getLogger(__name__)

# 键里带版本号：状态结构变了就整体失效，免得新代码读到旧口径的状态。
STATE_VERSION = "st1"

_BLOB_PREFIX = "aigc-eval/worker-state"


class StateUnavailable(RuntimeError):
    """状态存不进去或读不出来。

    与产物层和检查点不同，这一层**必须**抛：状态丢了，下一个阶段就没有输入，
    继续跑只会得到一份基于空状态的错误报告。
    """


class StateStore:
    """阶段状态的读写。两种后端行为一致，只有落点不同。"""

    def __init__(self, blob: Any | None, local_root: Path | None = None):
        self._blob = blob
        self._root = local_root
        self.kind = "blob" if blob is not None else "local"

    def _key(self, run_key: str, stage: str) -> str:
        return f"{_BLOB_PREFIX}/{STATE_VERSION}/{run_key}/{stage}.json"

    def _path(self, run_key: str, stage: str) -> Path:
        assert self._root is not None
        return self._root / STATE_VERSION / run_key / f"{stage}.json"

    def put(self, run_key: str, stage: str, payload: dict[str, Any]) -> str:
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        if self._blob is not None:
            key = self._key(run_key, stage)
            try:
                self._blob.put_at(key, body, content_type="application/json")
            except Exception as exc:  # noqa: BLE001
                raise StateUnavailable(f"阶段状态写入失败：{key}") from exc
            return key
        path = self._path(run_key, stage)
        try:
            path.parent.mkdir(parents=True, exist_ok=True)
            tmp = path.with_suffix(f".{os.getpid()}.tmp")
            tmp.write_bytes(body)
            os.replace(tmp, path)
        except OSError as exc:
            raise StateUnavailable(f"阶段状态写入失败：{path}") from exc
        return str(path)

    def get(self, run_key: str, stage: str) -> dict[str, Any]:
        if self._blob is not None:
            key = self._key(run_key, stage)
            try:
                data = self._blob.get_bytes(key)
            except Exception as exc:  # noqa: BLE001
                raise StateUnavailable(f"阶段状态读取失败：{key}") from exc
            if not data:
                raise StateUnavailable(f"阶段状态不存在：{key}")
        else:
            path = self._path(run_key, stage)
            try:
                data = path.read_bytes()
            except OSError as exc:
                raise StateUnavailable(f"阶段状态不存在：{path}") from exc
        try:
            payload = json.loads(data.decode("utf-8"))
        except Exception as exc:  # noqa: BLE001
            raise StateUnavailable(f"阶段状态内容损坏：{stage}") from exc
        if not isinstance(payload, dict):
            raise StateUnavailable(f"阶段状态不是对象：{stage}")
        return payload


def shared_state_store() -> StateStore:
    """有对象存储就用它，否则退到本地目录。"""
    from aigc_eval.paths import RUNTIME_ROOT
    from aigc_eval.providers.blob_store import BlobStore, load_blob_config

    if load_blob_config().configured:
        try:
            return StateStore(BlobStore())
        except Exception:  # noqa: BLE001 - 配了但建不起来，退本地
            logger.warning("对象存储不可用，阶段状态退回本地目录", exc_info=True)
    return StateStore(None, RUNTIME_ROOT / "worker_state")


# ---------------------------------------------------------------------------
# CaseFiles 的序列化
#
# 不用 build_upload_case_files 重建：那个函数会重算内容哈希（要读整段视频）并重写
# prompt.json，跨 activity 每次都做一遍既慢又多余。这里按字段直接存取，slug 一并带上
# ——它是内容寻址值，不能让构造函数按 case_id 重新推导。
# ---------------------------------------------------------------------------


def serialize_case_files(case_files: Any) -> dict[str, Any]:
    return {
        "case_id": case_files.case_id,
        "collection": case_files.collection,
        "case_name": case_files.case_name,
        "slug": case_files.slug,
        "case_dir": str(case_files.case_dir),
        "prompt_path": str(case_files.prompt_path),
        "video_path": str(case_files.video_path),
        "seedance_path": str(case_files.seedance_path) if case_files.seedance_path else None,
        "input_image_path": (
            str(case_files.input_image_path) if case_files.input_image_path else None
        ),
        "reference_assets": list(case_files.reference_assets or []),
    }


def deserialize_case_files(state: dict[str, Any]) -> Any:
    from aigc_eval.judge.cases import CaseFiles

    case_files = CaseFiles(
        case_id=str(state["case_id"]),
        collection=str(state["collection"]),
        case_name=str(state["case_name"]),
        case_dir=Path(str(state["case_dir"])),
        prompt_path=Path(str(state["prompt_path"])),
        video_path=Path(str(state["video_path"])),
        seedance_path=Path(state["seedance_path"]) if state.get("seedance_path") else None,
        input_image_path=(
            Path(state["input_image_path"]) if state.get("input_image_path") else None
        ),
        reference_assets=list(state.get("reference_assets") or []),
    )
    case_files.slug = str(state["slug"])
    return case_files


# ---------------------------------------------------------------------------
# media_bundle 的序列化
#
# 抽帧产出里混着 Path 与 URL 字符串，JSON 化只需把 Path 转成字符串；还原时转回来，
# 因为下游（观察器、4b、HTML）全都按 Path 用。
# ---------------------------------------------------------------------------

_BUNDLE_PATH_KEYS = ("contact_path",)


def serialize_media_bundle(bundle: dict[str, Any]) -> dict[str, Any]:
    out = dict(bundle)
    out["frames"] = [str(path) for path in bundle.get("frames") or []]
    out["frame_meta"] = [
        {**row, "path": str(row.get("path"))}
        for row in bundle.get("frame_meta") or []
        if isinstance(row, dict)
    ]
    for key in _BUNDLE_PATH_KEYS:
        if bundle.get(key):
            out[key] = str(bundle[key])
    return out


def deserialize_media_bundle(state: dict[str, Any]) -> dict[str, Any]:
    out = dict(state)
    out["frames"] = [Path(str(path)) for path in state.get("frames") or []]
    out["frame_meta"] = [
        {**row, "path": Path(str(row.get("path")))}
        for row in state.get("frame_meta") or []
        if isinstance(row, dict)
    ]
    for key in _BUNDLE_PATH_KEYS:
        if state.get(key):
            out[key] = Path(str(state[key]))
    return out


class UsageCarrier:
    """报告阶段用的 client 替身：只带累计用量与检查点统计。

    报告组装要算成本（``client.usage``）和检查点命中（``client.checkpoint_stats()``），
    但那一步不调模型，没有真的 client。用量在各阶段跑完时汇进状态，这里原样端出来，
    成本与命中数因此仍是整次质检的合计值，而不是最后一段的。
    """

    supports_video = False

    def __init__(self, usage: list[dict[str, Any]], checkpoint: dict[str, Any] | None):
        self.usage = list(usage or [])
        self._checkpoint = checkpoint

    def checkpoint_stats(self) -> dict[str, Any] | None:
        return self._checkpoint


def merge_checkpoint_stats(
    total: dict[str, Any] | None, new: dict[str, Any] | None
) -> dict[str, Any] | None:
    """把各阶段的检查点统计相加。``store`` 各阶段一致，取后来者。"""
    if not new:
        return total
    if not total:
        return dict(new)
    merged = dict(total)
    for key in ("hits", "misses", "writes"):
        if isinstance(new.get(key), int):
            merged[key] = int(merged.get(key) or 0) + int(new[key])
    if new.get("store"):
        merged["store"] = new["store"]
    return merged
