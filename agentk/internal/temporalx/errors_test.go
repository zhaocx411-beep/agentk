package temporalx

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"testing"

	"go.temporal.io/sdk/temporal"

	"agentk/internal/types"
)

type statusError int

func (e statusError) Error() string   { return fmt.Sprintf("HTTP %d", int(e)) }
func (e statusError) StatusCode() int { return int(e) }

type markedProviderError struct {
	status    int
	permanent bool
}

func (e markedProviderError) Error() string   { return fmt.Sprintf("provider HTTP %d", e.status) }
func (e markedProviderError) StatusCode() int { return e.status }
func (e markedProviderError) Permanent() bool { return e.permanent }

func TestClassifyActivityError(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		wantType     string
		nonRetryable bool
	}{
		{
			name: "not found", err: fmt.Errorf("load: %w", types.ErrNotFound),
			wantType: activityErrorNotFound, nonRetryable: true,
		},
		{
			name: "invalid input", err: InvalidActivityInput("bad %s", "call"),
			wantType: activityErrorInvalid, nonRetryable: true,
		},
		{
			name: "configuration", err: ConfigurationError(errors.New("bad profile")),
			wantType: activityErrorConfiguration, nonRetryable: true,
		},
		{
			name: "permission", err: fmt.Errorf("open: %w", fs.ErrPermission),
			wantType: activityErrorPermission, nonRetryable: true,
		},
		{
			name: "transient", err: errors.New("connection reset"),
			wantType: activityErrorTransient, nonRetryable: false,
		},
		{
			name: "ordinary upstream 4xx", err: fmt.Errorf("model: %w", statusError(401)),
			wantType: activityErrorUpstream4xx, nonRetryable: true,
		},
		{
			name: "request timeout remains retryable", err: fmt.Errorf("model: %w", statusError(408)),
			wantType: activityErrorTransient, nonRetryable: false,
		},
		{
			name: "conflict remains retryable", err: fmt.Errorf("model: %w", statusError(409)),
			wantType: activityErrorTransient, nonRetryable: false,
		},
		{
			name: "rate limit remains retryable", err: fmt.Errorf("model: %w", statusError(429)),
			wantType: activityErrorTransient, nonRetryable: false,
		},
		{
			name: "provider marks billing rejection permanent",
			err: fmt.Errorf("model: %w", markedProviderError{
				status: 429, permanent: true,
			}),
			wantType: activityErrorUpstream4xx, nonRetryable: true,
		},
		{
			name: "unmarked provider rate limit remains retryable",
			err: fmt.Errorf("model: %w", markedProviderError{
				status: 429, permanent: false,
			}),
			wantType: activityErrorTransient, nonRetryable: false,
		},
		{
			name: "upstream 5xx remains retryable", err: fmt.Errorf("model: %w", statusError(503)),
			wantType: activityErrorTransient, nonRetryable: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ClassifyActivityError("test", test.err)
			var applicationError *temporal.ApplicationError
			if !errors.As(err, &applicationError) {
				t.Fatalf("error = %T, want ApplicationError", err)
			}
			if applicationError.Type() != test.wantType ||
				applicationError.NonRetryable() != test.nonRetryable {
				t.Fatalf("type=%q nonRetryable=%v",
					applicationError.Type(), applicationError.NonRetryable())
			}
		})
	}
}

// 取消不是业务错误：原样传递，让 workflow 走取消收尾而不是失败收尾。
func TestClassifyActivityErrorPassesCancellation(t *testing.T) {
	for _, err := range []error{context.Canceled, context.DeadlineExceeded} {
		if got := ClassifyActivityError("test", err); !errors.Is(got, err) {
			t.Fatalf("classify(%v) = %v, want passthrough", err, got)
		}
	}
	if ClassifyActivityError("test", nil) != nil {
		t.Fatal("nil must classify to nil")
	}
}

// 已分类过的错误不再二次包装：重试路径上分类只发生一次。
func TestClassifyActivityErrorIsIdempotent(t *testing.T) {
	first := ClassifyActivityError("test", errors.New("boom"))
	if second := ClassifyActivityError("test", first); second != first {
		t.Fatalf("re-classified = %v, want the original", second)
	}
}

// 不可重试清单是 retry policy 与判定函数共用的同一份，二者不会漂移。
func TestIsPermanentActivityError(t *testing.T) {
	permanent := ClassifyActivityError("test", InvalidActivityInput("bad"))
	if !IsPermanentActivityError(permanent) {
		t.Fatal("invalid input must be permanent")
	}
	transient := ClassifyActivityError("test", errors.New("connection reset"))
	if IsPermanentActivityError(transient) {
		t.Fatal("transient must not be permanent")
	}
	if IsPermanentActivityError(errors.New("plain")) {
		t.Fatal("non-application error must not be permanent")
	}
	for _, kind := range nonRetryableActivityErrorTypes {
		err := temporal.NewApplicationError("x", kind)
		if !IsPermanentActivityError(err) {
			t.Fatalf("%s must be permanent even without the NonRetryable flag", kind)
		}
	}
}

func TestNonRetryableHTTPStatus(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 422} {
		if !nonRetryableHTTPStatus(status) {
			t.Fatalf("%d must be non-retryable", status)
		}
	}
	for _, status := range []int{200, 302, 408, 409, 425, 429, 500, 502, 503} {
		if nonRetryableHTTPStatus(status) {
			t.Fatalf("%d must stay retryable", status)
		}
	}
}
