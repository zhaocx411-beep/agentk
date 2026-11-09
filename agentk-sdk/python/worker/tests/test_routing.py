import json
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import pytest
from jsonschema import Draft202012Validator
from temporalio.workflow import (
    ChildWorkflowCancellationType,
    ParentClosePolicy,
)

from agentk.worker import _routing as routing_module
from agentk.worker import invoke_capability

_PROTOCOL_ROOT = (
    Path(__file__).resolve().parents[3] / "protocol" / "agentk" / "capability" / "v1"
)


@dataclass
class Result:
    value: str


def load_json(path: Path) -> dict[str, Any]:
    return json.loads(path.read_text())


def test_invoke_input_fixture_conforms_to_shared_schema() -> None:
    payload = load_json(_PROTOCOL_ROOT / "testdata" / "invoke-input.json")
    schema = load_json(_PROTOCOL_ROOT / "invoke-input.schema.json")
    Draft202012Validator(schema).validate(payload)


async def test_invoke_capability_executes_core_workflow(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    calls: list[tuple[str, Any, dict[str, Any]]] = []

    async def execute_child_workflow(name: str, arg: Any, **options: Any) -> Any:
        calls.append((name, arg, options))
        return Result(value="done")

    monkeypatch.setattr(
        routing_module.workflow,
        "execute_child_workflow",
        execute_child_workflow,
    )

    output = await invoke_capability(
        "contrib.demo.target",
        {"value": "input"},
        result_type=Result,
    )

    assert output == Result(value="done")
    assert calls == [
        (
            routing_module._INVOKE_WORKFLOW_TYPE,
            {
                "capabilityId": "contrib.demo.target",
                "input": {"value": "input"},
            },
            {
                "task_queue": "agentk",
                "result_type": Result,
                "cancellation_type": (
                    ChildWorkflowCancellationType.WAIT_CANCELLATION_COMPLETED
                ),
                "parent_close_policy": ParentClosePolicy.REQUEST_CANCEL,
            },
        )
    ]
