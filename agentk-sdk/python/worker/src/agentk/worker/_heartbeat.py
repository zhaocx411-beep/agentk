import asyncio
import contextlib
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from datetime import timedelta

from temporalio import activity

from ._telemetry import record_heartbeat
from .invocation import Meta, _activity_meta

_DEFAULT_INTERVAL_SECONDS = 10.0


@asynccontextmanager
async def auto_heartbeat() -> AsyncIterator[None]:
    """Keep the current async Temporal Activity heartbeating within the block.

    The context manager records a heartbeat immediately, continues at an
    interval derived from the Activity heartbeat timeout, and stops before the
    block exits. Use it only from an asynchronous Temporal Activity.

    Each heartbeat is counted in agentk.worker.heartbeats, attributed to the
    calling app when the Activity carries AgentK invocation meta.

    Yields:
        Control to the Activity body while automatic heartbeats are active.
    """
    interval = _interval_seconds(activity.info().heartbeat_timeout)
    meta = _activity_meta()
    _heartbeat(meta)
    task = asyncio.create_task(_heartbeat_loop(interval, meta))
    try:
        yield
    finally:
        task.cancel()
        with contextlib.suppress(asyncio.CancelledError):
            await task


def _heartbeat(meta: Meta | None) -> None:
    activity.heartbeat()
    record_heartbeat(meta)


async def _heartbeat_loop(interval: float, meta: Meta | None) -> None:
    while True:
        await asyncio.sleep(interval)
        _heartbeat(meta)


def _interval_seconds(timeout: timedelta | None) -> float:
    if timeout is None:
        return _DEFAULT_INTERVAL_SECONDS
    candidate = timeout.total_seconds() / 2
    if candidate <= 0 or candidate >= _DEFAULT_INTERVAL_SECONDS:
        return _DEFAULT_INTERVAL_SECONDS
    return candidate
