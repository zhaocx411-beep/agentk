"""Startup-stable configuration shared with Temporal workflow sandboxes.

The worker passes this module through Temporal's sandbox importer. This keeps
deployment-selected allowlists, concurrency, and task queues identical in the
host worker and every replay without reading files or environment variables
from workflow code.
"""

from __future__ import annotations

from agentk_capability_demo.agent100.narrato.settings import Settings

_SETTINGS = Settings()


def configure(settings: Settings) -> None:
    global _SETTINGS
    _SETTINGS = settings


def get_settings() -> Settings:
    return _SETTINGS
