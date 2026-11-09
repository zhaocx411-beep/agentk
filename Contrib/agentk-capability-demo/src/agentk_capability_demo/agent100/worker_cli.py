from __future__ import annotations

import argparse

from .registry import AGENTS, run_worker


def main() -> None:
    parser = argparse.ArgumentParser(description="Run one isolated Agent 100 worker")
    parser.add_argument("agent", choices=[agent.id for agent in AGENTS])
    args = parser.parse_args()
    run_worker(args.agent)


if __name__ == "__main__":
    main()
