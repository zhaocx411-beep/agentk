from __future__ import annotations

import asyncio
import os
from datetime import timedelta

from agentk.api.v1 import service_pb
from agentk.worker import Capability, Worker, connect_temporal

from narrato_agentk.agentk_client import create_agentk_client
from narrato_agentk.agentk_core import (
    CORE_SCHEMA_PROPERTIES,
    DISCOVERABLE_CORE_CAPABILITY_IDS,
)
from narrato_agentk.contracts import (
    CREATE_INPUT_SCHEMA,
    DOCUMENTARY_SCRIPT_SCHEMA,
    GENERATE_INPUT_SCHEMA,
    PIPELINE_OUTPUT_SCHEMA,
    RENDER_INPUT_SCHEMA,
)
from narrato_agentk.settings import Settings
from narrato_agentk.workflows import (
    CREATE_CAPABILITY_ID,
    CREATE_WORKFLOW_TYPE,
    GENERATE_CAPABILITY_ID,
    GENERATE_WORKFLOW_TYPE,
    RENDER_CAPABILITY_ID,
    RENDER_WORKFLOW_TYPE,
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
    missing = sorted(DISCOVERABLE_CORE_CAPABILITY_IDS - discovered.keys())
    if missing:
        raise RuntimeError(f"AgentK Core capabilities are unavailable: {', '.join(missing)}")
    _validate_core_schemas(discovered)


def _schema_properties(value: object) -> set[str]:
    converter = getattr(value, "to_python", None)
    schema = converter() if callable(converter) else value
    if not isinstance(schema, dict) or not isinstance(schema.get("properties"), dict):
        return set()
    return set(schema["properties"])


def _validate_core_schemas(discovered: dict[str, object]) -> None:
    incompatible: list[str] = []
    for capability_id, (expected_input, expected_output) in CORE_SCHEMA_PROPERTIES.items():
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


def build_worker(temporal: object, settings: Settings) -> Worker:
    configure(settings)
    worker = Worker(
        temporal,
        task_queue=settings.worker_task_queue,
        workflows=[
            GenerateDocumentaryScriptWorkflow,
            RenderDocumentaryVideoWorkflow,
            CreateDocumentaryVideoWorkflow,
        ],
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


async def run() -> None:
    settings = Settings.load()
    settings.validate_worker()
    # Temporal sandboxes re-import workflow modules. Mirror the configured Core
    # queue into the process environment so every sandbox resolves the same
    # deployment-stable Activity queue, including isolated local E2E runs.
    os.environ["NARRATO_CORE_TASK_QUEUE"] = settings.core_task_queue
    await validate_core_capabilities(settings)
    temporal = await connect_temporal()
    await build_worker(temporal, settings).run()


def main() -> None:
    asyncio.run(run())


if __name__ == "__main__":
    main()
