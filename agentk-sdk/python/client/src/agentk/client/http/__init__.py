from __future__ import annotations

import os
from collections.abc import Callable

from agentk.api.v1.service_connect import AgentKServiceClient

from .._core import AsyncClient, Options, _validate_endpoint
from .._provider import OwnedClient

_STAGING_GATEWAY = "https://agentk-api.staging.kuaishou.com"
_PRODUCTION_GATEWAY = "http://agentk-api.internal"


def create_client(options: Options) -> AsyncClient:
    """Create an AgentK HTTP client using an override or environment gateway."""
    endpoint = options.endpoint or _gateway_url(os.getenv)
    rpc = AgentKServiceClient(_validate_endpoint(endpoint))
    return AsyncClient(OwnedClient(rpc))


def _gateway_url(getenv: Callable[[str], str | None]) -> str:
    stage = (getenv("KWS_SERVICE_STAGE") or "").strip().upper()
    if stage in {"STAGING", "CANDIDATE"}:
        return _STAGING_GATEWAY
    return _PRODUCTION_GATEWAY


__all__ = ["create_client"]
