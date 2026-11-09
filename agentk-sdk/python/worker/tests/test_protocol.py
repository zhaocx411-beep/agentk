import json
from copy import deepcopy
from datetime import timedelta
from pathlib import Path
from typing import Any

import pytest
from jsonschema import Draft202012Validator

from agentk.worker import Capability, Tool
from agentk.worker._manifest import build_manifest

_PROTOCOL_ROOT = (
    Path(__file__).resolve().parents[3] / "protocol" / "agentk" / "capability" / "v1"
)


def load_json(path: Path) -> dict[str, Any]:
    return json.loads(path.read_text())


def validator() -> Draft202012Validator:
    schema = load_json(_PROTOCOL_ROOT / "manifest.schema.json")
    Draft202012Validator.check_schema(schema)
    return Draft202012Validator(schema)


def test_shared_fixtures_conform_to_manifest_schema() -> None:
    manifest_validator = validator()

    for name in ("basic.json", "tool.json"):
        payload = load_json(_PROTOCOL_ROOT / "testdata" / name)
        manifest_validator.validate(payload)


def test_manifest_schema_rejects_artifact_declaration() -> None:
    payload = deepcopy(load_json(_PROTOCOL_ROOT / "testdata" / "basic.json"))
    payload["capabilities"][0]["artifact"] = {
        "type": "video",
        "uriField": "videoUrl",
    }

    assert not validator().is_valid(payload)


def test_manifest_schema_rejects_background_tool_mode() -> None:
    payload = deepcopy(load_json(_PROTOCOL_ROOT / "testdata" / "tool.json"))
    payload["capabilities"][0]["tool"]["mode"] = "background"

    assert not validator().is_valid(payload)


def test_manifest_schema_rejects_invalid_lane() -> None:
    payload = deepcopy(load_json(_PROTOCOL_ROOT / "testdata" / "tool.json"))
    payload["laneId"] = "dev lane"

    assert not validator().is_valid(payload)


@pytest.mark.parametrize("field", ["name", "pipeline"])
def test_manifest_schema_rejects_removed_exposure_fields(field: str) -> None:
    payload = deepcopy(load_json(_PROTOCOL_ROOT / "testdata" / "tool.json"))
    capability = payload["capabilities"][0]
    if field == "name":
        capability["tool"]["name"] = "legacy_tool"
    else:
        capability["pipeline"] = {
            "name": "legacy_pipeline",
            "description": "Legacy pipeline",
        }

    assert not validator().is_valid(payload)


@pytest.mark.parametrize(
    ("field", "value"),
    [
        ("id", "example.generate_brand_ad"),
        ("maxAttempts", -1),
    ],
)
def test_manifest_schema_rejects_invalid_capability_fields(
    field: str,
    value: Any,
) -> None:
    payload = deepcopy(load_json(_PROTOCOL_ROOT / "testdata" / "tool.json"))
    capability = payload["capabilities"][0]
    if field == "maxAttempts":
        capability["tool"][field] = value
    else:
        capability[field] = value

    assert not validator().is_valid(payload)


def test_python_basic_manifest_matches_shared_fixture() -> None:
    input_schema = {
        "type": "object",
        "additionalProperties": False,
        "required": ["text"],
        "properties": {
            "text": {
                "type": "string",
                "description": "待摘要的原始文本",
            }
        },
    }
    output_schema = {
        "type": "object",
        "additionalProperties": False,
        "required": ["summary"],
        "properties": {
            "summary": {
                "type": "string",
                "description": "生成后的简短摘要",
            }
        },
    }
    declared = Capability(
        id="contrib.example.summarize",
        temporal_type="example.summarize",
        description="对输入文本生成简短摘要",
        input_schema=input_schema,
        output_schema=output_schema,
        timeout=timedelta(seconds=600),
    )

    payload = build_manifest(
        "example-go-pipeline",
        workflows=[declared],
    ).to_payload()

    assert payload == load_json(_PROTOCOL_ROOT / "testdata" / "basic.json")
    validator().validate(payload)


def test_python_tool_manifest_matches_shared_fixture() -> None:
    input_schema = {
        "type": "object",
        "additionalProperties": False,
        "required": ["title", "images", "videoPrompt"],
        "properties": {
            "title": {"type": "string"},
            "images": {
                "type": "array",
                "items": {"type": "string"},
            },
            "videoPrompt": {"type": "string"},
        },
    }
    output_schema = {
        "type": "object",
        "additionalProperties": False,
        "required": ["artifacts"],
        "properties": {
            "artifacts": {
                "type": "array",
                "minItems": 1,
                "items": {
                    "type": "object",
                    "additionalProperties": False,
                    "required": ["type", "uri"],
                    "properties": {
                        "type": {"type": "string"},
                        "uri": {"type": "string"},
                        "description": {"type": "string"},
                        "model": {"type": "string"},
                        "params": {"type": "object"},
                    },
                },
            }
        },
    }
    description = "根据商品素材生成品牌广告视频"
    declared = Capability(
        id="contrib.example.generate_brand_ad",
        temporal_type="brandad.generate",
        description=description,
        input_schema=input_schema,
        output_schema=output_schema,
        timeout=timedelta(seconds=9000),
        tool=Tool(
            max_attempts=3,
        ),
    )

    payload = build_manifest(
        "brandad", lane_id="dev_example", workflows=[declared]
    ).to_payload()

    assert payload == load_json(_PROTOCOL_ROOT / "testdata" / "tool.json")
    validator().validate(payload)
