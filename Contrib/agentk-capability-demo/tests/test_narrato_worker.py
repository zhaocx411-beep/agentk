import re
from types import SimpleNamespace
from typing import Any

import pytest
from temporalio import workflow
from temporalio.worker import Worker as TemporalWorker

from agentk_capability_demo.agent100.narrato import worker as worker_module
from agentk_capability_demo.agent100.narrato.contracts import (
    CREATE_INPUT_SCHEMA,
    DOCUMENTARY_SCRIPT_SCHEMA,
    GENERATE_INPUT_SCHEMA,
    PIPELINE_OUTPUT_SCHEMA,
    RENDER_INPUT_SCHEMA,
)
from agentk_capability_demo.agent100.narrato.settings import Settings
from agentk_capability_demo.agent100.narrato.worker import (
    CORE_SCHEMA_PROPERTIES,
    _validate_core_schemas,
    _validate_model_schema_candidates,
    build_worker,
    connect_worker_temporal,
    ensure_asr_rate_limiter,
)
from agentk_capability_demo.agent100.narrato.workflows import ASRRateLimiterWorkflow, documentary


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

    assert payload["taskQueue"] == "demo-worker"
    capability_ids = {item["id"] for item in payload["capabilities"]}
    assert capability_ids == {
        "contrib.narrato.generate_documentary_script",
        "contrib.narrato.render_documentary_video",
        "contrib.narrato.create_documentary_video",
    }
    assert all(
        re.fullmatch(r"contrib\.[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*", capability_id)
        for capability_id in capability_ids
    )
    assert all(item["kind"] == "workflow" for item in payload["capabilities"])
    by_id = {item["id"]: item for item in payload["capabilities"]}
    assert by_id["contrib.narrato.generate_documentary_script"] == {
        **by_id["contrib.narrato.generate_documentary_script"],
        "temporalType": "narrato.documentary.generate_script.v2",
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
    assert "agentk_capability_demo.agent100.narrato.workflow_config" in (
        worker._config["workflow_runner"].restrictions.passthrough_modules
    )
    assert ASRRateLimiterWorkflow in worker._config["workflows"]


async def test_all_workflows_load_in_the_configured_temporal_sandbox(fake_temporal_worker):
    worker = build_worker(object(), Settings(agentk_api_key="secret"))
    runner = worker._config["workflow_runner"]
    for workflow_class in worker._config["workflows"]:
        runner.prepare_workflow(workflow._Definition.must_from_class(workflow_class))


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


def test_worker_rejects_selected_model_not_advertised_by_core_schema():
    capabilities = {
        capability_id: SimpleNamespace(
            input_schema={"properties": {name: {} for name in input_properties}},
            output_schema={"properties": {name: {} for name in output_properties}},
        )
        for capability_id, (input_properties, output_properties) in CORE_SCHEMA_PROPERTIES.items()
    }
    capabilities["agentk.transcribe_audio"].input_schema["properties"]["model"] = {
        "enum": ["azure-whisper"]
    }
    capabilities["agentk.understand_video_segment"].input_schema["properties"]["model"] = {
        "enum": list(Settings().vision_model_candidates)
    }
    capabilities["agentk.generate_speech_qwen"].input_schema["properties"]["model"] = {
        "enum": ["qwen3-tts-flash"]
    }
    capabilities["agentk.generate_speech_minimax"].input_schema["properties"]["model"] = {
        "enum": list(Settings().minimax_tts_model_candidates)
    }
    _validate_model_schema_candidates(capabilities, Settings())

    bad = Settings(asr_model="asr-not-supported", asr_model_candidates=("asr-not-supported",))
    with pytest.raises(RuntimeError, match="asr-not-supported"):
        _validate_model_schema_candidates(capabilities, bad)


def test_worker_accepts_any_allowed_qwen_model_advertised_by_core_schema():
    capabilities = {
        "agentk.generate_speech_qwen": SimpleNamespace(
            input_schema={
                "properties": {
                    "model": {"enum": ["qwen3-tts-instruct-flash"]},
                }
            }
        )
    }

    _validate_model_schema_candidates(capabilities, Settings())

    capabilities["agentk.generate_speech_qwen"].input_schema["properties"]["model"] = {
        "enum": ["qwen3-tts-unknown"]
    }
    with pytest.raises(RuntimeError, match="agentk.generate_speech_qwen"):
        _validate_model_schema_candidates(capabilities, Settings())


def test_workflow_configuration_sets_core_activity_queue():
    documentary.configure(Settings(core_task_queue="agentk-local-e2e"))
    try:
        assert documentary.workflow_config.get_settings().core_task_queue == "agentk-local-e2e"
    finally:
        documentary.configure(Settings())


async def test_worker_connects_to_configured_local_temporal(monkeypatch):
    calls = []
    expected = object()

    async def fake_connect(address, *, namespace):
        calls.append((address, namespace))
        return expected

    monkeypatch.setattr(
        worker_module,
        "TemporalClient",
        SimpleNamespace(connect=fake_connect),
    )

    result = await connect_worker_temporal(
        Settings(
            temporal_address="127.0.0.1:7233",
            temporal_namespace="default",
        )
    )

    assert result is expected
    assert calls == [("127.0.0.1:7233", "default")]


async def test_worker_uses_kess_temporal_without_local_address(monkeypatch):
    expected = object()

    async def fake_connect_temporal():
        return expected

    monkeypatch.setattr(worker_module, "connect_temporal", fake_connect_temporal)

    assert await connect_worker_temporal(Settings()) is expected


async def test_worker_ensures_one_namespace_wide_asr_rate_limiter():
    calls = []

    class Temporal:
        async def start_workflow(self, workflow_type, initial_state, **options):
            calls.append((workflow_type, initial_state, options))

    settings = Settings(
        worker_task_queue="narrato-worker",
        asr_min_interval_seconds=11,
        asr_global_concurrency=1,
        asr_lease_timeout_seconds=900,
        asr_rate_limiter_workflow_id="staging-asr-limiter",
    )

    await ensure_asr_rate_limiter(Temporal(), settings)

    assert calls[0][0] == "narrato.asr.rate_limiter.v1"
    assert calls[0][1] == {
        "minIntervalSeconds": 11,
        "globalConcurrency": 1,
        "leaseTimeoutSeconds": 900,
    }
    assert calls[0][2]["id"] == "staging-asr-limiter"
    assert calls[0][2]["task_queue"] == "narrato-worker"
    assert calls[0][2]["id_conflict_policy"].name == "USE_EXISTING"
