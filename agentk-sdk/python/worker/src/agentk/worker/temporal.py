from __future__ import annotations

import asyncio
import logging
import random
from dataclasses import dataclass
from typing import Any, Protocol

from ksresolve import kess
from temporalio.client import Client
from temporalio.contrib.opentelemetry import TracingInterceptor
from temporalio.converter import DataConverter
from temporalio.runtime import Runtime

from ._carrier import CarrierInterceptor

_TEMPORAL_KESS_SERVICE = "klingai-muse-temporal-server"
_TEMPORAL_NAMESPACE = "agentk"
_REFRESH_INTERVAL_SECONDS = 3.0

_LOG = logging.getLogger(__name__)


class _InstanceDesc(Protocol):
    host: str
    port: int


@dataclass(frozen=True)
class _Endpoint:
    host: str
    port: int

    @property
    def address(self) -> str:
        if ":" in self.host and not self.host.startswith("["):
            return f"[{self.host}]:{self.port}"
        return f"{self.host}:{self.port}"


class KessTemporalClient(Client):
    _agentk_kess_client: kess.Client
    _agentk_kess_endpoint: _Endpoint
    _agentk_data_converter: DataConverter
    _agentk_runtime: Runtime

    async def _replacement_if_needed(self) -> KessTemporalClient | None:
        endpoints = await asyncio.to_thread(_all_endpoints, self._agentk_kess_client)
        if self._agentk_kess_endpoint in endpoints:
            return None
        endpoint = _select_endpoint(endpoints)
        return await _dial(
            endpoint,
            discovery=self._agentk_kess_client,
            data_converter=self._agentk_data_converter,
            runtime=self._agentk_runtime,
        )


async def connect(
    *,
    data_converter: DataConverter = DataConverter.default,
) -> Client:
    """Connect to the AgentK Temporal namespace through KESS discovery.

    Args:
        data_converter: The Temporal payload converter configuration used by
            workflows and activities.

    Returns:
        A Temporal client connected to the AgentK namespace and suitable for an
        AgentK Worker.

    Raises:
        RuntimeError: If KESS does not return a Temporal endpoint.
    """
    discovery = await asyncio.to_thread(kess.Client)
    endpoints = await asyncio.to_thread(_all_endpoints, discovery)
    endpoint = _select_endpoint(endpoints)
    return await _dial(
        endpoint,
        discovery=discovery,
        data_converter=data_converter,
        runtime=Runtime.default(),
    )


async def refresh_worker_client_forever(worker: Any) -> None:
    while True:
        await asyncio.sleep(_REFRESH_INTERVAL_SECONDS)
        client = worker.client
        if not isinstance(client, KessTemporalClient):
            return
        try:
            replacement = await client._replacement_if_needed()
            if replacement is not None:
                worker.client = replacement
        except asyncio.CancelledError:
            raise
        except Exception:  # noqa: BLE001 -- discovery failures keep the current client
            _LOG.warning(
                "failed to refresh Temporal endpoint through KESS",
                exc_info=True,
            )


async def _dial(
    endpoint: _Endpoint,
    *,
    discovery: kess.Client,
    data_converter: DataConverter,
    runtime: Runtime,
) -> KessTemporalClient:
    client = await KessTemporalClient.connect(
        endpoint.address,
        namespace=_TEMPORAL_NAMESPACE,
        data_converter=data_converter,
        interceptors=[TracingInterceptor(), CarrierInterceptor()],
        runtime=runtime,
    )
    client._agentk_kess_client = discovery
    client._agentk_kess_endpoint = endpoint
    client._agentk_data_converter = data_converter
    client._agentk_runtime = runtime
    return client


def _select_endpoint(endpoints: tuple[_Endpoint, ...]) -> _Endpoint:
    if not endpoints:
        raise RuntimeError(f"KESS returned no endpoint for {_TEMPORAL_KESS_SERVICE!r}")
    return random.choice(endpoints)


def _all_endpoints(discovery: kess.Client) -> tuple[_Endpoint, ...]:
    snapshot = discovery.snapshot(kess.Target(_TEMPORAL_KESS_SERVICE, "grpc"))
    return tuple(_endpoint(desc) for desc in snapshot.endpoints if not desc.lane)


def _endpoint(desc: _InstanceDesc) -> _Endpoint:
    if not desc.host:
        raise ValueError("KESS endpoint host is empty")
    if desc.port <= 0 or desc.port > 65535:
        raise ValueError(f"KESS endpoint port is invalid: {desc.port}")
    return _Endpoint(desc.host, desc.port)
