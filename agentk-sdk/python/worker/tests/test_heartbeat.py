import asyncio
from datetime import timedelta
from types import SimpleNamespace

import pytest

from agentk.worker import _heartbeat as heartbeat_module
from agentk.worker import auto_heartbeat


def install_heartbeat(
    monkeypatch: pytest.MonkeyPatch,
    timeout: timedelta | None,
) -> list[None]:
    calls: list[None] = []
    monkeypatch.setattr(
        heartbeat_module.activity,
        "info",
        lambda: SimpleNamespace(heartbeat_timeout=timeout),
    )
    monkeypatch.setattr(
        heartbeat_module.activity,
        "heartbeat",
        lambda: calls.append(None),
    )
    return calls


async def test_auto_heartbeat_records_until_exit(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    calls = install_heartbeat(monkeypatch, timedelta(milliseconds=2))

    async with auto_heartbeat():
        while len(calls) < 2:
            await asyncio.sleep(0)

    stopped_at = len(calls)
    await asyncio.sleep(0.01)
    assert len(calls) == stopped_at


async def test_auto_heartbeat_cleans_up_after_body_error(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    calls = install_heartbeat(monkeypatch, None)

    with pytest.raises(RuntimeError, match="boom"):
        async with auto_heartbeat():
            raise RuntimeError("boom")

    stopped_at = len(calls)
    await asyncio.sleep(0)
    assert len(calls) == stopped_at == 1


def test_interval_uses_half_of_short_heartbeat_timeout() -> None:
    assert heartbeat_module._interval_seconds(None) == 10
    assert heartbeat_module._interval_seconds(timedelta(seconds=30)) == 10
    assert heartbeat_module._interval_seconds(timedelta(seconds=8)) == 4
