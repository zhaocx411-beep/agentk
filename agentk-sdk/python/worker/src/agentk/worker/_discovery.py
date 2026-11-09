"""Cross-language Temporal names used for capability manifest discovery."""

from collections.abc import Callable
from typing import Any

from temporalio import activity

REGISTER_WORKFLOW_TYPE = "agentk.discovery.register.v1"
DESCRIBE_ACTIVITY_TYPE = "agentk.capabilities.describe.v1"
_CORE_TASK_QUEUE = "agentk"


def describe_activity(payload: Callable[[], dict[str, Any]]) -> Callable[[], Any]:
    """Build the diagnostics activity returning the manifest frozen at startup."""

    @activity.defn(name=DESCRIBE_ACTIVITY_TYPE)
    async def describe() -> dict[str, Any]:
        return payload()

    return describe
