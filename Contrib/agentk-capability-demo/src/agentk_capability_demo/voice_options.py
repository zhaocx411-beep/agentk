from __future__ import annotations

from copy import deepcopy
from typing import Any

# These tables mirror AgentK's provider defaults:
# internal/capability/tts/dashscope.go DefaultDashScopeVoices and
# internal/capability/tts/minimax.go DefaultMiniMaxVoices.
QWEN_VOICE_OPTIONS: tuple[dict[str, str], ...] = (
    {"id": "Cherry", "description": "芊晓：阳光亲切女声"},
    {"id": "Serena", "description": "佩霞：温婉女声"},
    {"id": "Ethan", "description": "晨煦：阳光稳重男声"},
    {"id": "Christie", "description": "千雪：二次元萌系女声"},
    {"id": "Rono", "description": "元兔：俏皮活泼女声"},
    {"id": "Vivian", "description": "十三：甜美女声"},
    {"id": "Ryan", "description": "沐白：清爽大气男声"},
    {"id": "Rana", "description": "兰月：温柔女声"},
    {"id": "Kai", "description": "凯：未来感男声"},
    {"id": "Nafish", "description": "不败枭：低沉男声"},
    {"id": "Bella", "description": "碧宝：俏皮稳重女声"},
    {"id": "Jennifer", "description": "珍妮弗：知性磁性女声"},
)

MINIMAX_VOICE_OPTIONS: tuple[dict[str, str], ...] = (
    {
        "id": "moss_audio_ce44fc67-7ce3-11f0-8de5-96e35d26fb85",
        "description": "中文系统音色 1",
    },
    {
        "id": "moss_audio_aaa1346a-7ce7-11f0-8e61-2e6e3c7ee85d",
        "description": "中文系统音色 2",
    },
    {"id": "Chinese (Mandarin)_Lyrical_Voice", "description": "中文普通话抒情音色"},
    {"id": "Chinese (Mandarin)_HK_Flight_Attendant", "description": "中文普通话空乘音色"},
    {"id": "English_Graceful_Lady", "description": "英文优雅女声"},
    {"id": "English_Insightful_Speaker", "description": "英文睿智讲述音色"},
    {"id": "English_radiant_girl", "description": "英文明快女声"},
    {"id": "English_Persuasive_Man", "description": "英文有说服力男声"},
    {
        "id": "moss_audio_6dc281eb-713c-11f0-a447-9613c873494c",
        "description": "英文系统音色 1",
    },
    {
        "id": "moss_audio_570551b1-735c-11f0-b236-0adeeecad052",
        "description": "英文系统音色 2",
    },
    {
        "id": "moss_audio_ad5baf92-735f-11f0-8263-fe5a2fe98ec8",
        "description": "英文系统音色 3",
    },
    {"id": "English_Lucky_Robot", "description": "英文幸运机器人音色"},
    {"id": "Japanese_Whisper_Belle", "description": "日文低语女声"},
    {
        "id": "moss_audio_24875c4a-7be4-11f0-9359-4e72c55db738",
        "description": "日文系统音色 1",
    },
    {
        "id": "moss_audio_7f4ee608-78ea-11f0-bb73-1e2a4cfcd245",
        "description": "日文系统音色 2",
    },
    {
        "id": "moss_audio_c1a6a3ac-7be6-11f0-8e8e-36b92fbb4f95",
        "description": "日文系统音色 3",
    },
    {"id": "Cantonese_GentleLady", "description": "粤语温柔女声"},
    {"id": "Cantonese_podcast_host_1", "description": "粤语播客主持音色"},
)

_OPTIONS_BY_CAPABILITY: dict[str, tuple[dict[str, str], ...]] = {
    "agentk.generate_speech_qwen": QWEN_VOICE_OPTIONS,
    "agentk.generate_speech_minimax": MINIMAX_VOICE_OPTIONS,
}

HIDDEN_CAPABILITY_IDS = frozenset(
    {
        "agentk.generate_brand_ad",
        "agentk.generate_speech_cosyvoice",
    }
)
CONTRIB_CAPABILITY_SOURCE = "CAPABILITY_SOURCE_CONTRIB"

_SCHEMA_PROPERTY_PATCHES: dict[str, dict[str, dict[str, Any]]] = {
    # AgentK's generic videogen implementation parses a string duration and
    # rejects values outside 4..15, but its current manifest omits that range.
    "agentk.generate_video": {
        "resolution": {
            "enum": ["720p", "1080p", "4k"],
            "default": "1080p",
            "description": "输出清晰度：720p、1080p 或 4k；缺省 1080p",
            "x-contract-warning": "AgentK staging manifest 未声明候选值；Demo 按当前实现补充",
        },
        "audio": {
            "enum": ["off", "native"],
            "default": "off",
            "description": "声音模式：off 无声，native 生成原生声音；缺省 off",
            "x-contract-warning": "AgentK staging manifest 未声明候选值；Demo 按当前实现补充",
        },
        "duration": {
            "enum": [str(seconds) for seconds in range(4, 16)],
            "default": "5",
            "description": "片段时长（秒），4~15，缺省 5",
            "x-contract-warning": "AgentK staging manifest 未声明 4~15 秒范围；Demo 按当前实现补充",
        }
    },
}


def enrich_capability_voice_options(capability: dict[str, Any]) -> dict[str, Any]:
    capability_id = str(capability.get("id") or "")
    if capability_id not in _OPTIONS_BY_CAPABILITY:
        return capability

    enriched = deepcopy(capability)
    schema = enriched.get("inputSchema")
    properties = schema.get("properties") if isinstance(schema, dict) else None
    voice_schema = properties.get("voiceId") if isinstance(properties, dict) else None
    if not isinstance(voice_schema, dict):
        return capability

    options = _OPTIONS_BY_CAPABILITY[capability_id]
    voice_schema["x-options"] = [
        {"value": option["id"], "label": f'{option["description"]}（{option["id"]}）'}
        for option in options
    ]
    if options and "default" not in voice_schema:
        voice_schema["default"] = options[0]["id"]
    return enriched


def enrich_capability_schema(capability: dict[str, Any]) -> dict[str, Any]:
    enriched = enrich_capability_voice_options(capability)
    patches = _SCHEMA_PROPERTY_PATCHES.get(str(enriched.get("id") or ""))
    if not patches:
        return enriched
    if enriched is capability:
        enriched = deepcopy(capability)
    schema = enriched.get("inputSchema")
    properties = schema.get("properties") if isinstance(schema, dict) else None
    if not isinstance(properties, dict):
        return enriched
    for name, patch in patches.items():
        field_schema = properties.get(name)
        if isinstance(field_schema, dict):
            field_schema.update(deepcopy(patch))
    return enriched


def capability_is_public(capability: dict[str, Any]) -> bool:
    capability_id = str(capability.get("id") or "")
    source = str(capability.get("source") or "")
    return capability_id not in HIDDEN_CAPABILITY_IDS and source != CONTRIB_CAPABILITY_SOURCE
