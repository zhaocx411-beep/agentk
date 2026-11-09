import pytest
from temporalio.exceptions import ApplicationError

from agentk.worker import failure


@pytest.mark.parametrize(
    ("error", "expected_type", "expected_message"),
    [
        (failure.not_found("asset a1"), "NotFound", "asset a1"),
        (
            failure.invalid_argument("prompt is empty"),
            "InvalidArgument",
            "prompt is empty",
        ),
        (failure.permission_denied("lane dev"), "PermissionDenied", "lane dev"),
        (failure.configuration(RuntimeError("boom")), "Configuration", "boom"),
        (
            failure.upstream_rejected(RuntimeError("boom")),
            "UpstreamRequestRejected",
            "boom",
        ),
    ],
)
def test_constructors_produce_non_retryable_listed_types(
    error: ApplicationError, expected_type: str, expected_message: str
) -> None:
    assert isinstance(error, ApplicationError)
    assert error.type == expected_type
    assert error.message == expected_message
    assert error.non_retryable is True
    assert expected_type in failure.NON_RETRYABLE_TYPES


def test_transient_is_retryable() -> None:
    cause = ConnectionResetError("connection reset")
    error = failure.transient(cause)
    assert error.type == "Transient"
    assert error.non_retryable is False
    assert error.__cause__ is cause
    assert "Transient" not in failure.NON_RETRYABLE_TYPES


def test_constructors_with_cause_keep_it() -> None:
    cause = ConnectionRefusedError("dial tcp: refused")
    assert failure.configuration(cause).__cause__ is cause
    assert failure.upstream_rejected(cause).__cause__ is cause


def test_non_retryable_types_match_go_sdk() -> None:
    assert set(failure.NON_RETRYABLE_TYPES) == {
        "NotFound",
        "InvalidArgument",
        "Configuration",
        "PermissionDenied",
        "UpstreamRequestRejected",
    }
