import json
from dataclasses import dataclass
from datetime import timedelta
from pathlib import Path
from types import SimpleNamespace
from typing import Any

import pytest
from jsonschema import Draft202012Validator
from temporalio.api.common.v1 import Payload

from agentk.worker import (
    InvocationKind,
    Meta,
    ReportProgressInput,
    current_meta,
    report_progress,
)
from agentk.worker import invocation as invocation_module

_PROTOCOL_ROOT = (
    Path(__file__).resolve().parents[3] / "protocol" / "agentk" / "invocation" / "v1"
)


def load_json(path: Path) -> dict[str, Any]:
    return json.loads(path.read_text())


def json_payload(value: Any) -> Payload:
    return Payload(
        metadata={"encoding": b"json/plain"},
        data=json.dumps(value, separators=(",", ":"), ensure_ascii=False).encode(),
    )


def meta_fixture() -> dict[str, Any]:
    return load_json(_PROTOCOL_ROOT / "testdata" / "meta.json")


def direct_meta_fixture() -> dict[str, Any]:
    return load_json(_PROTOCOL_ROOT / "testdata" / "meta-direct.json")


def legacy_meta_fixture() -> dict[str, Any]:
    value = meta_fixture()
    value["tenantId"] = value.pop("appId")
    return value


def partial_session_meta_fixture() -> dict[str, Any]:
    value = meta_fixture()
    del value["callbackTaskQueue"]
    return value


DIRECT_META = Meta(
    app_id="0198d72a-7c00-7000-8000-000000000001",
    invocation_id="inv_test",
    invocation_kind=InvocationKind.DIRECT,
    invocation_name="contrib.acme.summarize",
)


def install_headers(
    monkeypatch: pytest.MonkeyPatch,
    headers: dict[str, Payload],
) -> None:
    monkeypatch.setattr(
        invocation_module.workflow,
        "info",
        lambda: SimpleNamespace(headers=headers),
    )
    monkeypatch.setattr(
        invocation_module.workflow.unsafe, "is_replaying", lambda: False
    )


def test_invocation_values_match_shared_fixtures() -> None:
    meta = Meta(
        session_id=7,
        session="ses_test",
        app_id="0198d72a-7c00-7000-8000-000000000001",
        invocation_id="exe_test",
        invocation_kind=InvocationKind.PIPELINE,
        invocation_name="summarize",
        callback_task_queue="agentk-test",
    )
    progress = ReportProgressInput.from_meta(
        meta,
        "clip/0",
        "片段生成完成",
        {"videoUrl": "s3://example/clip-0.mp4"},
    )

    meta_payload = meta.to_payload()
    progress_payload = progress.to_payload()

    assert meta_payload == meta_fixture()
    assert progress_payload == load_json(
        _PROTOCOL_ROOT / "testdata" / "report-progress-input.json"
    )
    for name, payload in (
        ("meta", meta_payload),
        ("report-progress-input", progress_payload),
    ):
        schema = load_json(_PROTOCOL_ROOT / f"{name}.schema.json")
        Draft202012Validator(schema).validate(payload)


def test_direct_meta_matches_shared_fixture() -> None:
    payload = DIRECT_META.to_payload()

    assert payload == direct_meta_fixture()
    assert not DIRECT_META.has_session
    Draft202012Validator(
        load_json(_PROTOCOL_ROOT / "meta.schema.json")
    ).validate(payload)
    with pytest.raises(ValueError):
        ReportProgressInput.from_meta(DIRECT_META, "clip/0")


def test_meta_schema_rejects_partial_session() -> None:
    validator = Draft202012Validator(load_json(_PROTOCOL_ROOT / "meta.schema.json"))

    assert not validator.is_valid(partial_session_meta_fixture())


def test_current_meta_decodes_direct_header(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    install_headers(
        monkeypatch,
        {invocation_module.META_HEADER_KEY: json_payload(direct_meta_fixture())},
    )

    assert current_meta() == DIRECT_META


async def test_current_meta_in_activity_reads_interceptor_header(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setattr(invocation_module.activity, "in_activity", lambda: True)
    seen: list[Meta | None] = []

    class Next:
        async def execute_activity(self, input: Any) -> str:
            seen.append(current_meta())
            return "ok"

    inbound = invocation_module._InvocationActivityInbound(Next())  # type: ignore[arg-type]
    input = SimpleNamespace(
        headers={invocation_module.META_HEADER_KEY: json_payload(direct_meta_fixture())}
    )

    assert await inbound.execute_activity(input) == "ok"  # type: ignore[arg-type]
    assert seen == [DIRECT_META]
    assert current_meta() is None


def test_current_meta_decodes_go_compatible_json_header(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    install_headers(
        monkeypatch,
        {invocation_module.META_HEADER_KEY: json_payload(meta_fixture())},
    )

    assert current_meta() == Meta(
        session_id=7,
        session="ses_test",
        app_id="0198d72a-7c00-7000-8000-000000000001",
        invocation_id="exe_test",
        invocation_kind=InvocationKind.PIPELINE,
        invocation_name="summarize",
        callback_task_queue="agentk-test",
    )


@pytest.mark.parametrize(
    "headers",
    [
        {},
        {
            invocation_module.META_HEADER_KEY: Payload(
                metadata={"encoding": b"json/plain"},
                data=b"{not-json",
            )
        },
        {invocation_module.META_HEADER_KEY: json_payload({"sessionId": "wrong type"})},
        {invocation_module.META_HEADER_KEY: json_payload(legacy_meta_fixture())},
        {
            invocation_module.META_HEADER_KEY: json_payload(
                partial_session_meta_fixture()
            )
        },
    ],
)
def test_current_meta_ignores_missing_or_malformed_header(
    monkeypatch: pytest.MonkeyPatch,
    headers: dict[str, Payload],
) -> None:
    install_headers(monkeypatch, headers)

    assert current_meta() is None


async def test_report_progress_matches_go_activity_contract(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    install_headers(
        monkeypatch,
        {invocation_module.META_HEADER_KEY: json_payload(meta_fixture())},
    )
    calls: list[tuple[str, dict[str, Any], dict[str, Any]]] = []

    async def execute_activity(
        activity_type: str,
        payload: dict[str, Any],
        **options: Any,
    ) -> None:
        calls.append((activity_type, payload, options))

    monkeypatch.setattr(
        invocation_module.workflow,
        "execute_activity",
        execute_activity,
    )

    await report_progress(
        "clip/0",
        "片段生成完成",
        {"videoUrl": "s3://example/clip-0.mp4"},
    )

    assert len(calls) == 1
    activity_type, payload, options = calls[0]
    assert activity_type == invocation_module.REPORT_PROGRESS_ACTIVITY_TYPE
    assert payload == load_json(
        _PROTOCOL_ROOT / "testdata" / "report-progress-input.json"
    )
    assert options["task_queue"] == "agentk-test"
    assert options["start_to_close_timeout"] == timedelta(seconds=30)
    assert options["schedule_to_close_timeout"] == timedelta(minutes=2)
    retry = options["retry_policy"]
    assert retry.initial_interval == timedelta(seconds=1)
    assert retry.backoff_coefficient == 2
    assert retry.maximum_interval == timedelta(seconds=10)
    assert retry.maximum_attempts == 3
    assert set(retry.non_retryable_error_types) == {
        "NotFound",
        "InvalidArgument",
        "Configuration",
        "PermissionDenied",
        "UpstreamRequestRejected",
    }


async def test_report_progress_is_noop_without_meta(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    install_headers(monkeypatch, {})

    async def fail(*args: Any, **kwargs: Any) -> None:
        raise AssertionError("progress activity must not start")

    monkeypatch.setattr(invocation_module.workflow, "execute_activity", fail)

    await report_progress("clip/0")


@dataclass
class HeaderInput:
    headers: dict[str, Payload]


class ContinueAsNew(Exception):
    pass


class RecordingOutbound:
    def __init__(self) -> None:
        self.inputs: dict[str, HeaderInput] = {}

    def start_activity(self, input: HeaderInput) -> str:
        self.inputs["activity"] = input
        return "activity"

    def start_local_activity(self, input: HeaderInput) -> str:
        self.inputs["local_activity"] = input
        return "local_activity"

    async def start_child_workflow(self, input: HeaderInput) -> str:
        self.inputs["child_workflow"] = input
        return "child_workflow"

    def continue_as_new(self, input: HeaderInput) -> None:
        self.inputs["continue_as_new"] = input
        raise ContinueAsNew


async def test_outbound_interceptor_propagates_raw_meta_header(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    raw = json_payload(meta_fixture())
    install_headers(monkeypatch, {invocation_module.META_HEADER_KEY: raw})
    next = RecordingOutbound()
    outbound = invocation_module._InvocationOutboundInterceptor(next)

    assert outbound.start_activity(HeaderInput({})) == "activity"
    assert outbound.start_local_activity(HeaderInput({})) == "local_activity"
    assert await outbound.start_child_workflow(HeaderInput({})) == "child_workflow"
    with pytest.raises(ContinueAsNew):
        outbound.continue_as_new(HeaderInput({}))

    assert set(next.inputs) == {
        "activity",
        "local_activity",
        "child_workflow",
        "continue_as_new",
    }
    for input in next.inputs.values():
        assert input.headers[invocation_module.META_HEADER_KEY] == raw


def test_header_propagation_is_noop_without_meta(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    install_headers(monkeypatch, {})
    input = HeaderInput({})

    assert invocation_module._with_invocation_meta(input).headers == {}
