package temporalx

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"time"

	"go.temporal.io/sdk/temporal"

	"agentk/internal/types"
)

const (
	activityErrorNotFound      = "NotFound"
	activityErrorInvalid       = "InvalidArgument"
	activityErrorConfiguration = "Configuration"
	activityErrorPermission    = "PermissionDenied"
	activityErrorUpstream4xx   = "UpstreamRequestRejected"
	activityErrorTransient     = "Transient"
)

// nonRetryableActivityErrorTypes 是 ActivityRetryPolicy 的不可重试类型清单；
// IsPermanentActivityError 复用同一清单判定，二者不会漂移。
// 注意：sdk 子模块（progress.go）内联了同一份清单——类型名跨 Temporal 传给
// 外部实现方，是线上契约的一部分，增删须两边同步。
var nonRetryableActivityErrorTypes = []string{
	activityErrorNotFound,
	activityErrorInvalid,
	activityErrorConfiguration,
	activityErrorPermission,
	activityErrorUpstream4xx,
}

// ActivityRetryPolicy 是业务 Activity 的统一重试策略；maximumAttempts 为 0
// 表示不限次数（收尾类 Activity 用，瞬态故障持续重试到成功）。
func ActivityRetryPolicy(maximumAttempts int32) *temporal.RetryPolicy {
	return &temporal.RetryPolicy{
		InitialInterval:        time.Second,
		BackoffCoefficient:     2,
		MaximumInterval:        10 * time.Second,
		MaximumAttempts:        maximumAttempts,
		NonRetryableErrorTypes: nonRetryableActivityErrorTypes,
	}
}

type permanentActivityError struct {
	kind string
	err  error
}

func (e *permanentActivityError) Error() string { return e.err.Error() }
func (e *permanentActivityError) Unwrap() error { return e.err }

// InvalidActivityInput 构造不可重试的入参错误。
func InvalidActivityInput(format string, args ...any) error {
	return &permanentActivityError{
		kind: activityErrorInvalid,
		err:  fmt.Errorf(format, args...),
	}
}

// ConfigurationError 构造不可重试的装配/配置错误。
func ConfigurationError(err error) error {
	return &permanentActivityError{kind: activityErrorConfiguration, err: err}
}

// ClassifyActivityError 把明确永久错误标为不可重试；其余错误（含网络、
// 429/5xx 与数据库瞬态故障）默认保留为可重试 ApplicationError。
func ClassifyActivityError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var permanent *permanentActivityError
	if errors.As(err, &permanent) {
		return temporal.NewNonRetryableApplicationError(
			operation+": "+permanent.Error(), permanent.kind, err)
	}
	var markedPermanent interface{ Permanent() bool }
	if errors.As(err, &markedPermanent) && markedPermanent.Permanent() {
		return temporal.NewNonRetryableApplicationError(
			operation+": "+err.Error(), activityErrorUpstream4xx, err)
	}
	var statusErr interface{ StatusCode() int }
	if errors.As(err, &statusErr) && nonRetryableHTTPStatus(statusErr.StatusCode()) {
		return temporal.NewNonRetryableApplicationError(
			operation+": "+err.Error(), activityErrorUpstream4xx, err)
	}
	switch {
	case errors.Is(err, types.ErrNotFound):
		return temporal.NewNonRetryableApplicationError(
			operation+": "+err.Error(), activityErrorNotFound, err)
	case errors.Is(err, fs.ErrPermission):
		return temporal.NewNonRetryableApplicationError(
			operation+": "+err.Error(), activityErrorPermission, err)
	default:
		return temporal.NewApplicationErrorWithCause(
			operation+": "+err.Error(), activityErrorTransient, err)
	}
}

// IsPermanentActivityError 报告错误链里是否有不可重试分类的 ApplicationError：
// 显式标记 NonRetryable，或类型命中 ActivityRetryPolicy 的 NonRetryableErrorTypes。
// 这类错误退避重试不会有结果，调用方应改走收尾路径。
func IsPermanentActivityError(err error) bool {
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) {
		return false
	}
	if appErr.NonRetryable() {
		return true
	}
	return slices.Contains(nonRetryableActivityErrorTypes, appErr.Type())
}

func nonRetryableHTTPStatus(status int) bool {
	if status < 400 || status >= 500 {
		return false
	}
	switch status {
	case 408, 409, 425, 429:
		return false
	default:
		return true
	}
}
