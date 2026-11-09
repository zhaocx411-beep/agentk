from __future__ import annotations

import asyncio
from dataclasses import dataclass, field
from datetime import UTC, datetime


@dataclass(slots=True)
class RunRecord:
    session_id: str
    execution_id: str
    kind: str
    label: str
    created_at: str = field(default_factory=lambda: datetime.now(UTC).isoformat())


@dataclass(slots=True)
class AgentSession:
    session_id: str
    agent_id: str
    title: str
    model: str
    execution_id: str = ""
    created_at: str = field(default_factory=lambda: datetime.now(UTC).isoformat())


class MemoryStore:
    def __init__(self) -> None:
        self.runs: dict[str, RunRecord] = {}
        self.sessions: dict[str, AgentSession] = {}
        self.lock = asyncio.Lock()
