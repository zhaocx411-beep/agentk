from __future__ import annotations

import asyncio
import os
from types import SimpleNamespace
from typing import Any

import pytest
from ksresolve import kess
from temporalio.api.workflowservice.v1 import DescribeNamespaceRequest
from temporalio.converter import DataConverter
from temporalio.runtime import Runtime

from agentk.worker import temporal
from agentk.worker.temporal import (
    KessTemporalClient,
    _all_endpoints,
    _Endpoint,
    _endpoint,
    _select_endpoint,
    connect,
    refresh_worker_client_forever,
)


class FakeDiscovery:
    def __init__(self, *addresses: tuple[str, int]) -> None:
        self.addresses = list(addresses)
        self.calls = 0

    def snapshot(self, target):
        self.calls += 1
        assert target == kess.Target("klingai-muse-temporal-server", "grpc")
        return kess.Snapshot(
            target,
            self.calls,
            tuple(
                kess.Endpoint(host, host, "grpc", port, 1)
                for host, port in self.addresses
            ),
        )


def test_discovery_endpoints_are_extracted() -> None:
    discovery = FakeDiscovery(("10.0.0.1", 7233), ("10.0.0.2", 7233))

    endpoints = _all_endpoints(discovery)
    assert _select_endpoint(endpoints) in endpoints
    assert set(endpoints) == {
        _Endpoint("10.0.0.1", 7233),
        _Endpoint("10.0.0.2", 7233),
    }


def test_endpoint_formats_ipv6() -> None:
    assert _Endpoint("2001:db8::1", 7233).address == "[2001:db8::1]:7233"


@pytest.mark.parametrize(
    "desc",
    [
        SimpleNamespace(host="", port=7233),
        SimpleNamespace(host="10.0.0.1", port=0),
        SimpleNamespace(host="10.0.0.1", port=65536),
    ],
)
def test_endpoint_rejects_invalid_kess_instances(desc: Any) -> None:
    with pytest.raises(ValueError):
        _endpoint(desc)


async def test_client_is_replaced_only_after_its_endpoint_disappears(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    discovery = FakeDiscovery(("10.0.0.1", 7233))
    client = object.__new__(KessTemporalClient)
    client._agentk_kess_client = discovery
    client._agentk_kess_endpoint = _Endpoint("10.0.0.1", 7233)
    client._agentk_data_converter = object()  # type: ignore[assignment]
    client._agentk_runtime = object()  # type: ignore[assignment]

    assert await client._replacement_if_needed() is None

    replacement = object()
    dial_options: dict[str, Any] = {}

    async def dial(*args: Any, **kwargs: Any) -> Any:
        dial_options.update(kwargs)
        return replacement

    monkeypatch.setattr(temporal, "_dial", dial)
    discovery.addresses = [("10.0.0.2", 7233)]

    assert await client._replacement_if_needed() is replacement
    assert dial_options["data_converter"] is client._agentk_data_converter
    assert dial_options["runtime"] is client._agentk_runtime
    assert discovery.calls == 2


async def test_worker_refresher_applies_replacement(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    replaced = asyncio.Event()

    class FakeKessClient:
        def __init__(self, replacement: FakeKessClient | None = None) -> None:
            self.replacement = replacement

        async def _replacement_if_needed(self) -> FakeKessClient | None:
            replacement = self.replacement
            self.replacement = None
            return replacement

    class FakeWorker:
        def __init__(self, client: FakeKessClient) -> None:
            self._client = client

        @property
        def client(self) -> FakeKessClient:
            return self._client

        @client.setter
        def client(self, client: FakeKessClient) -> None:
            self._client = client
            replaced.set()

    second = FakeKessClient()
    worker = FakeWorker(FakeKessClient(second))
    monkeypatch.setattr(temporal, "KessTemporalClient", FakeKessClient)
    monkeypatch.setattr(temporal, "_REFRESH_INTERVAL_SECONDS", 0.0)

    task = asyncio.create_task(refresh_worker_client_forever(worker))
    await replaced.wait()
    task.cancel()
    with pytest.raises(asyncio.CancelledError):
        await task

    assert worker.client is second


async def test_connect_discovers_off_loop_and_selects_only_baseline(monkeypatch):
    import threading

    main_thread = threading.get_ident()
    target = kess.Target("klingai-muse-temporal-server", "grpc")

    class Discovery:
        def __init__(self):
            assert threading.get_ident() != main_thread

        def snapshot(self, actual):
            assert threading.get_ident() != main_thread
            assert actual == target
            return kess.Snapshot(
                target,
                1,
                (
                    kess.Endpoint("dev", "dev", "grpc", 7233, 1, lane="dev"),
                    kess.Endpoint("base", "::1", "grpc", 7233, 1),
                ),
            )

    async def dial(endpoint, **kwargs):
        assert endpoint.address == "[::1]:7233"
        assert kwargs["data_converter"] is converter
        return sentinel

    sentinel = object()
    converter = object()
    monkeypatch.setattr(kess, "Client", Discovery)
    monkeypatch.setattr(temporal, "_dial", dial)
    assert await connect(data_converter=converter) is sentinel


async def test_initial_empty_discovery_fails(monkeypatch):
    discovery = FakeDiscovery()
    monkeypatch.setattr(kess, "Client", lambda: discovery)
    with pytest.raises(RuntimeError, match="no endpoint"):
        await connect()


@pytest.mark.parametrize("failure", ["discovery", "empty", "dial"])
async def test_refresh_failure_keeps_client_and_recovers(monkeypatch, failure):
    discovery = FakeDiscovery(("old", 7233))
    client = object.__new__(KessTemporalClient)
    client._agentk_kess_client = discovery
    client._agentk_kess_endpoint = _Endpoint("old", 7233)
    client._agentk_data_converter = DataConverter.default
    client._agentk_runtime = Runtime.default()
    worker = SimpleNamespace(client=client)
    attempted = asyncio.Event()
    recovered = asyncio.Event()
    loop = asyncio.get_running_loop()
    original_snapshot = discovery.snapshot
    failing = True
    sentinel = object()

    def snapshot(target):
        if failing:
            loop.call_soon_threadsafe(attempted.set)
            if failure == "discovery":
                raise OSError("discovery unavailable")
            if failure == "empty":
                discovery.addresses = []
            else:
                discovery.addresses = [("new", 7233)]
        else:
            discovery.addresses = [("new", 7233)]
        return original_snapshot(target)

    async def dial(*args, **kwargs):
        if failing:
            raise OSError("connection failed")
        recovered.set()
        return sentinel

    discovery.snapshot = snapshot
    monkeypatch.setattr(temporal, "_dial", dial)
    monkeypatch.setattr(temporal, "_REFRESH_INTERVAL_SECONDS", 0.01)
    task = asyncio.create_task(refresh_worker_client_forever(worker))
    try:
        await asyncio.wait_for(attempted.wait(), 2)
        assert worker.client is client
        failing = False
        await asyncio.wait_for(recovered.wait(), 2)
        await task
        assert worker.client is sentinel
    finally:
        task.cancel()
        if not task.done():
            with pytest.raises(asyncio.CancelledError):
                await task


@pytest.mark.e2e
@pytest.mark.skipif(
    os.getenv("AGENTK_SDK_TEST_KESS") != "1",
    reason="AGENTK_SDK_TEST_KESS is not enabled",
)
async def test_kess_temporal_integration() -> None:
    async with asyncio.timeout(20):
        client = await connect()

        response = await client.workflow_service.describe_namespace(
            DescribeNamespaceRequest(namespace="agentk")
        )

    assert response.namespace_info.name == "agentk"
