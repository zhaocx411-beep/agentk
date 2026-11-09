from collections.abc import AsyncIterator

import pytest
from e2e.echo_workflow import EchoPipeline
from temporalio.testing import WorkflowEnvironment
from temporalio.worker import UnsandboxedWorkflowRunner
from temporalio.worker.workflow_sandbox import SandboxedWorkflowRunner

from agentk.worker import Worker


@pytest.fixture
async def temporal_env() -> AsyncIterator[WorkflowEnvironment]:
    async with await WorkflowEnvironment.start_time_skipping() as env:
        yield env


async def test_workflow_importing_agentk_loads_in_default_sandbox(
    temporal_env: WorkflowEnvironment,
) -> None:
    worker = Worker(
        temporal_env.client,
        task_queue="sandbox-test",
        workflows=[EchoPipeline],
    )

    assert isinstance(worker._config["workflow_runner"], SandboxedWorkflowRunner)


async def test_explicit_workflow_runner_is_kept(
    temporal_env: WorkflowEnvironment,
) -> None:
    runner = UnsandboxedWorkflowRunner()

    worker = Worker(
        temporal_env.client,
        task_queue="sandbox-test",
        workflows=[EchoPipeline],
        workflow_runner=runner,
    )

    assert worker._config["workflow_runner"] is runner
