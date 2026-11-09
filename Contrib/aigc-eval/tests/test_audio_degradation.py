"""音频静默降级的防线：模型没真听到时，不许留下有结论的音频判据。

这条防线针对的是平台的一个**静默**失败模式，2026-09-01 放开 understand_media 的音频
入口当天实测出来的：主模型上约一半的调用会在服务端把音频转成文字再交给模型。那种情况下
接口通、台词对、还带时间戳，接入层面完全看不出问题，但音色、语调、音质、配乐一概拿不到，
而模型往往仍会填出一份完整的音频结论——一份没有听觉依据的结论。

现在音频阶段固定走 audio_model（flash-lite，实测 8/8 拿到音频），所以这条防线平时不该
触发。它存在是因为触发条件不在我们手里：平台改一次行为就可能回来，而回来的时候没有任何
报错。真触发时记 not_observable，会有人去查；一份编出来的 pass 不会。

离线，不发任何调用。
"""

from __future__ import annotations

import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from aigc_eval.config import AUDIO_STAGES
from aigc_eval.judge.native8_recheck import (
    AUDIO_NOT_HEARD_REASON,
    AUDIO_RECHECK_KEYS,
    normalize_audio_recheck,
)
from aigc_eval.judge.pipeline import _compare_reference_audio
from aigc_eval.judge.rubric_prompts import AUDIO_RECHECK_SYSTEM, AUDIO_REFERENCE_SYSTEM

FAILURES: list[str] = []


def check(label: str, condition: bool, detail: object = "") -> None:
    print(f"  {'PASS' if condition else 'FAIL'}  {label}")
    if not condition:
        FAILURES.append(label)
        if detail != "":
            print(f"        {detail!r}")


# 一份**看起来完全正常**的结论：三项都有 verdict、有置信度、有时刻、有时长、有证据。
# 静默降级时模型给的就是这个样子，所以判别不能靠"结论是否完整"。
_PLAUSIBLE = {
    key: {
        "verdict": "pass",
        "confidence": "high",
        "time": "00:00.0-00:14.0",
        "duration_seconds": 0.0,
        "evidence": "全程音质清晰，无底噪与爆音",
    }
    for key in AUDIO_RECHECK_KEYS
}


class _FakeClient:
    """只回一份预设 JSON。记下 stage，用来确认阶段名传对了。"""

    max_media = 10
    supports_video = True

    def __init__(self, answer: dict) -> None:
        self.answer = answer
        self.stages: list[str] = []

    def generate_json(self, parts, *, system_instruction=None, stage="", title="", **_kw):
        self.stages.append(stage)
        self.parts = parts
        return self.answer


class _FakeCase:
    """够 _compare_reference_audio 用的最小 case：只需要 reference_assets。"""

    def __init__(self, audio: Path) -> None:
        self.reference_assets = [{"path": str(audio), "kind": "audio"}]


def _run_reference_compare(bundle: dict, answer: dict) -> _FakeClient:
    """在临时目录里造一段假音频与假视频，跑一次参考音频对照。

    音频与视频都只需"存在且非空"：_compare_reference_audio 不解码它们，读字节发生在
    上传那一层，而这里的 client 是假的。
    """
    client = _FakeClient(answer)
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        audio = root / "ref.mp3"
        audio.write_bytes(b"FAKE-MP3" * 64)
        video = root / "gen.mp4"
        video.write_bytes(b"FAKE-MP4" * 64)
        _compare_reference_audio(
            emit=lambda *_a, **_k: None,
            client=client,
            case_files=_FakeCase(audio),
            video_path=video,
            metadata={"has_audio": True},
            observations_bundle=bundle,
        )
    return client


def main() -> int:
    print("[1] 模型自报没听到：三项一律退成未核实")
    degraded = normalize_audio_recheck({"can_hear_audio": False, **_PLAUSIBLE})
    check("三项都在（不是丢掉，是明确记未核实）",
          set(degraded) == set(AUDIO_RECHECK_KEYS), sorted(degraded))
    check("三项都是 not_observable",
          all(item["verdict"] == "not_observable" for item in degraded.values()),
          {k: v["verdict"] for k, v in degraded.items()})
    check("原因写进了证据字段，报告里能看到为什么未核实",
          all(item["evidence"] == AUDIO_NOT_HEARD_REASON for item in degraded.values()))
    # 时长残留会让下游的三档聚合拿它去定档，等于把一个不存在的问题算进分数。
    check("时长清空，不参与后续定档",
          all(item["duration_seconds"] is None for item in degraded.values()))

    print("\n[2] 模型说听到了：结论照常收下")
    heard = normalize_audio_recheck({"can_hear_audio": True, **_PLAUSIBLE})
    check("三项保持原判",
          all(item["verdict"] == "pass" for item in heard.values()),
          {k: v["verdict"] for k, v in heard.items()})

    # 字段缺失与"没听到"必须区分。把缺失也当没听到，会在模型偶尔漏字段时整维清空——
    # 那是拿一个更常见的故障替掉一个更罕见的。
    print("\n[3] 字段缺失 ≠ 没听到")
    silent = normalize_audio_recheck(dict(_PLAUSIBLE))
    check("缺 can_hear_audio 时不触发降级",
          all(item["verdict"] == "pass" for item in silent.values()),
          {k: v["verdict"] for k, v in silent.items()})
    for falsy in ("", 0, None):
        got = normalize_audio_recheck({"can_hear_audio": falsy, **_PLAUSIBLE})
        check(f"can_hear_audio={falsy!r} 不当作 False（只认显式 False）",
              all(item["verdict"] == "pass" for item in got.values()),
              {k: v["verdict"] for k, v in got.items()})

    # 归一层认这个字段，提示词却没让模型输出它 —— 那这条防线永远不会触发，
    # 而且没有任何迹象。两处必须一起改，所以在这里钉住。
    print("\n[4] 提示词与归一层对得上")
    check("提示词要求输出 can_hear_audio",
          "can_hear_audio" in AUDIO_RECHECK_SYSTEM)
    check("提示词讲明了服务端转写这个坑（否则模型不知道何时该填 false）",
          "转成文字" in AUDIO_RECHECK_SYSTEM or "转写" in AUDIO_RECHECK_SYSTEM)

    print("\n[5] 参考音频对照阶段：同一道防线，两个字段")
    check("提示词要求两边都自报能否听到",
          "can_hear_reference" in AUDIO_REFERENCE_SYSTEM
          and "can_hear_video_audio" in AUDIO_REFERENCE_SYSTEM)
    # 这一阶段只产出事实、不产出判定。措辞里明确禁止下结论，否则它会和 4b 争夺
    # 同一维度的作者权，而合并冲突时没有任何规则可依。
    check("提示词明确不下判定（判定权归 4b）",
          "不下判定" in AUDIO_REFERENCE_SYSTEM or "不打分" in AUDIO_REFERENCE_SYSTEM)
    check("阶段名登记进 AUDIO_STAGES，否则它会用主模型跑",
          "audio_reference" in AUDIO_STAGES, sorted(AUDIO_STAGES))

    print("\n[6] 参考音频对照：没听到就不留事实")
    for deaf_field in ("can_hear_reference", "can_hear_video_audio"):
        bundle: dict = {}
        _run_reference_compare(bundle, {deaf_field: False, "same_voice": "yes",
                                        "summary": "音色完全一致"})
        check(f"{deaf_field}=False 时不写入对照事实",
              "audio_reference" not in bundle and not bundle.get("observer_facts"),
              bundle)
        check(f"{deaf_field}=False 时留下可查的原因",
              bundle.get("audio_reference_not_heard"), bundle)

    # 观察器已经发过 OBS-001/002 了，对照事实必须续着发，不能撞号也不能另起一套 ID：
    # 4b 的提示词告诉模型观察器事实由程序分配稳定的 OBS-* ID，回引时也用这套 ID。
    bundle = {
        "observer_facts": [
            {"fact_id": "OBS-001", "kind": "observation", "fact": "画面里有个人"},
            {"fact_id": "OBS-002", "kind": "focus", "fact": "第 3 秒看不清"},
        ]
    }
    _run_reference_compare(bundle, {"can_hear_reference": True, "can_hear_video_audio": True,
                                    "same_voice": "no", "speech_match": "参考说A，视频说B",
                                    "summary": "换了个人声"})
    facts = bundle.get("observer_facts") or []
    check("追加而不是覆盖既有事实", len(facts) == 3, len(facts))
    fact = facts[-1] if facts else {}
    check("ID 续着 OBS-* 往下发", fact.get("fact_id") == "OBS-003", fact.get("fact_id"))
    # kind 不能是 focus：focus 行会触发 4b 的强制逐条表态与审计校验，那套机制是为
    # "观察器看不清的时刻"设计的，而这里是一份已经查清的描述，供引用即可。
    check("kind 是 observation 而不是 focus", fact.get("kind") == "observation",
          fact.get("kind"))
    # subcriteria 是既有的路由机制：4b 按它把事实归到对应判据下。空的话事实进了提示词
    # 但没有任何东西指示它该落到哪，等于送到了却用不上。
    check("subcriteria 指向指令遵循与参考一致性",
          set(fact.get("subcriteria") or [])
          == {"instruction_requested_text_audio", "reference_input_fidelity"},
          fact.get("subcriteria"))
    # 4b 自己听不到独立音频。不在正文里点明这条事实的来源，它没有依据决定该不该采信
    # 一份关于音色的描述。
    check("正文点明这是能直接听到音频的模型给出的",
          "直接听到" in str(fact.get("fact") or ""), str(fact.get("fact"))[:120])
    check("正文带上了逐字对照内容",
          "参考说A，视频说B" in str(fact.get("fact") or ""), str(fact.get("fact"))[:200])
    # 行形状必须与 normalize_observer_facts 产出的一致，否则 4b 看到一条缺字段的记录。
    check("行形状与观察器事实一致",
          set(fact) == {"fact_id", "kind", "time", "fact", "confidence", "tags",
                        "subcriteria", "constraint_ids", "contradicted_constraint_ids"},
          sorted(fact))

    print()
    if FAILURES:
        print(f"FAILED: {len(FAILURES)} 项 — {FAILURES}")
        return 1
    print("ALL PASSED")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
