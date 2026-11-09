from datetime import timedelta

import pytest

from agentk.worker import Capability, Tool
from agentk.worker._manifest import build_manifest


def declare(
    capability_id: str,
    *,
    tool: Tool | None = None,
) -> Capability:
    return Capability(
        id=capability_id,
        temporal_type=f"{capability_id}.workflow",
        description=f"Capability {capability_id}",
        input_schema={
            "type": "object",
            "properties": {"topic": {"type": "string"}},
        },
        output_schema={
            "type": "object",
            "properties": {"result": {"type": "string"}},
        },
        timeout=timedelta(minutes=5),
        tool=tool,
    )


def test_manifest_sorts_capabilities_and_maps_wire_names() -> None:
    manifest = build_manifest(
        "example-queue",
        workflows=[
            declare("contrib.example.zeta"),
            declare(
                "contrib.example.alpha",
                tool=Tool(),
            ),
        ],
    )

    payload = manifest.to_payload()

    assert payload["protocolVersion"] == 1
    assert payload["taskQueue"] == "example-queue"
    assert [definition["id"] for definition in payload["capabilities"]] == [
        "contrib.example.alpha",
        "contrib.example.zeta",
    ]
    assert payload["capabilities"][0]["temporalType"] == (
        "contrib.example.alpha.workflow"
    )
    assert payload["capabilities"][0]["timeoutSeconds"] == 300
    assert payload["capabilities"][0]["tool"] == {
        "description": "Capability contrib.example.alpha",
        "mode": "blocking",
    }
    assert "artifact" not in payload["capabilities"][1]


def test_manifest_payload_is_a_deep_copy() -> None:
    declared = declare("contrib.example.copy")
    manifest = build_manifest("example-queue", workflows=[declared])
    first = manifest.to_payload()

    first["capabilities"][0]["inputSchema"]["properties"]["topic"]["type"] = "integer"
    declared.input_schema["properties"]["topic"]["type"] = "number"

    second = manifest.to_payload()
    assert (
        second["capabilities"][0]["inputSchema"]["properties"]["topic"]["type"]
        == "string"
    )


def test_manifest_rejects_duplicate_capability_ids() -> None:
    workflows = [
        declare("contrib.example.same"),
        declare("contrib.example.same"),
    ]
    with pytest.raises(ValueError, match="duplicate capability id"):
        build_manifest("example-queue", workflows=workflows)


def test_manifest_rejects_blank_task_queue() -> None:
    with pytest.raises(ValueError, match="task_queue"):
        build_manifest(" ", workflows=[declare("contrib.example.valid")])


@pytest.mark.parametrize("lane_id", ["", "dev lane", "x" * 257])
def test_manifest_rejects_invalid_lane(lane_id: str) -> None:
    with pytest.raises(ValueError):
        build_manifest(
            "example-queue",
            lane_id=lane_id,
            workflows=[declare("contrib.example.valid")],
        )


def test_manifest_allows_agentk_capability() -> None:
    manifest = build_manifest(
        "agentk",
        workflows=[declare("agentk.summarize")],
    )

    assert manifest.to_payload()["capabilities"][0]["id"] == "agentk.summarize"


def test_manifest_injects_kind_per_group() -> None:
    payload = build_manifest(
        "example-queue",
        workflows=[declare("contrib.example.orchestration")],
        activities=[declare("contrib.example.step")],
    ).to_payload()

    kinds = {
        definition["id"]: definition["kind"] for definition in payload["capabilities"]
    }
    assert kinds == {
        "contrib.example.orchestration": "workflow",
        "contrib.example.step": "activity",
    }


def test_manifest_serializes_positive_max_attempts() -> None:
    payload = build_manifest(
        "example-queue",
        workflows=[
            declare(
                "contrib.example.limited",
                tool=Tool(
                    max_attempts=4,
                ),
            )
        ],
    ).to_payload()

    assert payload["capabilities"][0]["tool"]["maxAttempts"] == 4
