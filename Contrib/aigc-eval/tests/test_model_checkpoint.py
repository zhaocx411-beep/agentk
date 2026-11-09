"""模型调用级检查点的离线测试。

检查点存在的理由：质检整体是一个 Temporal activity，崩溃或容器重启后没有中间点
可续，重跑要把四到五次模型调用的钱再花一遍。所以这里验的每一条都指向"钱"和"正确性"
两件事——省钱的前提是命中，而命中错了比不命中糟得多。

七件最容易出错、且出错代价最高的事：

1. 同一个请求第二次不再调模型（省钱的本体）；
2. 请求任何一处变了就必须未命中——素材、提示词、stage、模型、温度、输出上限；
3. ``refresh=True`` 跳过读、照常写（用户要重判，不能把上次答案端出来）；
4. 失败与未收敛的响应不写检查点（否则把一次坏结果固化下来）；
5. 存储挂掉时质检照常跑完（检查点是旁路，不能成为新的失败源）；
6. 命中时补齐 trace 与成本记录，且标明未计费（审计链不能有空洞）；
7. 未知属性透传给内层 client（编排会读 ``max_media`` 做素材预算）。

直接 ``uv run python tests/test_model_checkpoint.py`` 运行，不联网、不碰模型。
"""

from __future__ import annotations

import json
import os
import sys
import tempfile
from pathlib import Path
from typing import Any

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

os.environ.setdefault("JUDGE_CHECKPOINT", "1")

from aigc_eval.judge.checkpoint_store import (  # noqa: E402
    BlobCheckpointStore,
    LocalCheckpointStore,
    STORE_VERSION,
    checkpoints_enabled,
)
from aigc_eval.judge.model_checkpoint import (  # noqa: E402
    CheckpointedClient,
    checkpointed,
    request_digest,
)

FAILURES: list[str] = []


def check(label: str, ok: bool, detail: object = "") -> None:
    print(f"{'PASS' if ok else 'FAIL'}  {label}" + (f" — {detail}" if not ok else ""))
    if not ok:
        FAILURES.append(label)


class FakeConfig:
    def __init__(self, model: str = "fake-model-v1"):
        self.model = model


class FakeTrace:
    def __init__(self) -> None:
        self.steps: list[dict[str, Any]] = []

    def record_step(self, **kwargs: Any) -> None:
        self.steps.append(kwargs)


class FakeClient:
    """最小的 provider 替身：签名与三个真 provider 一致（鸭子类型）。"""

    def __init__(self, answer: dict[str, Any] | None = None, model: str = "fake-model-v1"):
        self.config = FakeConfig(model)
        self.trace = FakeTrace()
        self.usage: list[dict[str, Any]] = []
        self.calls: list[dict[str, Any]] = []
        self.max_media = 10  # 编排读这个做素材预算，用来验属性透传
        self._answer = answer or {"verdict": "ok", "n": 1}

    def generate_json(
        self,
        parts: list[dict[str, Any]],
        *,
        system_instruction: str | None = None,
        temperature: float = 0.1,
        stage: str = "fake_call",
        title: str = "fake",
        max_attempts: int = 4,
        max_output_tokens: int | None = None,
    ) -> dict[str, Any]:
        self.calls.append({"stage": stage, "parts": parts})
        self.usage.append({"stage": stage, "ok": True, "prompt_tokens": 1000})
        return dict(self._answer)


class ExplodingStore:
    """读写都炸的存储，用来验"检查点不能成为新的失败源"。"""

    kind = "exploding"

    def get_json(self, key: str) -> dict[str, Any] | None:
        raise RuntimeError("存储不可用")

    def put_json(self, key: str, payload: dict[str, Any]) -> None:
        raise RuntimeError("存储不可用")


class FakeBlob:
    """BlobStore 的替身：只记 key 与字节，不发 HTTP。"""

    def __init__(self) -> None:
        self.objects: dict[str, bytes] = {}
        self.gets: list[str] = []

    def put_at(self, key: str, data: bytes, *, content_type: str) -> str:
        self.objects[key] = data
        return f"https://blob.example/{key}"

    def get_bytes(self, key: str) -> bytes | None:
        self.gets.append(key)
        return self.objects.get(key)


PARTS = [
    {"text": "判定这段视频是否符合提示词"},
    {"inline_data": {"mime_type": "image/jpeg", "data": "AAAABBBBCCCC"}},
]


def wrap(inner: FakeClient, store: Any, **kwargs: Any) -> CheckpointedClient:
    return CheckpointedClient(inner, store=store, provider="fake", **kwargs)


def main() -> int:
    with tempfile.TemporaryDirectory() as tmp:
        store = LocalCheckpointStore(Path(tmp))

        print("\n[1] 第二次同样的请求不再调模型")
        a = FakeClient()
        first = wrap(a, store).generate_json(PARTS, stage="native8_scan", title="4b 主判")
        b = FakeClient()
        proxy_b = wrap(b, store)
        second = proxy_b.generate_json(PARTS, stage="native8_scan", title="4b 主判")
        check("首次真调模型", len(a.calls) == 1, a.calls)
        check("二次零模型调用", len(b.calls) == 0, b.calls)
        check("两次结果一致", first == second, (first, second))
        check("命中计数", proxy_b.checkpoint_stats()["hits"] == 1, proxy_b.checkpoint_stats())

        print("\n[2] 请求任何一处变了都必须未命中")
        variants = {
            "素材字节变了": dict(
                parts=[
                    PARTS[0],
                    {"inline_data": {"mime_type": "image/jpeg", "data": "AAAABBBBCCCD"}},
                ],
                stage="native8_scan",
            ),
            "提示词变了": dict(
                parts=[{"text": "换一句"}, PARTS[1]], stage="native8_scan"
            ),
            "stage 变了": dict(parts=PARTS, stage="observer"),
            "温度变了": dict(parts=PARTS, stage="native8_scan", temperature=0.9),
            "输出上限变了": dict(parts=PARTS, stage="native8_scan", max_output_tokens=65536),
            "system 变了": dict(
                parts=PARTS, stage="native8_scan", system_instruction="你是质检员"
            ),
        }
        for label, kwargs in variants.items():
            fresh = FakeClient()
            wrap(fresh, store).generate_json(**kwargs)  # type: ignore[arg-type]
            check(f"{label} → 未命中", len(fresh.calls) == 1, fresh.calls)

        换模型 = FakeClient(model="fake-model-v2")
        wrap(换模型, store).generate_json(PARTS, stage="native8_scan")
        check("换模型 → 未命中", len(换模型.calls) == 1, 换模型.calls)

        print("\n[3] refresh 跳过读、照常写")
        c = FakeClient(answer={"verdict": "refreshed"})
        proxy_c = wrap(c, store, refresh=True)
        out = proxy_c.generate_json(PARTS, stage="native8_scan", title="4b 主判")
        check("refresh 时仍调模型", len(c.calls) == 1, c.calls)
        d = FakeClient()
        wrap(d, store).generate_json(PARTS, stage="native8_scan", title="4b 主判")
        check("refresh 的结果被写入并可复用", len(d.calls) == 0 and out["verdict"] == "refreshed")

        print("\n[4] 坏结果不写检查点")
        unfinished = FakeClient(answer={"_transcript": "半截", "_unfinished": True})
        wrap(unfinished, store).generate_json(PARTS, stage="unfinished_stage")
        again = FakeClient(answer={"_transcript": "半截", "_unfinished": True})
        wrap(again, store).generate_json(PARTS, stage="unfinished_stage")
        check("未收敛响应不复用", len(again.calls) == 1, again.calls)

        class Boom(FakeClient):
            def generate_json(self, *args: Any, **kwargs: Any) -> dict[str, Any]:
                raise RuntimeError("模型 500")

        boom = Boom()
        try:
            wrap(boom, store).generate_json(PARTS, stage="boom_stage")
            check("失败原样抛出", False, "没抛异常")
        except RuntimeError:
            check("失败原样抛出", True)
        retry = FakeClient()
        wrap(retry, store).generate_json(PARTS, stage="boom_stage")
        check("失败不写检查点", len(retry.calls) == 1, retry.calls)

        print("\n[5] 存储挂掉不影响质检")
        e = FakeClient()
        proxy_e = wrap(e, ExplodingStore())
        try:
            got = proxy_e.generate_json(PARTS, stage="native8_scan")
            check("读写都炸时照常返回结果", got == {"verdict": "ok", "n": 1}, got)
        except Exception as err:  # noqa: BLE001
            check("读写都炸时照常返回结果", False, err)

        print("\n[6] 命中时审计链不留空洞")
        f = FakeClient()
        proxy_f = wrap(f, store)
        proxy_f.generate_json(PARTS, stage="native8_scan", title="4b 主判")
        usage = f.usage[-1] if f.usage else {}
        trace_step = f.trace.steps[-1] if f.trace.steps else {}
        check("补了成本记录", usage.get("from_checkpoint") is True, usage)
        check("成本记录不带 token（未计费）", "prompt_tokens" not in usage, usage)
        check("补了 trace 步骤", bool(trace_step), trace_step)
        check(
            "trace 标明来自检查点",
            "检查点" in str(trace_step.get("title", "")),
            trace_step.get("title"),
        )

        print("\n[7] 未知属性透传给内层")
        g = FakeClient()
        proxy_g = wrap(g, store)
        check("max_media 透传", proxy_g.max_media == 10, proxy_g.max_media)
        check("config 透传", proxy_g.config.model == "fake-model-v1", proxy_g.config.model)

        print("\n[8] 本地存储：坏文件按未命中、写入是原子的")
        key = request_digest(
            provider="fake",
            model="fake-model-v1",
            stage="native8_scan",
            parts=PARTS,
            system_instruction=None,
            temperature=0.1,
            max_output_tokens=None,
        )
        path = Path(tmp) / STORE_VERSION / key[:2] / f"{key}.json"
        check("检查点确实落盘", path.exists(), path)
        path.write_text("{半截 JSON", encoding="utf-8")
        h = FakeClient()
        wrap(h, store).generate_json(PARTS, stage="native8_scan")
        check("坏文件按未命中处理", len(h.calls) == 1, h.calls)
        check("没有残留临时文件", not list(path.parent.glob("*.tmp")), list(path.parent.glob("*.tmp")))

    print("\n[9] blob 后端：键带版本与分桶，往返一致")
    blob = FakeBlob()
    blob_store = BlobCheckpointStore(blob)
    i = FakeClient()
    wrap(i, blob_store).generate_json(PARTS, stage="native8_scan")
    j = FakeClient()
    wrap(j, blob_store).generate_json(PARTS, stage="native8_scan")
    check("blob 二次零模型调用", len(j.calls) == 0, j.calls)
    stored_key = next(iter(blob.objects), "")
    check("键带前缀与版本", stored_key.startswith(f"aigc-eval/checkpoints/{STORE_VERSION}/"), stored_key)
    check("键以 .json 结尾", stored_key.endswith(".json"), stored_key)
    envelope = json.loads(blob.objects[stored_key].decode("utf-8"))
    check(
        "信封带 meta 便于排查",
        envelope.get("meta", {}).get("stage") == "native8_scan",
        envelope.get("meta"),
    )

    print("\n[10] 开关：JUDGE_CHECKPOINT=0 时完全不介入")
    os.environ["JUDGE_CHECKPOINT"] = "0"
    try:
        check("开关关掉后 checkpoints_enabled 为假", not checkpoints_enabled())
        raw = FakeClient()
        check("关掉后原样返回内层 client", checkpointed(raw, provider="fake") is raw)
    finally:
        os.environ["JUDGE_CHECKPOINT"] = "1"

    print("\n[11] pipeline 真的把 client 包了检查点")
    # 这条是防回归的哨兵：模块导入正确但忘了包，表现是"功能静默失效、
    # 每次重跑照样烧钱"，没有任何报错能提示。
    import inspect

    import aigc_eval.judge.pipeline as jp

    # client 的构造统一走 build_live_client（worker 侧每个 activity 也用它），
    # 所以检查点的包装点只有一处；这里就盯这一处。
    check(
        "build_live_client 里包了 checkpointed",
        "checkpointed(" in inspect.getsource(jp.build_live_client),
    )
    check(
        "analyze_case 经 build_live_client 建 client",
        "build_live_client(" in inspect.getsource(jp.analyze_case),
    )
    check("导出的是同一个函数", jp.checkpointed.__module__.endswith("model_checkpoint"))

    if FAILURES:
        print(f"\nFAILED: {len(FAILURES)} 项 — {FAILURES}")
        return 1
    print("\nALL PASSED")
    return 0


if __name__ == "__main__":
    sys.exit(main())
