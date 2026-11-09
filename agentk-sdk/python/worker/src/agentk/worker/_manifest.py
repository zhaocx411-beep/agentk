from collections.abc import Iterable
from copy import deepcopy
from dataclasses import dataclass
from typing import Any

from ksresolve import rpc as ksrpc

from .capability import (
    Capability,
    CapabilityKind,
    Tool,
    _normalize_capability,
    _timeout_seconds,
)

MANIFEST_PROTOCOL_VERSION = 1


@dataclass(frozen=True, slots=True)
class _CapabilityDefinition:
    id: str
    kind: CapabilityKind
    temporal_type: str
    description: str
    input_schema: dict[str, Any]
    output_schema: dict[str, Any]
    timeout_seconds: int
    tool: Tool | None

    def to_payload(self) -> dict[str, Any]:
        payload: dict[str, Any] = {
            "id": self.id,
            "kind": self.kind,
            "temporalType": self.temporal_type,
            "description": self.description,
            "inputSchema": deepcopy(self.input_schema),
            "outputSchema": deepcopy(self.output_schema),
            "timeoutSeconds": self.timeout_seconds,
        }
        if self.tool is not None:
            tool: dict[str, Any] = {
                "description": self.tool.description,
                "mode": "blocking",
            }
            if self.tool.max_attempts is not None:
                tool["maxAttempts"] = self.tool.max_attempts
            payload["tool"] = tool
        return payload


@dataclass(frozen=True, slots=True)
class Manifest:
    protocol_version: int
    task_queue: str
    lane_id: str | None
    capabilities: tuple[_CapabilityDefinition, ...]

    def to_payload(self) -> dict[str, Any]:
        payload: dict[str, Any] = {
            "protocolVersion": self.protocol_version,
            "taskQueue": self.task_queue,
            "capabilities": [
                capability.to_payload() for capability in self.capabilities
            ],
        }
        if self.lane_id is not None:
            payload["laneId"] = self.lane_id
        return payload


def build_manifest(
    task_queue: str,
    *,
    lane_id: str | None = None,
    workflows: Iterable[Capability] = (),
    activities: Iterable[Capability] = (),
) -> Manifest:
    if not isinstance(task_queue, str) or not task_queue.strip():
        raise ValueError("task_queue is required")

    if lane_id is not None:
        if not isinstance(lane_id, str):
            raise TypeError("lane_id must be a string")
        ksrpc.validate_lane(lane_id)

    definitions = (
        *(_to_definition("workflow", item) for item in workflows),
        *(_to_definition("activity", item) for item in activities),
    )
    _reject_duplicate_ids(definitions)
    return Manifest(
        protocol_version=MANIFEST_PROTOCOL_VERSION,
        task_queue=task_queue,
        lane_id=lane_id,
        capabilities=tuple(sorted(definitions, key=lambda definition: definition.id)),
    )


def _to_definition(
    kind: CapabilityKind,
    declared: Capability,
) -> _CapabilityDefinition:
    capability = _normalize_capability(declared, kind=kind)
    return _CapabilityDefinition(
        id=capability.id,
        kind=kind,
        temporal_type=capability.temporal_type,
        description=capability.description,
        input_schema=deepcopy(dict(capability.input_schema)),
        output_schema=deepcopy(dict(capability.output_schema)),
        timeout_seconds=_timeout_seconds(capability.timeout),
        tool=capability.tool,
    )


def _reject_duplicate_ids(
    definitions: tuple[_CapabilityDefinition, ...],
) -> None:
    capability_ids: set[str] = set()

    for definition in definitions:
        if definition.id in capability_ids:
            raise ValueError(f"duplicate capability id {definition.id!r}")
        capability_ids.add(definition.id)
