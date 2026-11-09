import asyncio
import base64
import logging
import random
import secrets
from collections.abc import Callable
from datetime import timedelta
from typing import Any

from temporalio.client import Client

from . import telemetry
from ._discovery import _CORE_TASK_QUEUE, REGISTER_WORKFLOW_TYPE
from ._telemetry import record_announce

_LOG = logging.getLogger(__name__)

_INTERVAL = timedelta(minutes=15)
_JITTER = timedelta(minutes=3)
_MAX_BACKOFF = timedelta(minutes=1)
_EXECUTION_TIMEOUT = timedelta(minutes=2)


async def announce_forever(
    get_client: Callable[[], Client],
    *,
    worker_task_queue: str,
    payload: dict[str, Any],
) -> None:
    """Report the manifest until cancelled.

    Announcing is a side channel: agent-k keeps the manifest under a TTL and a
    missed report only shortens its remaining life, so failures are logged and
    retried instead of taking the worker down.
    """
    delay = 0.0
    failures = 0
    while True:
        await asyncio.sleep(delay)
        try:
            await get_client().execute_workflow(
                REGISTER_WORKFLOW_TYPE,
                payload,
                id=_register_workflow_id(worker_task_queue),
                task_queue=_CORE_TASK_QUEUE,
                execution_timeout=_EXECUTION_TIMEOUT,
            )
        except Exception:  # noqa: BLE001 -- any announce failure is retried, never fatal
            failures += 1
            record_announce(telemetry.STATUS_FAILED)
            _LOG.warning(
                "agentk capability manifest announce failed",
                exc_info=True,
            )
            delay = _failure_delay(failures)
            continue
        record_announce(telemetry.STATUS_COMPLETED)
        failures = 0
        delay = _success_delay()


def _register_workflow_id(worker_task_queue: str) -> str:
    return (
        "agentk-discovery-register/"
        f"{_base64url(worker_task_queue.encode())}/"
        f"{_base64url(secrets.token_bytes(9))}"
    )


def _base64url(value: bytes) -> str:
    return base64.urlsafe_b64encode(value).decode().rstrip("=")


def _success_delay() -> float:
    jitter = _JITTER.total_seconds()
    return _INTERVAL.total_seconds() + random.uniform(-jitter, jitter)


def _failure_delay(attempt: int) -> float:
    cap = _MAX_BACKOFF.total_seconds()
    base = min(2.0 ** (max(attempt, 1) - 1), cap)
    return min(base * random.uniform(0.8, 1.2), cap)
