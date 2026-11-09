from __future__ import annotations

import asyncio
import os
from datetime import timedelta

from agentk.api.v1 import service_pb
from agentk.worker import Capability, Worker, connect_temporal
from temporalio.client import Client as TemporalClient
from temporalio.common import WorkflowIDConflictPolicy, WorkflowIDReusePolicy
from temporalio.worker.workflow_sandbox import SandboxedWorkflowRunner, SandboxRestrictions

from agentk_capability_demo.agent100.narrato.agentk_client import create_agentk_client
from agentk_capability_demo.agent100.narrato.agentk_core import (
    CORE_SCHEMA_PROPERTIES,
    DISCOVERABLE_CORE_CAPABILITY_IDS,
    REQUIRED_CORE_CAPABILITY_IDS,
    TTS_CORE_CAPABILITY_IDS,
)
from agentk_capability_demo.agent100.narrato.contracts import (
    CREATE_INPUT_SCHEMA,
    DOCUMENTARY_SCRIPT_SCHEMA,
    GENERATE_INPUT_SCHEMA,
    PIPELINE_OUTPUT_SCHEMA,
    RENDER_INPUT_SCHEMA,
)
from agentk_capability_demo.agent100.narrato.settings import Settings
from agentk_capability_demo.agent100.narrato.workflows import (
    ASR_RATE_LIMITER_WORKFLOW_TYPE,
    CREATE_CAPABILITY_ID,
    CREATE_WORKFLOW_TYPE,
    GENERATE_CAPABILITY_ID,
    GENERATE_WORKFLOW_TYPE,
    RENDER_CAPABILITY_ID,
    RENDER_WORKFLOW_TYPE,
    ASRRateLimiterWorkflow,
    CreateDocumentaryVideoWorkflow,
    GenerateDocumentaryScriptWorkflow,
    RenderDocumentaryVideoWorkflow,
    configure,
)


async def validate_core_capabilities(settings: Settings) -> None:
    discovered: dict[str, object] = {}
    page_token = ""
    async with create_agentk_client(settings.agentk_base_url, settings.agentk_api_key) as client:
        while True:
            response = await client.list_capabilities(
                service_pb.ListCapabilitiesRequest(page_size=100, page_token=page_token)
            )
            discovered.update(
                (capability.id, capability)
                for capability in response.capabilities
                if capability.id in DISCOVERABLE_CORE_CAPABILITY_IDS
            )
            page_token = response.next_page_token
            if not page_token:
                break
    missing = sorted(REQUIRED_CORE_CAPABILITY_IDS - discovered.keys())
    if missing:
        raise RuntimeError(f"AgentK Core capabilities are unavailable: {', '.join(missing)}")
    if not TTS_CORE_CAPABILITY_IDS.intersection(discovered):
        raise RuntimeError("AgentK Core has no supported TTS capability (Qwen or MiniMax)")
    _validate_core_schemas(discovered)
    _validate_model_schema_candidates(discovered, settings)


def _schema_properties(value: object) -> set[str]:
    converter = getattr(value, "to_python", None)
    schema = converter() if callable(converter) else value
    if not isinstance(schema, dict) or not isinstance(schema.get("properties"), dict):
        return set()
    return set(schema["properties"])


def _validate_core_schemas(discovered: dict[str, object]) -> None:
    incompatible: list[str] = []
    for capability_id, (expected_input, expected_output) in CORE_SCHEMA_PROPERTIES.items():
        if capability_id not in discovered:
            continue
        capability = discovered[capability_id]
        actual_input = _schema_properties(getattr(capability, "input_schema", None))
        actual_output = _schema_properties(getattr(capability, "output_schema", None))
        missing_input = sorted(expected_input - actual_input)
        missing_output = sorted(expected_output - actual_output)
        if missing_input or missing_output:
            incompatible.append(
                f"{capability_id} input={missing_input or '-'} output={missing_output or '-'}"
            )
    if incompatible:
        raise RuntimeError(
            "AgentK Core capability schemas are incompatible: " + "; ".join(incompatible)
        )


def _validate_model_schema_candidates(
    discovered: dict[str, object],
    settings: Settings,
) -> None:
    checks = {
        "agentk.understand_video_segment": settings.vision_model_candidates,
        "agentk.transcribe_audio": settings.asr_model_candidates,
        "agentk.web_search": settings.search_model_candidates,
    }
    incompatible: list[str] = []
    for capability_id, configured in checks.items():
        if capability_id not in discovered:
            continue
        advertised = _advertised_models(discovered[capability_id])
        if not advertised:
            continue
        missing = sorted(set(configured) - advertised)
        if missing:
            incompatible.append(f"{capability_id} models={missing}")
    for provider, capability_id in (
        ("qwen", "agentk.generate_speech_qwen"),
        ("minimax", "agentk.generate_speech_minimax"),
    ):
        if capability_id not in discovered:
            continue
        advertised = _advertised_models(discovered[capability_id])
        allowed = set(settings.tts_model_options()[provider])
        if advertised and advertised.isdisjoint(allowed):
            incompatible.append(f"{capability_id} models={sorted(allowed)}")
    if incompatible:
        raise RuntimeError(
            "AgentK Core model schemas do not advertise configured candidates: "
            + "; ".join(incompatible)
        )


def _advertised_models(capability: object) -> set[str]:
    input_schema = getattr(capability, "input_schema", None)
    converter = getattr(input_schema, "to_python", None)
    schema = converter() if callable(converter) else input_schema
    model_schema = (
        schema.get("properties", {}).get("model", {}) if isinstance(schema, dict) else {}
    )
    advertised = model_schema.get("enum") if isinstance(model_schema, dict) else None
    if not isinstance(advertised, list):
        return set()
    return {str(item) for item in advertised if str(item).strip()}


def build_worker(temporal: object, settings: Settings) -> Worker:
    configure(settings)
    worker = Worker(
        temporal,
        task_queue=settings.worker_task_queue,
        workflows=[
            ASRRateLimiterWorkflow,
            GenerateDocumentaryScriptWorkflow,
            RenderDocumentaryVideoWorkflow,
            CreateDocumentaryVideoWorkflow,
        ],
        workflow_runner=SandboxedWorkflowRunner(
            restrictions=SandboxRestrictions.default.with_passthrough_modules(
                "agentk_capability_demo.agent100.narrato.workflow_config"
            )
        ),
    )
    worker.publish_workflow(
        Capability(
            id=GENERATE_CAPABILITY_ID,
            temporal_type=GENERATE_WORKFLOW_TYPE,
            description="分析单个视频并生成结构化纪录片中文解说脚本。",
            input_schema=GENERATE_INPUT_SCHEMA,
            output_schema=DOCUMENTARY_SCRIPT_SCHEMA,
            timeout=timedelta(hours=3),
        )
    )
    worker.publish_workflow(
        Capability(
            id=RENDER_CAPABILITY_ID,
            temporal_type=RENDER_WORKFLOW_TYPE,
            description="按照可编辑的 OST 解说脚本生成配音、裁剪视频并合成字幕成片。",
            input_schema=RENDER_INPUT_SCHEMA,
            output_schema=PIPELINE_OUTPUT_SCHEMA,
            timeout=timedelta(hours=21),
        )
    )
    worker.publish_workflow(
        Capability(
            id=CREATE_CAPABILITY_ID,
            temporal_type=CREATE_WORKFLOW_TYPE,
            description="从单个视频自动生成纪录片解说脚本并渲染最终 MP4。",
            input_schema=CREATE_INPUT_SCHEMA,
            output_schema=PIPELINE_OUTPUT_SCHEMA,
            timeout=timedelta(hours=24),
        )
    )
    return worker


async def ensure_asr_rate_limiter(temporal: object, settings: Settings) -> None:
    await temporal.start_workflow(
        ASR_RATE_LIMITER_WORKFLOW_TYPE,
        {
            "minIntervalSeconds": settings.asr_min_interval_seconds,
            "globalConcurrency": settings.asr_global_concurrency,
            "leaseTimeoutSeconds": settings.asr_lease_timeout_seconds,
        },
        id=settings.asr_rate_limiter_workflow_id,
        task_queue=settings.worker_task_queue,
        id_reuse_policy=WorkflowIDReusePolicy.ALLOW_DUPLICATE,
        id_conflict_policy=WorkflowIDConflictPolicy.USE_EXISTING,
    )


async def connect_worker_temporal(settings: Settings) -> object:
    address = settings.temporal_address.strip()
    if address:
        return await TemporalClient.connect(
            address,
            namespace=settings.temporal_namespace.strip(),
        )
    return await connect_temporal()


async def run() -> None:
    settings = Settings.load()
    settings.validate_worker()
    # Temporal sandboxes re-import workflow modules. Mirror the configured Core
    # queue into the process environment so every sandbox resolves the same
    # deployment-stable Activity queue, including isolated local E2E runs.
    os.environ["NARRATO_CORE_TASK_QUEUE"] = settings.core_task_queue
    await validate_core_capabilities(settings)
    temporal = await connect_worker_temporal(settings)
    await ensure_asr_rate_limiter(temporal, settings)
    await build_worker(temporal, settings).run()


def main() -> None:
    asyncio.run(run())


if __name__ == "__main__":
    main()
