from __future__ import annotations

import getpass
import hashlib
import os
import re
import socket
from dataclasses import dataclass

from ksresolve import rpc as ksrpc

_WORKER_LANE_ENV = "KWS_LANE_ID"
_WORKER_STAGE_ENV = "KWS_SERVICE_STAGE"
_LOCAL_LANE_SALT = "agentk-local-lane-v1"
_LOCAL_SLUG_LIMIT = 32


@dataclass(frozen=True, slots=True)
class WorkerLane:
    base_task_queue: str
    lane: str | None

    @property
    def poll_task_queue(self) -> str:
        if self.lane is None:
            return self.base_task_queue
        return f"{self.base_task_queue}@{self.lane}"


def read_worker_lane(base_task_queue: str) -> WorkerLane:
    raw_lane = os.getenv(_WORKER_LANE_ENV, "")
    service_stage = os.getenv(_WORKER_STAGE_ENV, "")
    if raw_lane != "" or service_stage.strip() != "":
        lane = _instance_lane_from(raw_lane, service_stage, "", "")
        return WorkerLane(base_task_queue=base_task_queue, lane=lane)
    try:
        username = getpass.getuser()
        hostname = socket.gethostname()
    except OSError as error:
        raise ValueError(
            "cannot derive local worker lane: operating-system username and "
            f"hostname are required; set {_WORKER_LANE_ENV} explicitly"
        ) from error
    lane = _instance_lane_from(raw_lane, service_stage, username, hostname)
    return WorkerLane(base_task_queue=base_task_queue, lane=lane)


def _instance_lane_from(
    raw_lane: str,
    service_stage: str,
    username: str,
    hostname: str,
) -> str | None:
    if raw_lane != "":
        try:
            ksrpc.validate_lane(raw_lane)
        except ValueError as error:
            raise ValueError(f"invalid {_WORKER_LANE_ENV}: {error}") from error
        return raw_lane
    if service_stage.strip() != "":
        return None
    return _local_instance_lane(username, hostname)


def _local_instance_lane(username: str, hostname: str) -> str:
    if username.strip() == "" or hostname.strip() == "":
        raise ValueError(
            "cannot derive local worker lane: operating-system username and "
            f"hostname are required; set {_WORKER_LANE_ENV} explicitly"
        )
    slug = re.sub(r"[^a-z0-9]+", "-", username.lower()).strip("-")
    slug = slug[:_LOCAL_SLUG_LIMIT].rstrip("-") or "user"
    digest = hashlib.sha256(
        f"{_LOCAL_LANE_SALT}\0{username}\0{hostname}".encode()
    ).hexdigest()[:12]
    lane = f"local-{slug}-{digest}"
    try:
        ksrpc.validate_lane(lane)
    except ValueError as error:
        raise ValueError(f"cannot derive local worker lane: {error}") from error
    return lane
