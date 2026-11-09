from types import SimpleNamespace
from typing import Any

import pytest
from temporalio.worker import Worker as TemporalWorker

from narrato_agentk.contracts import (
    CREATE_INPUT_SCHEMA,
    DOCUMENTARY_SCRIPT_SCHEMA,
    GENERATE_INPUT_SCHEMA,
    PIPELINE_OUTPUT_SCHEMA,
    RENDER_INPUT_SCHEMA,
)
from narrato_agentk.settings import Settings
from narrato_agentk.worker import CORE_SCHEMA_PROPERTIES, _validate_core_schemas, build_worker
from narrato_agentk.workflows import documentary


@pytest.fixture
def fake_temporal_worker(monkeypatch):
    def init(
        self: TemporalWorker,
        client: Any,
        *,
        task_queue: str,
        workflows: list[type],
        activities: list[Any],
        **options: Any,
    ) -> None:
        self._config = {
            "client": client,
            "task_queue": task_queue,
            "workflows": list(workflows),
            "activities": list(activities),
            **options,
        }

    monkeypatch.setattr(TemporalWorker, "__init__", init)


def test_worker_publishes_three_pipeline_manifests(fake_temporal_worker):
    worker = build_worker(object(), Settings(agentk_api_key="secret"))
    payload = worker._build().to_payload()

    assert payload["taskQueue"] == "contrib-narrato-documentary-v1"
    assert {item["id"] for item in payload["capabilities"]} == {
        "contrib.narrato.generate_documentary_script",
        "contrib.narrato.render_documentary_video",
        "contrib.narrato.create_documentary_video",
    }
    assert all(item["kind"] == "workflow" for item in payload["capabilities"])
    by_id = {item["id"]: item for item in payload["capabilities"]}
    assert by_id["contrib.narrato.generate_documentary_script"] == {
        **by_id["contrib.narrato.generate_documentary_script"],
        "temporalType": "narrato.documentary.generate_script.v1",
        "inputSchema": GENERATE_INPUT_SCHEMA,
        "outputSchema": DOCUMENTARY_SCRIPT_SCHEMA,
        "timeoutSeconds": 3 * 60 * 60,
    }
    assert by_id["contrib.narrato.render_documentary_video"]["inputSchema"] == (RENDER_INPUT_SCHEMA)
    assert by_id["contrib.narrato.render_documentary_video"]["outputSchema"] == (
        PIPELINE_OUTPUT_SCHEMA
    )
    assert by_id["contrib.narrato.render_documentary_video"]["timeoutSeconds"] == 21 * 60 * 60
    assert by_id["contrib.narrato.create_documentary_video"]["inputSchema"] == (CREATE_INPUT_SCHEMA)
    assert by_id["contrib.narrato.create_documentary_video"]["outputSchema"] == (
        PIPELINE_OUTPUT_SCHEMA
    )
    assert by_id["contrib.narrato.create_documentary_video"]["timeoutSeconds"] == 24 * 60 * 60


def test_worker_rejects_incompatible_core_schema():
    capabilities = {
        capability_id: SimpleNamespace(
            input_schema={"properties": {name: {} for name in input_properties}},
            output_schema={"properties": {name: {} for name in output_properties}},
        )
        for capability_id, (input_properties, output_properties) in CORE_SCHEMA_PROPERTIES.items()
    }
    _validate_core_schemas(capabilities)

    capabilities["agentk.generate_text"].output_schema = {"properties": {}}
    with pytest.raises(RuntimeError, match="agentk.generate_text"):
        _validate_core_schemas(capabilities)


def test_workflow_configuration_sets_core_activity_queue():
    documentary.configure(Settings(core_task_queue="agentk-local-e2e"))
    try:
        assert documentary.CORE_TASK_QUEUE == "agentk-local-e2e"
    finally:
        documentary.configure(Settings())
