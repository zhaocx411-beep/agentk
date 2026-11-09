"""阶段产物共享存储的离线测试。

产物层存在的理由：质检拆成多个 Temporal activity 后，各阶段可能落在不同实例上，
前一阶段写在容器本地盘的帧图，后一阶段看不到。所以这里验的每一条都指向同一个问题
——**换了机器还能不能照原样跑完**。

八件最容易出错、且出错代价最高的事：

1. 上传返回的 manifest 能定位到内容（path/key/sha256/size 齐全）；
2. 同一份字节在一次 run 里不重复上传（一次质检里帧图会被三个阶段反复引用）；
3. 已在对象存储上的内容不再重传（跨 run 复用）；
4. 本地文件还在且大小一致时不产生任何网络往返（同机重跑是最常见情况）；
5. 本地文件被清掉后能按原路径还原，且字节完全一致（换实例续跑的本体）；
6. 本地文件被截断/写坏时会重新拉取，而不是将错就错；
7. 拉不回来时：required=True 必须抛错（模型判定缺帧会静默漏判），
   required=False 只计缺失（HTML 少几张图不该阻断已成立的结论）；
8. 没配对象存储时整层是空操作，单机跑法完全不受影响。

外加一条防回归：pipeline 的每个阶段函数都得接 store/artifacts，
免得以后重构把接线弄丢了却没人发现。

直接 ``uv run python tests/test_artifact_store.py`` 运行，不联网、不碰对象存储。
"""

from __future__ import annotations

import hashlib
import inspect
import os
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

from aigc_eval.judge.artifact_store import (  # noqa: E402
    ArtifactMissing,
    ArtifactStore,
    artifacts_enabled,
    materialize,
    put_files,
    shared_artifact_store,
)
from aigc_eval.paths import RUNTIME_ROOT  # noqa: E402

FAILURES: list[str] = []


def check(label: str, ok: bool, detail: object = "") -> None:
    print(f"{'PASS' if ok else 'FAIL'}  {label}" + (f" — {detail}" if not ok else ""))
    if not ok:
        FAILURES.append(label)


class FakeBlob:
    """对象存储替身：记下每次 PUT/HEAD/GET，好断言"有没有多传一次"。"""

    def __init__(self) -> None:
        self.objects: dict[str, bytes] = {}
        self.puts: list[str] = []
        self.heads: list[str] = []
        self.gets: list[str] = []
        self.fail_put = False
        self.fail_get = False

    def exists(self, key: str) -> bool:
        self.heads.append(key)
        return key in self.objects

    def put_at(self, key: str, data: bytes, *, content_type: str) -> str:
        if self.fail_put:
            raise RuntimeError("上传失败（测试注入）")
        self.puts.append(key)
        self.objects[key] = data
        return f"https://blob.test/{key}"

    def get_bytes(self, key: str) -> bytes | None:
        if self.fail_get:
            raise RuntimeError("读取失败（测试注入）")
        self.gets.append(key)
        return self.objects.get(key)


# 产物必须落在 RUNTIME_ROOT 内（manifest 记的是相对路径），所以测试也用真 RUNTIME_ROOT
# 下的独立子目录，跑完清掉。
SANDBOX = RUNTIME_ROOT / "work" / "_artifact_test"


def write(name: str, data: bytes) -> Path:
    path = SANDBOX / name
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(data)
    return path


def cleanup() -> None:
    if not SANDBOX.exists():
        return
    for path in sorted(SANDBOX.rglob("*"), reverse=True):
        if path.is_file():
            path.unlink()
        else:
            path.rmdir()
    SANDBOX.rmdir()


def main() -> int:
    cleanup()
    frame_a = write("frames/frame_001.jpg", b"AAAA" * 32)
    frame_b = write("frames/frame_002.jpg", b"BBBB" * 32)
    dup = write("frames/frame_003.jpg", b"AAAA" * 32)  # 与 frame_a 同字节

    print("\n[1] manifest：能定位到内容")
    blob = FakeBlob()
    store = ArtifactStore(blob)
    manifest = store.put_files([frame_a, frame_b])
    check("两个文件都进 manifest", len(manifest) == 2, manifest)
    entry = manifest[0]
    check(
        "条目字段齐全",
        {"path", "key", "sha256", "size"} <= set(entry),
        sorted(entry),
    )
    check(
        "path 是相对 RUNTIME_ROOT 的路径",
        entry["path"] == "work/_artifact_test/frames/frame_001.jpg",
        entry["path"],
    )
    check(
        "sha256 与文件内容一致",
        entry["sha256"] == hashlib.sha256(frame_a.read_bytes()).hexdigest(),
    )
    check("size 与文件大小一致", entry["size"] == frame_a.stat().st_size)

    print("\n[2] 同一份字节不重复上传")
    before = len(blob.puts)
    store.put_files([dup])
    check("同字节不再 PUT", len(blob.puts) == before, blob.puts)
    check(
        "但仍给出可定位的 manifest 条目",
        store.put_files([dup])[0]["key"] == entry["key"],
    )

    print("\n[3] 已在对象存储上的内容不重传（跨 run）")
    fresh = ArtifactStore(blob)  # 新进程/新实例：进程内备忘是空的
    puts_before = len(blob.puts)
    fresh.put_files([frame_a])
    check("HEAD 命中后不 PUT", len(blob.puts) == puts_before, blob.puts[-3:])
    check("确实做了 HEAD 判定", blob.heads[-1] == entry["key"])

    print("\n[4] 本地文件还在：不产生网络往返")
    gets_before = len(blob.gets)
    stats = store.materialize(manifest)
    check("全部按已在本地处理", stats["present"] == 2, stats)
    check("没有 GET", len(blob.gets) == gets_before, blob.gets)

    print("\n[5] 换实例：本地清掉后按原路径还原")
    original = frame_a.read_bytes()
    frame_a.unlink()
    frame_b.unlink()
    stats = store.materialize(manifest)
    check("两个文件都还原了", stats["restored"] == 2, stats)
    check("路径与产出时一致", frame_a.exists() and frame_b.exists())
    check("字节完全一致", frame_a.read_bytes() == original)

    print("\n[6] 本地文件被写坏：重新拉取而不是将错就错")
    frame_a.write_bytes(b"broken")
    stats = store.materialize(manifest)
    check("大小不符触发重新拉取", stats["restored"] >= 1, stats)
    check("内容已修复", frame_a.read_bytes() == original)

    print("\n[7] 拉不回来：required 决定抛错还是记缺失")
    ghost = [{"path": "work/_artifact_test/frames/ghost.jpg", "key": "nope", "size": 9}]
    stats = store.materialize(ghost)
    check("required=False 只记缺失", stats["missing"] == 1, stats)
    raised = False
    try:
        store.materialize(ghost, required=True)
    except ArtifactMissing:
        raised = True
    check("required=True 抛 ArtifactMissing", raised)

    blob.fail_get = True
    stats = store.materialize(ghost)
    check("存储读失败也按缺失处理，不外抛", stats["missing"] == 1, stats)
    blob.fail_get = False

    print("\n[8] 上传失败不外抛，条目不进 manifest")
    blob.fail_put = True
    lone = write("frames/frame_009.jpg", b"CCCC" * 8)
    check("失败文件不进 manifest", ArtifactStore(blob).put_files([lone]) == [])
    blob.fail_put = False

    print("\n[9] RUNTIME_ROOT 之外的文件被跳过")
    outside = Path(RUNTIME_ROOT).parent / "pyproject.toml"
    check(
        "外部文件不入 manifest",
        outside.exists() and store.put_files([outside]) == [],
    )

    print("\n[10] 没配对象存储：整层空操作")
    check("put_files(None) 返回空", put_files(None, [frame_a]) == [])
    check(
        "materialize(None) 是空操作",
        materialize(None, manifest) == {"present": 0, "restored": 0, "missing": 0},
    )
    check(
        "manifest 为空时也不动作",
        materialize(store, []) == {"present": 0, "restored": 0, "missing": 0},
    )

    print("\n[11] 开关与自动选型")
    os.environ["JUDGE_ARTIFACT_STORE"] = "0"
    check("显式关掉后 enabled 为假", not artifacts_enabled())
    check("关掉后不返回 store", shared_artifact_store() is None)
    os.environ["JUDGE_ARTIFACT_STORE"] = "1"
    check("默认开", artifacts_enabled())
    os.environ.pop("AIGC_EVAL_BLOB_ENDPOINT", None)
    os.environ.pop("AIGC_EVAL_BLOB_BUCKET", None)
    check("没配 blob 时返回 None", shared_artifact_store() is None)

    print("\n[12] 防回归：每个阶段函数都接产物层")
    from aigc_eval.judge import pipeline

    for name in (
        "_stage_prepare",
        "_stage_compile",
        "_stage_observe",
        "_stage_scan",
        "_stage_recheck",
        "_stage_evidence",
        "_stage_report",
    ):
        params = set(inspect.signature(getattr(pipeline, name)).parameters)
        need = {"store"} if name == "_stage_prepare" else {"store", "artifacts"}
        check(f"{name} 接了 {'/'.join(sorted(need))}", need <= params, sorted(params))

    src = Path(pipeline.__file__).read_text(encoding="utf-8")
    check(
        "analyze_case 建了产物 store",
        "art_store = shared_artifact_store()" in src,
    )
    check(
        "manifest 在阶段之间累积",
        src.count("artifacts.update(") >= 3,
        src.count("artifacts.update("),
    )

    cleanup()
    print()
    if FAILURES:
        print("FAILED:", ", ".join(FAILURES))
        return 1
    print("ALL PASSED")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
