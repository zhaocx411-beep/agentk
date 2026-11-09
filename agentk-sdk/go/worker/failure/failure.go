// Package failure defines the ApplicationError types AgentK reads off a failed
// Activity or Workflow, and constructors that produce them. The type names
// travel across Temporal as ApplicationError.Type, so they are part of the wire
// contract shared with the AgentK server and the Python Worker SDK.
package failure

import (
	"fmt"

	"go.temporal.io/sdk/temporal"
)

const (
	TypeNotFound                = "NotFound"
	TypeInvalidArgument         = "InvalidArgument"
	TypeConfiguration           = "Configuration"
	TypePermissionDenied        = "PermissionDenied"
	TypeUpstreamRequestRejected = "UpstreamRequestRejected"
	TypeTransient               = "Transient"
)

// NonRetryableTypes lists the ApplicationError types that AgentK treats as
// deterministic failures: a retry with the same input cannot succeed, so the
// dispatch fails immediately instead of exhausting the retry budget.
var NonRetryableTypes = []string{
	TypeNotFound,
	TypeInvalidArgument,
	TypeConfiguration,
	TypePermissionDenied,
	TypeUpstreamRequestRejected,
}

// NotFound reports that a referenced resource does not exist. The error is
// never retried.
func NotFound(format string, args ...any) error {
	return temporal.NewNonRetryableApplicationError(fmt.Sprintf(format, args...), TypeNotFound, nil)
}

// InvalidArgument reports that the input is malformed or violates a
// precondition. The error is never retried.
func InvalidArgument(format string, args ...any) error {
	return temporal.NewNonRetryableApplicationError(fmt.Sprintf(format, args...), TypeInvalidArgument, nil)
}

// PermissionDenied reports that the caller may not perform the operation. The
// error is never retried.
func PermissionDenied(format string, args ...any) error {
	return temporal.NewNonRetryableApplicationError(fmt.Sprintf(format, args...), TypePermissionDenied, nil)
}

// Configuration reports that the Worker itself is misconfigured and cannot
// serve until an operator intervenes. cause is kept as the error cause. The
// error is never retried.
func Configuration(cause error) error {
	return temporal.NewNonRetryableApplicationError(cause.Error(), TypeConfiguration, cause)
}

// UpstreamRejected reports that an upstream service rejected the request as
// invalid (an HTTP 4xx-class outcome), so repeating it cannot succeed. cause is
// kept as the error cause. The error is never retried.
func UpstreamRejected(cause error) error {
	return temporal.NewNonRetryableApplicationError(cause.Error(), TypeUpstreamRequestRejected, cause)
}

// Transient reports a failure that a retry may recover from (network faults,
// rate limits, upstream 5xx). cause is kept as the error cause. The error
// follows the retry policy.
func Transient(cause error) error {
	return temporal.NewApplicationErrorWithCause(cause.Error(), TypeTransient, cause)
}
