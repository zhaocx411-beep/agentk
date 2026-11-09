import json
from pathlib import Path

import pytest

from agentk.worker._worker_lane import (
    _instance_lane_from,
    _local_instance_lane,
    read_worker_lane,
)

_FIXTURE = (
    Path(__file__).resolve().parents[3]
    / "protocol"
    / "agentk"
    / "worker_lane"
    / "v1"
    / "testdata"
    / "worker-instance-lanes.json"
)


@pytest.mark.parametrize(
    ("stage", "raw_lane", "expected"),
    [
        ("STAGING", "", None),
        ("PROD", "", None),
        ("PROD_REGRESSION_TEST", "", None),
        ("PROD_REGRESSION_TEST", "PRT", "PRT"),
        ("PROD_REGRESSION_TEST", "dev_example", "dev_example"),
        ("STAGING", "dev_example", "dev_example"),
        ("STAGING", "prt", "prt"),
    ],
)
def test_worker_lane_managed_stage(
    monkeypatch: pytest.MonkeyPatch, stage: str, raw_lane: str, expected: str | None
) -> None:
    monkeypatch.setenv("KWS_LANE_ID", raw_lane)
    monkeypatch.setenv("KWS_SERVICE_STAGE", stage)
    lane = read_worker_lane("example")
    assert lane.lane == expected
    assert lane.base_task_queue == "example"
    assert lane.poll_task_queue == (
        "example" if expected is None else f"example@{expected}"
    )


def test_worker_lane_rejects_invalid_env(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("KWS_LANE_ID", "dev lane")
    monkeypatch.setenv("KWS_SERVICE_STAGE", "PROD_REGRESSION_TEST")
    with pytest.raises(ValueError, match="KWS_LANE_ID"):
        read_worker_lane("example")


def test_explicit_lane_wins_over_managed_stage() -> None:
    assert _instance_lane_from("dev_example", "STAGING", "", "") == "dev_example"


def test_blank_stage_uses_local_lane() -> None:
    lane = _instance_lane_from("", " \t", "Yuchen", "dev-host")
    assert lane is not None
    assert lane.startswith("local-yuchen-")


@pytest.mark.parametrize(("username", "hostname"), [("", "dev-host"), ("Yuchen", "")])
def test_local_lane_requires_os_identity(username: str, hostname: str) -> None:
    with pytest.raises(ValueError, match="KWS_LANE_ID"):
        _instance_lane_from("", "", username, hostname)


def test_local_lane_matches_shared_fixture() -> None:
    fixture = json.loads(_FIXTURE.read_text())
    for case in fixture["cases"]:
        assert (
            _local_instance_lane(case["username"], case["hostname"]) == case["laneId"]
        )


def test_worker_lane_keeps_case(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("KWS_LANE_ID", "PRT")
    monkeypatch.setenv("KWS_SERVICE_STAGE", "PROD_REGRESSION_TEST")
    assert read_worker_lane("agentk").poll_task_queue == "agentk@PRT"
