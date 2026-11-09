"""ApplicationError types AgentK reads off a failed Activity or Workflow.

The type names travel across Temporal as ``ApplicationError.type``, so they are
part of the wire contract shared with the AgentK server and the Go Worker SDK.
"""

from temporalio.exceptions import ApplicationError

TYPE_NOT_FOUND = "NotFound"
TYPE_INVALID_ARGUMENT = "InvalidArgument"
TYPE_CONFIGURATION = "Configuration"
TYPE_PERMISSION_DENIED = "PermissionDenied"
TYPE_UPSTREAM_REQUEST_REJECTED = "UpstreamRequestRejected"
TYPE_TRANSIENT = "Transient"

NON_RETRYABLE_TYPES = [
    TYPE_NOT_FOUND,
    TYPE_INVALID_ARGUMENT,
    TYPE_CONFIGURATION,
    TYPE_PERMISSION_DENIED,
    TYPE_UPSTREAM_REQUEST_REJECTED,
]
"""ApplicationError types that AgentK treats as deterministic failures.

A retry with the same input cannot succeed, so the dispatch fails immediately
instead of exhausting the retry budget.
"""


def not_found(message: str) -> ApplicationError:
    """Return a never-retried error for a referenced resource that does not exist."""
    return ApplicationError(message, type=TYPE_NOT_FOUND, non_retryable=True)


def invalid_argument(message: str) -> ApplicationError:
    """Return a never-retried error for malformed input or a violated precondition."""
    return ApplicationError(message, type=TYPE_INVALID_ARGUMENT, non_retryable=True)


def permission_denied(message: str) -> ApplicationError:
    """Return a never-retried error for an operation the caller may not perform."""
    return ApplicationError(message, type=TYPE_PERMISSION_DENIED, non_retryable=True)


def configuration(cause: BaseException) -> ApplicationError:
    """Return a never-retried error for a misconfigured Worker.

    The Worker cannot serve until an operator intervenes. The cause is kept as
    ``__cause__``.
    """
    error = ApplicationError(str(cause), type=TYPE_CONFIGURATION, non_retryable=True)
    error.__cause__ = cause
    return error


def upstream_rejected(cause: BaseException) -> ApplicationError:
    """Return a never-retried error for a request an upstream service rejected.

    This is the HTTP 4xx-class outcome: repeating the request cannot succeed.
    The cause is kept as ``__cause__``.
    """
    error = ApplicationError(
        str(cause), type=TYPE_UPSTREAM_REQUEST_REJECTED, non_retryable=True
    )
    error.__cause__ = cause
    return error


def transient(cause: BaseException) -> ApplicationError:
    """Return a retryable error for a failure a retry may recover from.

    Network faults, rate limits and upstream 5xx belong here. The error follows
    the retry policy. The cause is kept as ``__cause__``.
    """
    error = ApplicationError(str(cause), type=TYPE_TRANSIENT)
    error.__cause__ = cause
    return error
