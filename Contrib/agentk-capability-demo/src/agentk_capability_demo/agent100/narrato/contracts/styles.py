"""解说风格预设。

同一段素材可以有完全不同的解说做法，而这些做法之间的差异不是"参数微调"，
是互相矛盾的创作取向：纪录片式伴随解说要避让原片对白、严禁提前说破剧情；
影视解说要盖着对白讲、主动复述剧情。以前这些取向散落在 prompt 的条件分支、
pacing 的覆盖判定和渲染的默认音量里，改一种风格要同时改三处且容易漏。

这里把一种风格需要的全部取向收进一个不可变对象，调用方只传风格 id。
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from .models import ValidationError

DEFAULT_STYLE_ID = "documentary"


@dataclass(frozen=True, slots=True)
class NarrationStyle:
    """一种解说风格的全部创作取向。"""

    id: str
    display_name: str
    description: str

    # —— 策划与文案 ——
    coverage_low: float
    """旁白目标覆盖原片时长的下限（0-1）。"""

    coverage_high: float
    """旁白目标覆盖原片时长的上限（0-1）。"""

    max_gap_seconds: float
    """相邻解说之间可接受的最长无解说空档，超出会触发一次改写。"""

    pause_low_seconds: float
    pause_high_seconds: float
    """普通句间停顿区间。"""

    speech_rate_low: float
    speech_rate_high: float
    """中文语速区间（字/秒）。"""

    retell_plot: bool
    """True 表示主动复述剧情（允许提前概括、点明结果）；
    False 表示严格事件后置，绝不在画面之前说破。"""

    talk_over_dialogue: bool
    """True 表示盖着原片对白讲；False 表示避让有信息量的对白。"""

    tone_guidance: str
    """注入 prompt 的文案语气要求。"""

    # —— 节奏校验 ——
    dialogue_counts_as_coverage: bool
    """True 表示原片有对白的区间视为"已覆盖"、不要求补解说。
    影视解说必须为 False，否则只要原片有人说话就不会生成解说。"""

    # —— 渲染默认值 ——
    default_original_audio_volume: float
    """该风格下原声的建议音量，仅作为前端默认值，用户仍可覆盖。"""

    def coverage_percent_text(self) -> str:
        return f"{self.coverage_low:.0%}-{self.coverage_high:.0%}"

    def speech_rate_text(self) -> str:
        return f"{self.speech_rate_low:g}-{self.speech_rate_high:g}字/秒"

    def pause_text(self) -> str:
        return f"{self.pause_low_seconds:g}-{self.pause_high_seconds:g}秒"


DOCUMENTARY_STYLE = NarrationStyle(
    id="documentary",
    display_name="纪录片伴随解说",
    description="克制、有画面感的旁白，避让原片对白，严格不剧透。适合纪录片、Vlog、实拍素材。",
    coverage_low=0.55,
    coverage_high=0.70,
    max_gap_seconds=5.0,
    pause_low_seconds=0.8,
    pause_high_seconds=2.0,
    speech_rate_low=2.8,
    speech_rate_high=3.5,
    retell_plot=False,
    talk_over_dialogue=False,
    tone_guidance=("语气克制、有画面感，像纪录片旁白。不抢戏，让画面和原声自己说话。"),
    dialogue_counts_as_coverage=True,
    default_original_audio_volume=0.2,
)

RECAP_STYLE = NarrationStyle(
    id="recap",
    display_name="影视解说",
    description=(
        "全程复述剧情的第三人称解说，口语化、高信息密度，盖着原片对白讲。适合影视剧、预告片、番剧。"
    ),
    coverage_low=0.88,
    coverage_high=0.96,
    max_gap_seconds=2.0,
    pause_low_seconds=0.2,
    pause_high_seconds=0.6,
    speech_rate_low=4.0,
    speech_rate_high=5.0,
    retell_plot=True,
    talk_over_dialogue=True,
    tone_guidance=(
        "口语化的第三人称讲故事语气，像在跟朋友转述一部片子。多用短句，"
        "善用「这时候」「结果」「没想到」这类推进词衔接。每句都要有新信息，"
        "不写形容词堆砌的空话，不复述对白原文而是转述其含义。"
    ),
    dialogue_counts_as_coverage=False,
    default_original_audio_volume=0.08,
)

NARRATION_STYLES: dict[str, NarrationStyle] = {
    DOCUMENTARY_STYLE.id: DOCUMENTARY_STYLE,
    RECAP_STYLE.id: RECAP_STYLE,
}


def resolve_style(value: Any) -> NarrationStyle:
    """把输入里的风格 id 解析成预设，空值回落到默认风格。"""

    style_id = str(value or "").strip() or DEFAULT_STYLE_ID
    style = NARRATION_STYLES.get(style_id)
    if style is None:
        raise ValidationError(f"style must be one of {sorted(NARRATION_STYLES)}")
    return style


def style_options() -> list[dict[str, Any]]:
    """供前端渲染选择器使用的风格清单。"""

    return [
        {
            "id": style.id,
            "displayName": style.display_name,
            "description": style.description,
            "defaultOriginalAudioVolume": style.default_original_audio_volume,
        }
        for style in NARRATION_STYLES.values()
    ]
