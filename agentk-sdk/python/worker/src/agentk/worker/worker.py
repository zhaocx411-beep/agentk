import asyncio
import contextlib
import logging
from collections.abc import Callable, Sequence
from copy import deepcopy
from typing import Any, Self

from temporalio.client import Client
from temporalio.worker import Worker as TemporalWorker
from temporalio.worker.workflow_sandbox import (
    SandboxedWorkflowRunner,
    SandboxRestrictions,
)

from ._announce import announce_forever
from ._carrier import with_carrier_interceptor
from ._discovery import _CORE_TASK_QUEUE, describe_activity
from ._manifest import Manifest, build_manifest
from ._telemetry import with_telemetry_interceptor
from ._worker_lane import read_worker_lane
from .capability import Capability
from .invocation import _with_invocation_interceptor
from .temporal import KessTemporalClient, refresh_worker_client_forever

_LOGGER = logging.getLogger(__name__)

# protobuf-py backs its descriptors with a C extension, which cannot be
# re-imported inside the Temporal workflow sandbox; ksresolve carries it.
# opentelemetry and the SDK's own instruments must be the host's instances so
# that metrics recorded from workflow code reach the globally configured
# MeterProvider instead of a sandbox copy.
_PASSTHROUGH_MODULES = (
    "ksresolve",
    "protobuf",
    "opentelemetry",
    "agentk.worker._telemetry",
)


class Worker(TemporalWorker):
    """A Temporal Worker that publishes selected AgentK capabilities.

    Register workflow and activity implementations through the native Temporal
    constructor arguments, then call publish_workflow or publish_activity before
    running the Worker. Publishing a capability does not register its
    implementation.

    Running the Worker freezes and validates its capability declarations. The
    Worker announces those declarations in the background until it stops;
    announcement failures are retried without stopping workflow or activity
    processing.

    Args:
        client: The Temporal client used by the Worker.
        task_queue: The contributor-owned Temporal task queue. The AgentK core
            task queue cannot be used.
        workflows: Workflow implementations registered with Temporal.
        activities: Activity implementations registered with Temporal.
        **worker_options: Additional native Temporal Worker options.
    """

    def __init__(
        self,
        client: Client,
        *,
        task_queue: str,
        workflows: Sequence[type] = (),
        activities: Sequence[Callable] = (),
        **worker_options: Any,
    ) -> None:
        self._workflows: list[Capability] = []
        self._activities: list[Capability] = []
        self._frozen: Manifest | None = None
        self._announcer: asyncio.Task[None] | None = None
        self._client_refresher: asyncio.Task[None] | None = None
        self._route_logged = False
        self._worker_lane = read_worker_lane(task_queue)
        interceptors = with_telemetry_interceptor(
            with_carrier_interceptor(
                _with_invocation_interceptor(worker_options.pop("interceptors", ()))
            ),
            self._is_capability,
        )
        if "workflow_runner" not in worker_options:
            worker_options["workflow_runner"] = SandboxedWorkflowRunner(
                restrictions=SandboxRestrictions.default.with_passthrough_modules(
                    *_PASSTHROUGH_MODULES
                )
            )
        super().__init__(
            client,
            task_queue=self._worker_lane.poll_task_queue,
            workflows=workflows,
            activities=[*activities, describe_activity(self._frozen_payload)],
            interceptors=interceptors,
            **worker_options,
        )

    def publish_workflow(self, capability: Capability) -> None:
        """Publish an already registered workflow as an AgentK capability.

        Args:
            capability: The public contract for the registered workflow.

        Raises:
            RuntimeError: If the Worker has already started and its capability
                declarations have been frozen.
        """
        self._ensure_mutable()
        self._workflows.append(deepcopy(capability))

    def publish_activity(self, capability: Capability) -> None:
        """Publish an already registered activity as an AgentK capability.

        Args:
            capability: The public contract for the registered activity.

        Raises:
            RuntimeError: If the Worker has already started and its capability
                declarations have been frozen.
        """
        self._ensure_mutable()
        self._activities.append(deepcopy(capability))

    async def run(self) -> None:
        """Validate, publish, and run the Worker until it is shut down.

        Capability publication and KESS endpoint refresh stop when the native
        Temporal Worker returns.
        """
        self._log_route_once()
        self._start_client_refresher()
        self._start_announcing()
        try:
            await super().run()
        finally:
            await self._stop_client_refresher()
            await self._stop_announcing()

    async def shutdown(self) -> None:
        """Stop capability publication before shutting down the Worker."""
        await self._stop_client_refresher()
        await self._stop_announcing()
        await super().shutdown()

    async def __aenter__(self) -> Self:
        """Validate and freeze capabilities before entering the Worker context."""
        if self._frozen is None:
            self._frozen = self._build()
        self._log_route_once()
        return await super().__aenter__()

    def _log_route_once(self) -> None:
        if self._route_logged:
            return
        self._route_logged = True
        lane = str(self._worker_lane.lane) if self._worker_lane.lane else "baseline"
        _LOGGER.info(
            "agentk worker route (lane=%s, base_task_queue=%s, task_queue=%s)",
            lane,
            self._worker_lane.base_task_queue,
            self._worker_lane.poll_task_queue,
        )

    def _ensure_mutable(self) -> None:
        if self._frozen is not None:
            raise RuntimeError("capability manifest is frozen after the worker starts")

    def _build(self) -> Manifest:
        manifest = build_manifest(
            self._worker_lane.base_task_queue,
            lane_id=(
                str(self._worker_lane.lane)
                if self._worker_lane.lane is not None
                else None
            ),
            workflows=self._workflows,
            activities=self._activities,
        )
        if self._worker_lane.base_task_queue == _CORE_TASK_QUEUE:
            raise ValueError(
                f"contributor worker task queue must not be {_CORE_TASK_QUEUE!r}"
            )
        for definition in manifest.capabilities:
            if not definition.id.startswith("contrib."):
                raise ValueError(
                    "contributor worker capability id "
                    f"{definition.id!r} must match contrib.<owner>.<name>"
                )
        return manifest

    def _is_capability(self, kind: str, temporal_type: str) -> bool:
        if self._frozen is None:
            return False
        return any(
            definition.kind == kind and definition.temporal_type == temporal_type
            for definition in self._frozen.capabilities
        )

    def _frozen_payload(self) -> dict[str, Any]:
        if self._frozen is None:
            raise RuntimeError(
                "capability manifest is not frozen until the worker runs"
            )
        return self._frozen.to_payload()

    def _start_announcing(self) -> None:
        if self._frozen is None:
            self._frozen = self._build()
        manifest = self._frozen
        if self._announcer is not None:
            return
        self._announcer = asyncio.create_task(
            announce_forever(
                lambda: self.client,
                worker_task_queue=self._worker_lane.base_task_queue,
                payload=manifest.to_payload(),
            )
        )

    def _start_client_refresher(self) -> None:
        if not isinstance(self.client, KessTemporalClient):
            return
        if self._client_refresher is not None:
            return
        self._client_refresher = asyncio.create_task(
            refresh_worker_client_forever(self)
        )

    async def _stop_client_refresher(self) -> None:
        refresher = self._client_refresher
        self._client_refresher = None
        if refresher is None:
            return
        refresher.cancel()
        with contextlib.suppress(asyncio.CancelledError):
            await refresher

    async def _stop_announcing(self) -> None:
        announcer = self._announcer
        self._announcer = None
        if announcer is None:
            return
        announcer.cancel()
        with contextlib.suppress(asyncio.CancelledError):
            await announcer
