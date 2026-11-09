package failure

import (
	"errors"
	"slices"
	"testing"

	"go.temporal.io/sdk/temporal"
)

func TestNonRetryableConstructorsProduceListedTypes(t *testing.T) {
	cause := errors.New("boom")
	cases := []struct {
		err      error
		wantType string
		wantMsg  string
	}{
		{NotFound("asset %s", "a1"), TypeNotFound, "asset a1"},
		{InvalidArgument("prompt is %s", "empty"), TypeInvalidArgument, "prompt is empty"},
		{PermissionDenied("lane %q", "dev"), TypePermissionDenied, `lane "dev"`},
		{Configuration(cause), TypeConfiguration, "boom"},
		{UpstreamRejected(cause), TypeUpstreamRequestRejected, "boom"},
	}
	for _, tc := range cases {
		var appErr *temporal.ApplicationError
		if !errors.As(tc.err, &appErr) {
			t.Fatalf("%v is not an ApplicationError", tc.err)
		}
		if appErr.Type() != tc.wantType {
			t.Errorf("type = %q, want %q", appErr.Type(), tc.wantType)
		}
		if appErr.Message() != tc.wantMsg {
			t.Errorf("message = %q, want %q", appErr.Message(), tc.wantMsg)
		}
		if !appErr.NonRetryable() {
			t.Errorf("%s: NonRetryable() = false", tc.wantType)
		}
		if !slices.Contains(NonRetryableTypes, appErr.Type()) {
			t.Errorf("%s missing from NonRetryableTypes", appErr.Type())
		}
	}
}

func TestTransientIsRetryable(t *testing.T) {
	cause := errors.New("connection reset")
	var appErr *temporal.ApplicationError
	err := Transient(cause)
	if !errors.As(err, &appErr) || appErr.Type() != TypeTransient || appErr.NonRetryable() ||
		appErr.Unwrap() != cause || slices.Contains(NonRetryableTypes, TypeTransient) {
		t.Fatalf("Transient(%v) = %v", cause, err)
	}
}

func TestConstructorsWithCauseKeepIt(t *testing.T) {
	cause := errors.New("dial tcp: refused")
	for _, err := range []error{Configuration(cause), UpstreamRejected(cause), Transient(cause)} {
		var appErr *temporal.ApplicationError
		if !errors.As(err, &appErr) || appErr.Unwrap() != cause {
			t.Errorf("%v does not keep cause %v", err, cause)
		}
	}
}
