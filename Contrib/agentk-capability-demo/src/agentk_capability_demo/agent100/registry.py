from __future__ import annotations

from dataclasses import asdict, dataclass
from importlib import import_module
from typing import NoReturn


@dataclass(frozen=True, slots=True)
class Agent100Definition:
    id: str
    name: str
    icon: str
    accent: str
    description: str
    worker_module: str


AGENTS = (
    Agent100Definition(
        id="narrato",
        name="Narrato",
        icon="N/A",
        accent="#ff6b48",
        description="纪录片解说脚本、配音与成片 Pipeline 测试台。",
        worker_module="agentk_capability_demo.agent100.narrato.worker",
    ),
)


def public_agents() -> list[dict[str, str]]:
    return [
        {key: value for key, value in asdict(agent).items() if key != "worker_module"}
        for agent in AGENTS
    ]


def run_worker(agent_id: str) -> NoReturn:
    definition = next((agent for agent in AGENTS if agent.id == agent_id), None)
    if definition is None:
        choices = ", ".join(agent.id for agent in AGENTS)
        raise ValueError(f"unknown Agent 100 id {agent_id!r}; choose one of: {choices}")
    module = import_module(definition.worker_module)
    module.main()
    raise RuntimeError(f"Agent 100 worker {agent_id!r} exited unexpectedly")
