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

SEGMENT_SCHEMA = {
    "type": "object",
    "additionalProperties": False,
    "required": [
        "id",
        "sourceStartSeconds",
        "sourceEndSeconds",
        "narration",
        "ost",
    ],
    "properties": {
        "id": {"type": "string", "pattern": r"^segment-[0-9]{3,}$"},
        "sourceStartSeconds": {"type": "number", "minimum": 0},
        "sourceEndSeconds": {"type": "number", "exclusiveMinimum": 0},
        "narration": {"type": "string", "minLength": 1},
        "ost": {"type": "integer", "enum": [0, 1, 2]},
    },
}

DOCUMENTARY_SCRIPT_SCHEMA = {
    "type": "object",
    "additionalProperties": False,
    "required": [
        "version",
        "sourceVideoUri",
        "sourceDurationSeconds",
        "theme",
        "segments",
    ],
    "properties": {
        "version": {"const": "1"},
        "sourceVideoUri": {"type": "string", "minLength": 1},
        "sourceDurationSeconds": {"type": "number", "exclusiveMinimum": 0},
        "theme": {"type": "string"},
        "segments": {
            "type": "array",
            "minItems": 1,
            "maxItems": 500,
            "items": SEGMENT_SCHEMA,
        },
    },
}

GENERATE_INPUT_SCHEMA = {
    "type": "object",
    "additionalProperties": False,
    "required": ["videoUri"],
    "properties": {
        "videoUri": {"type": "string", "minLength": 1},
        "theme": {"type": "string", "default": ""},
        "instructions": {"type": "string", "default": ""},
    },
}

_RENDER_PROPERTIES = {
    "script": DOCUMENTARY_SCRIPT_SCHEMA,
    "voiceId": {"type": "string", "minLength": 1},
    "aspect": {"type": "string", "enum": ["source", "16:9", "9:16", "1:1"]},
    "narrationVolume": {"type": "number", "minimum": 0, "maximum": 2},
    "subtitles": {"type": "boolean"},
}

RENDER_INPUT_SCHEMA = {
    "type": "object",
    "additionalProperties": False,
    "required": ["script", "voiceId"],
    "properties": _RENDER_PROPERTIES,
}

CREATE_INPUT_SCHEMA = {
    "type": "object",
    "additionalProperties": False,
    "required": ["videoUri", "voiceId"],
    "properties": {
        **GENERATE_INPUT_SCHEMA["properties"],
        "voiceId": _RENDER_PROPERTIES["voiceId"],
        "aspect": _RENDER_PROPERTIES["aspect"],
        "narrationVolume": _RENDER_PROPERTIES["narrationVolume"],
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
