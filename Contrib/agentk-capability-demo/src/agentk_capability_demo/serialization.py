from __future__ import annotations

import json
from typing import Any


def message_dict(message: Any) -> dict[str, Any]:
    """Serialize protobuf-py messages using their canonical JSON mapping."""
    if message is None:
        return {}
    return json.loads(message.to_json())


def message_value(message: Any) -> Any:
    if message is None:
        return None
    return json.loads(message.to_json())


def proto_from_dict(message_type: Any, value: dict[str, Any]) -> Any:
    return message_type.from_json(json.dumps(value, ensure_ascii=False))


def oneof_name(message: Any, group: str) -> str | None:
    oneof = getattr(message, group, None)
    if oneof is None:
        return None
    name = getattr(oneof, "field_name", None)
    if callable(name):
        return name()
    return str(name) if name else None

