ARTIFACT_SCHEMA = {
    "type": "object",
    "additionalProperties": True,
    "required": ["type", "uri"],
    "properties": {
        "type": {"type": "string", "minLength": 1},
        "uri": {"type": "string", "minLength": 1},
        "description": {"type": "string"},
        "model": {"type": "string"},
        "params": {"type": "object"},
    },
}

NARRATIVE_PROFILE_SCHEMA = {
    "type": "object",
    "additionalProperties": False,
    "required": [
        "primaryGenre",
        "secondaryGenres",
        "thesis",
        "hook",
        "style",
        "pacing",
        "globalTtsInstruction",
    ],
    "properties": {
        "primaryGenre": {"type": "string", "minLength": 1},
        "secondaryGenres": {
            "type": "array",
            "maxItems": 3,
            "items": {"type": "string", "minLength": 1},
        },
        "thesis": {"type": "string", "minLength": 1},
        "hook": {"type": "string", "minLength": 1},
        "style": {"type": "string", "minLength": 1},
        "pacing": {"type": "string", "minLength": 1},
        "globalTtsInstruction": {"type": "string", "minLength": 1},
    },
}

NARRATION_CUE_SCHEMA = {
    "type": "object",
    "additionalProperties": False,
    "required": [
        "id",
        "startSeconds",
        "endSeconds",
        "narration",
        "evidenceWindowIndexes",
        "ttsInstruction",
        "importance",
    ],
    "properties": {
        "id": {"type": "string", "pattern": r"^cue-[0-9]{3,}$"},
        "startSeconds": {"type": "number", "minimum": 0},
        "endSeconds": {"type": "number", "exclusiveMinimum": 0},
        "evidenceEndSeconds": {
            "type": "number",
            "description": (
                "解说所使用的全部画面与 ASR 证据在原片中最晚发生完毕的时间；"
                "自动脚本应尽量让 startSeconds 不早于此时间"
            ),
        },
        "narration": {"type": "string", "minLength": 1},
        "evidenceWindowIndexes": {
            "type": "array",
            "minItems": 1,
            "items": {"type": "integer", "minimum": 0},
        },
        "ttsInstruction": {"type": "string", "minLength": 1},
        "importance": {"type": "integer", "minimum": 1, "maximum": 5},
        "emotion": {
            "type": "string",
            "enum": [
                "happy",
                "sad",
                "angry",
                "fearful",
                "disgusted",
                "surprised",
                "calm",
                "fluent",
                "whisper",
            ],
        },
    },
}

DOCUMENTARY_SCRIPT_SCHEMA = {
    "type": "object",
    "additionalProperties": False,
    "required": [
        "version",
        "sourceVideoUri",
        "sourceDurationSeconds",
        "narrativeProfile",
        "narrationCues",
    ],
    "properties": {
        "version": {"const": "2"},
        "sourceVideoUri": {"type": "string", "minLength": 1},
        "sourceDurationSeconds": {"type": "number", "exclusiveMinimum": 0},
        "narrativeProfile": NARRATIVE_PROFILE_SCHEMA,
        "narrationCues": {
            "type": "array",
            "minItems": 1,
            "maxItems": 500,
            "items": NARRATION_CUE_SCHEMA,
        },
        "pacingWarning": {"type": "string", "minLength": 1},
    },
}

# 交给模型做约束解码用的脚本 schema。
#
# 与 DOCUMENTARY_SCRIPT_SCHEMA 的唯一差别是去掉了 pacingWarning：那是工作流在拿到
# 脚本之后确定性算出来的节奏批注，不是创作内容。把它暴露给模型只有坏处——模型会往里
# 写"覆盖率约 97%、无超长空档"这类自我评价，既不可信，又会诱导它为了凑出好看的数字
# 而扭曲文案。产出契约里仍保留该字段，由工作流自己填。
SCRIPT_GENERATION_SCHEMA = {
    **DOCUMENTARY_SCRIPT_SCHEMA,
    "properties": {
        key: value
        for key, value in DOCUMENTARY_SCRIPT_SCHEMA["properties"].items()
        if key != "pacingWarning"
    },
}

NARRATIVE_PLAN_SCHEMA = {
    "type": "object",
    "additionalProperties": False,
    "required": [
        *NARRATIVE_PROFILE_SCHEMA["required"],
        "researchNeeded",
        "researchReason",
        "researchQueries",
        "storyBeats",
    ],
    "properties": {
        **NARRATIVE_PROFILE_SCHEMA["properties"],
        "researchNeeded": {"type": "boolean"},
        "researchReason": {"type": "string"},
        "researchQueries": {
            "type": "array",
            "maxItems": 5,
            "items": {"type": "string", "minLength": 1, "maxLength": 100},
        },
        "storyBeats": {
            "type": "array",
            "minItems": 1,
            "maxItems": 500,
            "items": {
                "type": "object",
                "additionalProperties": False,
                "required": [
                    "id",
                    "startSeconds",
                    "endSeconds",
                    "summary",
                    "narrationPurpose",
                    "importance",
                    "evidenceWindowIndexes",
                ],
                "properties": {
                    "id": {"type": "string", "pattern": r"^beat-[0-9]{3,}$"},
                    "startSeconds": {"type": "number", "minimum": 0},
                    "endSeconds": {"type": "number", "exclusiveMinimum": 0},
                    "summary": {"type": "string", "minLength": 1},
                    "narrationPurpose": {"type": "string", "minLength": 1},
                    "importance": {"type": "integer", "minimum": 1, "maximum": 5},
                    "evidenceWindowIndexes": {
                        "type": "array",
                        "minItems": 1,
                        "items": {"type": "integer", "minimum": 0},
                    },
                },
            },
        },
    },
}

GENERATE_INPUT_SCHEMA = {
    "type": "object",
    "additionalProperties": False,
    "required": ["videoUri"],
    "properties": {
        "videoUri": {"type": "string", "minLength": 1},
        "instructions": {"type": "string", "default": ""},
        "style": {
            "type": "string",
            "enum": ["documentary", "recap"],
            "default": "documentary",
            "description": (
                "解说风格：documentary 为避让对白、不剧透的伴随旁白；"
                "recap 为盖着对白复述剧情的影视解说"
            ),
        },
        "models": {
            "type": "object",
            "additionalProperties": False,
            "properties": {
                "vision": {"type": "string", "minLength": 1},
                "asr": {"type": "string", "minLength": 1},
                "search": {"type": "string", "minLength": 1},
                "story": {"type": "string", "minLength": 1},
                "script": {"type": "string", "minLength": 1},
            },
        },
        "analysisOptions": {
            "type": "object",
            "additionalProperties": False,
            "properties": {
                "asrEnabled": {"type": "boolean", "default": True},
                "asrLanguage": {
                    "type": "string",
                    "default": "",
                    "description": "BCP-47 language hint; empty lets ASR auto-detect",
                },
            },
        },
    },
}

TTS_INPUT_SCHEMA = {
    "type": "object",
    "additionalProperties": False,
    "required": ["provider", "model", "voiceId"],
    "properties": {
        "provider": {"type": "string", "enum": ["qwen", "minimax"]},
        "model": {"type": "string", "minLength": 1},
        "voiceId": {"type": "string", "minLength": 1},
    },
}

_RENDER_PROPERTIES = {
    "script": DOCUMENTARY_SCRIPT_SCHEMA,
    "tts": TTS_INPUT_SCHEMA,
    "aspect": {"type": "string", "enum": ["source", "16:9", "9:16", "1:1"]},
    "narrationVolume": {"type": "number", "minimum": 0, "maximum": 2},
    "originalAudioVolume": {"type": "number", "minimum": 0, "maximum": 2},
    "subtitles": {"type": "boolean"},
}

RENDER_INPUT_SCHEMA = {
    "type": "object",
    "additionalProperties": False,
    "required": ["script", "tts"],
    "properties": _RENDER_PROPERTIES,
}

CREATE_INPUT_SCHEMA = {
    "type": "object",
    "additionalProperties": False,
    "required": ["videoUri", "tts"],
    "properties": {
        **GENERATE_INPUT_SCHEMA["properties"],
        "tts": _RENDER_PROPERTIES["tts"],
        "aspect": _RENDER_PROPERTIES["aspect"],
        "narrationVolume": _RENDER_PROPERTIES["narrationVolume"],
        "originalAudioVolume": _RENDER_PROPERTIES["originalAudioVolume"],
        "subtitles": _RENDER_PROPERTIES["subtitles"],
    },
}

PIPELINE_OUTPUT_SCHEMA = {
    "type": "object",
    "additionalProperties": True,
    "required": ["script", "artifacts", "durationSeconds", "segmentCount"],
    "properties": {
        "script": DOCUMENTARY_SCRIPT_SCHEMA,
        "artifacts": {"type": "array", "minItems": 1, "items": ARTIFACT_SCHEMA},
        "durationSeconds": {"type": "number", "minimum": 0},
        "segmentCount": {"type": "integer", "minimum": 1},
    },
}

TRANSCRIPT_CORRECTION_SCHEMA = {
    "type": "object",
    "additionalProperties": False,
    "required": ["segments"],
    "properties": {
        "segments": {
            "type": "array",
            "maxItems": 5000,
            "items": {
                "type": "object",
                "additionalProperties": False,
                "required": ["id", "startSeconds", "endSeconds", "text"],
                "properties": {
                    "id": {"type": "string", "minLength": 1},
                    "startSeconds": {"type": "number", "minimum": 0},
                    "endSeconds": {"type": "number", "exclusiveMinimum": 0},
                    "text": {"type": "string", "minLength": 1},
                },
            },
        }
    },
}

RESEARCH_PLAN_SCHEMA = {
    "type": "object",
    "additionalProperties": False,
    "required": ["needed", "reason", "queries"],
    "properties": {
        "needed": {"type": "boolean"},
        "reason": {"type": "string"},
        "queries": {
            "type": "array",
            "maxItems": 5,
            "items": {"type": "string", "minLength": 1, "maxLength": 100},
        },
    },
}

STORY_ANALYSIS_SCHEMA = {
    "type": "object",
    "additionalProperties": False,
    "required": ["summary", "characters", "scenes", "facts", "uncertainties"],
    "properties": {
        "summary": {"type": "string", "minLength": 1},
        "characters": {
            "type": "array",
            "maxItems": 100,
            "items": {
                "type": "object",
                "additionalProperties": False,
                "required": ["name", "role", "evidence"],
                "properties": {
                    "name": {"type": "string", "minLength": 1},
                    "role": {"type": "string"},
                    "evidence": {"type": "array", "items": {"type": "string"}},
                },
            },
        },
        "scenes": {
            "type": "array",
            "minItems": 1,
            "maxItems": 500,
            "items": {
                "type": "object",
                "additionalProperties": False,
                "required": [
                    "startSeconds",
                    "endSeconds",
                    "summary",
                    "visualFacts",
                    "dialogueFacts",
                    "uncertainties",
                ],
                "properties": {
                    "startSeconds": {"type": "number", "minimum": 0},
                    "endSeconds": {"type": "number", "exclusiveMinimum": 0},
                    "summary": {"type": "string", "minLength": 1},
                    "visualFacts": {"type": "array", "items": {"type": "string"}},
                    "dialogueFacts": {"type": "array", "items": {"type": "string"}},
                    "uncertainties": {"type": "array", "items": {"type": "string"}},
                },
            },
        },
        "facts": {"type": "array", "items": {"type": "string"}},
        "uncertainties": {"type": "array", "items": {"type": "string"}},
    },
}

NARRATION_DRAFT_SCHEMA = {
    "type": "object",
    "additionalProperties": False,
    "required": ["segments"],
    "properties": {
        "segments": {
            "type": "array",
            "minItems": 1,
            "maxItems": 500,
            "items": {
                "type": "object",
                "additionalProperties": False,
                "required": ["id", "narration", "evidenceWindowIndexes"],
                "properties": {
                    "id": {"type": "string", "pattern": r"^draft-[0-9]{3,}$"},
                    "narration": {"type": "string", "minLength": 1},
                    "evidenceWindowIndexes": {
                        "type": "array",
                        "minItems": 1,
                        "items": {"type": "integer", "minimum": 0},
                    },
                },
            },
        }
    },
}
