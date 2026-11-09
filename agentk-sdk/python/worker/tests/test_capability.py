import json
from dataclasses import FrozenInstanceError, asdict
from datetime import timedelta

import pytest

from agentk.worker import Artifact, Capability, Tool
from agentk.worker.capability import _normalize_capability


def capability(
    capability_id: str = "contrib.example.generate",
    *,
    timeout: timedelta = timedelta(minutes=1),
    input_schema: dict | None = None,
    output_schema: dict | None = None,
    tool: Tool | None = None,
) -> Capability:
    return Capability(
        id=capability_id,
        temporal_type=capability_id,
        description="Generate an example",
        input_schema=input_schema or {"type": "object"},
        output_schema=output_schema or {"type": "object"},
        timeout=timeout,
        tool=tool,
    )


def test_declarations_are_immutable_and_schemas_are_snapshotted() -> None:
    input_schema = {
        "type": "object",
        "properties": {"topic": {"type": "string"}},
    }
    declared = capability(input_schema=input_schema)
    normalized = _normalize_capability(declared, kind="workflow")

    input_schema["properties"]["topic"]["type"] = "integer"

    assert normalized.input_schema["properties"]["topic"]["type"] == "string"
    with pytest.raises(FrozenInstanceError):
        declared.id = "contrib.example.other"  # type: ignore[misc]


def test_artifact_has_fixed_runtime_json_wire() -> None:
    artifact = Artifact(
        type="video",
        uri="kfs://outputs/video.mp4",
        description="品牌广告成片",
        model="kling-video",
        params={"duration": 5},
    )

    encoded = json.dumps(
        asdict(artifact),
        separators=(",", ":"),
        ensure_ascii=False,
    )

    assert json.loads(encoded) == json.loads(
        '{"type":"video","uri":"kfs://outputs/video.mp4",'
        '"description":"品牌广告成片","model":"kling-video",'
        '"params":{"duration":5}}'
    )


@pytest.mark.parametrize(
    "capability_id",
    [
        "agentk.generate_brand_ad",
        "contrib.storyboard.film_python",
        "contrib.example.summarize",
        f"agentk.{'a' * 121}",
    ],
)
def test_accepts_canonical_capability_ids(capability_id: str) -> None:
    _normalize_capability(capability(capability_id), kind="workflow")


@pytest.mark.parametrize(
    "capability_id",
    [
        "contrib.storyboard_film_python",
        "example.summarize",
        "agentk.brandad.generate",
        "agentk.generate-brand-ad",
        "AgentK.generate_brand_ad",
        "agentk",
        f"agentk.{'a' * 122}",
    ],
)
def test_rejects_non_canonical_capability_ids(capability_id: str) -> None:
    with pytest.raises(ValueError, match=r"agentk\.<name> or contrib\.<owner>"):
        _normalize_capability(capability(capability_id), kind="workflow")


def test_tool_inherits_capability_description() -> None:
    normalized = _normalize_capability(
        capability(
            tool=Tool(),
        ),
        kind="workflow",
    )

    assert normalized.tool is not None
    assert normalized.tool.description == "Generate an example"
    assert normalized.tool.max_attempts is None


@pytest.mark.parametrize(("value", "expected"), [(None, None), (0, None), (3, 3)])
def test_tool_normalizes_max_attempts(
    value: int | None,
    expected: int | None,
) -> None:
    normalized = _normalize_capability(
        capability(
            tool=Tool(
                max_attempts=value,
            )
        ),
        kind="workflow",
    )

    assert normalized.tool is not None
    assert normalized.tool.max_attempts == expected


@pytest.mark.parametrize("value", [-1, True, 1.5])
def test_tool_rejects_invalid_max_attempts(value: object) -> None:
    with pytest.raises((TypeError, ValueError), match="max_attempts"):
        _normalize_capability(
            capability(
                tool=Tool(
                    max_attempts=value,  # type: ignore[arg-type]
                )
            ),
            kind="workflow",
        )


@pytest.mark.parametrize(
    ("timeout", "message"),
    [
        (timedelta(0), "must be in"),
        (timedelta(hours=24, seconds=1), "must be in"),
        (timedelta(microseconds=1), "whole seconds"),
    ],
)
def test_rejects_invalid_timeout(timeout: timedelta, message: str) -> None:
    with pytest.raises(ValueError, match=message):
        _normalize_capability(capability(timeout=timeout), kind="workflow")


@pytest.mark.parametrize(
    ("value", "message"),
    [
        (
            capability("Bad ID"),
            "capability.id",
        ),
        (
            capability(input_schema={"type": "string"}),
            "input_schema.type",
        ),
        (
            capability(output_schema={"value": float("nan")}),
            "NaN",
        ),
    ],
)
def test_rejects_invalid_capability_contract(
    value: Capability,
    message: str,
) -> None:
    with pytest.raises(ValueError, match=message):
        _normalize_capability(value, kind="workflow")


def test_rejects_blank_tool_description() -> None:
    with pytest.raises(ValueError, match="tool.description"):
        _normalize_capability(
            capability(
                tool=Tool(
                    description=" ",
                )
            ),
            kind="workflow",
        )
