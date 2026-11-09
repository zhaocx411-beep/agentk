import json
import math
import re
from collections.abc import Mapping
from dataclasses import dataclass, replace
from datetime import timedelta
from typing import Any, Literal

JsonObject = Mapping[str, Any]
CapabilityKind = Literal["workflow", "activity"]

_CAPABILITY_ID_PATTERN = re.compile(
    r"^(agentk\.[a-z][a-z0-9_]*|"
    r"contrib\.[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*)$"
)
_MAX_TIMEOUT = timedelta(hours=24)


@dataclass(frozen=True, slots=True)
class Artifact:
    """An artifact produced by a capability.

    Attributes:
        type: The kind of artifact, such as an image or video.
        uri: The location of the artifact for downstream consumers.
        description: An optional human-readable summary.
        model: The model that produced the artifact, when applicable.
        params: Optional model or generation parameters.
    """

    type: str
    uri: str
    description: str | None = None
    model: str | None = None
    params: dict[str, Any] | None = None


@dataclass(frozen=True, slots=True)
class Tool:
    """Settings for exposing a capability as a model-callable Tool.

    Attributes:
        description: Text used for Tool selection. None inherits the capability
            description.
        max_attempts: The maximum number of Tool attempts. None or zero leaves
            the value unspecified; negative values are invalid.
    """

    description: str | None = None
    max_attempts: int | None = None


@dataclass(frozen=True, slots=True)
class Capability:
    """A Temporal workflow or activity published by an AgentK Worker.

    The publish method supplies the implementation kind, and the Worker supplies
    the task queue. The Worker snapshots and validates this declaration before
    it starts.

    Attributes:
        id: The globally unique capability ID. Contributor Workers use
            contrib.<owner>.<name>; IDs contain lowercase letters, digits, and
            underscores and are at most 128 characters.
        temporal_type: The registered Temporal workflow or activity type.
        description: A user-facing description of the capability.
        input_schema: The JSON Schema for input. Its top-level type must be
            object.
        output_schema: The JSON Schema for the capability result.
        timeout: The maximum invocation duration. It must contain whole seconds
            and be greater than zero and no more than 24 hours.
        tool: Tool exposure settings, or None when the capability should not be
            presented as a Tool.
    """

    id: str
    temporal_type: str
    description: str
    input_schema: JsonObject
    output_schema: JsonObject
    timeout: timedelta
    tool: Tool | None = None


def _normalize_capability(
    capability: Capability,
    *,
    kind: CapabilityKind,
) -> Capability:
    if not isinstance(capability, Capability):
        raise TypeError("capability must be a Capability")
    if kind not in ("workflow", "activity"):
        raise ValueError(f"capability kind is unknown: {kind!r}")
    if (
        len(capability.id) > 128
        or _CAPABILITY_ID_PATTERN.fullmatch(capability.id) is None
    ):
        raise ValueError(
            "capability.id must match agentk.<name> or contrib.<owner>.<name>, "
            "each segment must match [a-z][a-z0-9_]*, and the total length "
            "must be at most 128 characters"
        )
    _require_non_blank(capability.temporal_type, "capability.temporal_type")
    _require_non_blank(capability.description, "capability.description")

    timeout_seconds = _timeout_seconds(capability.timeout)
    input_schema = _clone_json_object(
        capability.input_schema,
        "capability.input_schema",
    )
    if input_schema.get("type") != "object":
        raise ValueError('capability.input_schema.type must be "object"')
    output_schema = _clone_json_object(
        capability.output_schema,
        "capability.output_schema",
    )

    return replace(
        capability,
        input_schema=input_schema,
        output_schema=output_schema,
        timeout=timedelta(seconds=timeout_seconds),
        tool=_normalize_tool(
            capability.tool,
            fallback_description=capability.description,
        ),
    )


def _normalize_tool(
    tool: Tool | None,
    *,
    fallback_description: str,
) -> Tool | None:
    if tool is None:
        return None
    max_attempts = tool.max_attempts
    if max_attempts is not None:
        if isinstance(max_attempts, bool) or not isinstance(max_attempts, int):
            raise TypeError("tool.max_attempts must be an integer or None")
        if max_attempts < 0:
            raise ValueError("tool.max_attempts must be non-negative")
        if max_attempts == 0:
            max_attempts = None
    description = tool.description
    if description is None:
        description = fallback_description
    else:
        _require_non_blank(description, "tool.description")
    return replace(
        tool,
        description=description,
        max_attempts=max_attempts,
    )


def _timeout_seconds(timeout: timedelta) -> int:
    if not isinstance(timeout, timedelta):
        raise TypeError("capability.timeout must be a timedelta")
    seconds = timeout.total_seconds()
    if not math.isfinite(seconds) or not seconds.is_integer():
        raise ValueError("capability.timeout must contain whole seconds")
    if timeout <= timedelta(0) or timeout > _MAX_TIMEOUT:
        raise ValueError("capability.timeout must be in (0, 24 hours]")
    return int(seconds)


def _clone_json_object(value: JsonObject, path: str) -> dict[str, Any]:
    if not isinstance(value, Mapping):
        raise TypeError(f"{path} must be a JSON object")
    _validate_json_value(value, path)
    return json.loads(json.dumps(value, allow_nan=False, ensure_ascii=False))


def _validate_json_value(value: Any, path: str) -> None:
    if value is None or isinstance(value, (bool, str, int)):
        return
    if isinstance(value, float):
        if not math.isfinite(value):
            raise ValueError(f"{path} must not contain NaN or infinity")
        return
    if isinstance(value, Mapping):
        for key, item in value.items():
            if not isinstance(key, str):
                raise TypeError(f"{path} must contain only string keys")
            _validate_json_value(item, f"{path}.{key}")
        return
    if isinstance(value, (list, tuple)):
        for index, item in enumerate(value):
            _validate_json_value(item, f"{path}[{index}]")
        return
    raise ValueError(f"{path} contains a non-JSON value of type {type(value).__name__}")


def _require_non_blank(value: str, path: str) -> None:
    if not isinstance(value, str) or not value.strip():
        raise ValueError(f"{path} is required")
