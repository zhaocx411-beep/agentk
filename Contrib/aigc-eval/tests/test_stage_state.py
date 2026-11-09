"""阶段状态层的单测：worker/state.py。

这一层是"下一个阶段有没有输入"的唯一依赖，所以它的失败必须**响**——与产物层和检查点
（丢了只是变慢/变贵）不同，状态丢了继续跑只会产出一份基于空状态的错误报告。

覆盖：
1. 两种后端都能原样往返，且换一个 store 实例照样读得到（跨实例就是这个形状）；
2. 每个阶段写自己的键、不覆盖别人的——重试幂等靠这个；
3. 读不到 / 内容损坏 / 写不进去，一律抛 StateUnavailable，不静默返回空；
4. CaseFiles 与抽帧产出的 Path 能安全过一次 JSON；
5. 用量与检查点统计是累加的，不是覆盖的。

运行：``uv run python tests/test_stage_state.py``
"""

from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Any

REPO = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPO / "src"))
sys.path.insert(0, str(REPO / "worker"))

from state import (  # noqa: E402
    StateStore,
    StateUnavailable,
    UsageCarrier,
    deserialize_case_files,
    deserialize_media_bundle,
    merge_checkpoint_stats,
    serialize_case_files,
    serialize_media_bundle,
)

FAILURES: list[str] = []


def check(label: str, ok: bool, detail: object = "") -> None:
    print(f"{'PASS' if ok else 'FAIL'}  {label}" + (f" — {detail}" if not ok else ""))
    if not ok:
        FAILURES.append(label)


def expect_raises(label: str, exc: type[BaseException], fn: Any) -> None:
    try:
        fn()
    except exc:
        check(label, True)
        return
    except Exception as other:  # noqa: BLE001
        check(label, False, f"抛的是 {type(other).__name__}")
        return
    check(label, False, "没抛异常")


class MemoryBlob:
    """内存对象存储。只实现状态层用到的两个方法。"""

    def __init__(self) -> None:
        self.objects: dict[str, bytes] = {}
        self.puts: list[str] = []

    def put_at(self, key: str, data: bytes, *, content_type: str) -> str:
        self.objects[key] = data
        self.puts.append(key)
        return key

    def get_bytes(self, key: str) -> bytes | None:
        return self.objects.get(key)


class BrokenBlob(MemoryBlob):
    def put_at(self, key: str, data: bytes, *, content_type: str) -> str:
        raise OSError("模拟对象存储挂了")


def main() -> int:
    import tempfile

    tmp = Path(tempfile.mkdtemp(prefix="stage-state-"))
    blob = MemoryBlob()
    backends = {
        "local": StateStore(None, tmp),
        "blob": StateStore(blob),
    }

    print("\n[1] 两种后端都能原样往返")
    payload = {"a": 1, "深": ["嵌", {"套": None}], "float": 1.5}
    for kind, store in backends.items():
        store.put("run-1", "prepare", payload)
        check(f"  {kind} 往返一致", store.get("run-1", "prepare") == payload)
    check("blob 后端 kind 标对", backends["blob"].kind == "blob")
    check("local 后端 kind 标对", backends["local"].kind == "local")

    print("\n[2] 换一个 store 实例照样读得到（跨实例就是这个形状）")
    check(
        "  local 跨实例",
        StateStore(None, tmp).get("run-1", "prepare") == payload,
    )
    check(
        "  blob 跨实例",
        StateStore(blob).get("run-1", "prepare") == payload,
    )

    print("\n[3] 每个阶段写自己的键，不覆盖别人的")
    for kind, store in backends.items():
        store.put("run-1", "scan", {"stage": "scan"})
        ok = (
            store.get("run-1", "prepare") == payload
            and store.get("run-1", "scan") == {"stage": "scan"}
        )
        check(f"  {kind} 两个键并存", ok)
    # 同一阶段重跑：原样重写自己的键，读到的仍是最新一次。
    backends["blob"].put("run-1", "scan", {"stage": "scan", "attempt": 2})
    check(
        "  重试重写自己的键",
        backends["blob"].get("run-1", "scan")["attempt"] == 2,
    )

    print("\n[4] run 之间互不干扰")
    backends["blob"].put("run-2", "prepare", {"other": True})
    check("  run-1 未被串改", backends["blob"].get("run-1", "prepare") == payload)

    print("\n[5] 失败必须响，不能静默返回空")
    for kind, store in backends.items():
        expect_raises(f"  {kind} 读不到就抛", StateUnavailable, lambda s=store: s.get("run-x", "prepare"))
    blob.objects["aigc-eval/worker-state/st1/run-3/prepare.json"] = b"{not json"
    expect_raises(
        "  内容损坏就抛",
        StateUnavailable,
        lambda: StateStore(blob).get("run-3", "prepare"),
    )
    blob.objects["aigc-eval/worker-state/st1/run-4/prepare.json"] = b"[1,2]"
    expect_raises(
        "  不是对象就抛",
        StateUnavailable,
        lambda: StateStore(blob).get("run-4", "prepare"),
    )
    expect_raises(
        "  写不进去就抛",
        StateUnavailable,
        lambda: StateStore(BrokenBlob()).put("run-5", "prepare", {"a": 1}),
    )

    print("\n[6] CaseFiles 过一次 JSON 不变形")
    from aigc_eval.judge.cases import CaseFiles

    case_files = CaseFiles(
        case_id="up_deadbeef",
        collection="uploads",
        case_name="up_deadbeef",
        case_dir=tmp / "case",
        prompt_path=tmp / "case" / "prompt.json",
        video_path=tmp / "case" / "assets" / "v.mp4",
        seedance_path=None,
        input_image_path=tmp / "case" / "assets" / "ref.png",
        reference_assets=[{"reference_id": "Image 1", "kind": "image", "path": "x.png"}],
    )
    case_files.slug = "up_deadbeef"  # 内容寻址值，不能被构造函数按 case_id 覆盖
    revived = deserialize_case_files(
        json.loads(json.dumps(serialize_case_files(case_files)))
    )
    for field in ("case_id", "collection", "case_name", "slug"):
        check(f"  {field} 一致", getattr(revived, field) == getattr(case_files, field))
    check("  video_path 仍是 Path", isinstance(revived.video_path, Path))
    check("  video_path 值一致", revived.video_path == case_files.video_path)
    check("  seedance_path 保持 None", revived.seedance_path is None)
    check("  input_image_path 一致", revived.input_image_path == case_files.input_image_path)
    check("  reference_assets 一致", revived.reference_assets == case_files.reference_assets)

    print("\n[7] 抽帧产出里的 Path 能安全过 JSON")
    bundle = {
        "frames": [tmp / "f1.jpg", tmp / "f2.jpg"],
        "frame_meta": [{"path": tmp / "f1.jpg", "time": 0.5}],
        "contact_path": tmp / "contact.jpg",
        "contact_sheet_url": "./runtime/x.jpg",
        "fps": 2,
    }
    revived_bundle = deserialize_media_bundle(
        json.loads(json.dumps(serialize_media_bundle(bundle)))
    )
    check("  frames 仍是 Path", all(isinstance(p, Path) for p in revived_bundle["frames"]))
    check("  frames 值一致", revived_bundle["frames"] == bundle["frames"])
    check("  frame_meta path 仍是 Path", isinstance(revived_bundle["frame_meta"][0]["path"], Path))
    check("  frame_meta 其他字段保留", revived_bundle["frame_meta"][0]["time"] == 0.5)
    check("  contact_path 仍是 Path", isinstance(revived_bundle["contact_path"], Path))
    check("  非 Path 字段原样", revived_bundle["fps"] == 2)

    print("\n[8] 用量与检查点是累加的")
    carrier = UsageCarrier([{"stage": "a"}, {"stage": "b"}], {"hits": 3, "misses": 1})
    check("  用量端出来", len(carrier.usage) == 2)
    check("  检查点端出来", carrier.checkpoint_stats() == {"hits": 3, "misses": 1})
    merged = merge_checkpoint_stats(
        {"store": "blob", "hits": 2, "misses": 1}, {"store": "blob", "hits": 3, "misses": 4}
    )
    check("  hits 相加", merged["hits"] == 5, merged)
    check("  misses 相加", merged["misses"] == 5, merged)
    check("  空的一边不清零", merge_checkpoint_stats({"hits": 2}, None) == {"hits": 2})
    check("  从零开始", merge_checkpoint_stats(None, {"hits": 1}) == {"hits": 1})

    print()
    if FAILURES:
        print(f"FAILED: {len(FAILURES)} 项 — {FAILURES}")
        return 1
    print("ALL PASSED")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
