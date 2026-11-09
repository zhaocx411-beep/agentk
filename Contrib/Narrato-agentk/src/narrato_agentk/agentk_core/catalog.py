from __future__ import annotations

# Mirrors agent-k/internal/capability/tts/dashscope.go DefaultDashScopeVoices.
# AgentK uses this list when the active qwen3-tts model does not override `voices`.
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
