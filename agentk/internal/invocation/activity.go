// Package invocation handles callbacks from capability workflows invoked by agent-k.
package invocation

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	sdkinvocation "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/invocation"
	"go.temporal.io/sdk/activity"

	"agentk/internal/event"
	"agentk/internal/temporalx"
	"agentk/internal/types"
)

var canonicalNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,127}$`)

// Store 是 invocation callback 写入会话事件日志的最小依赖。
type Store interface {
	AppendEvents(
		ctx context.Context,
		sessionID int64,
		executionID types.ExecutionID,
		batchID event.BatchID,
		drafts []event.Draft,
	) ([]event.Event, error)
}

// Activities 持有 invocation callback Activity 的共享依赖。
type Activities struct {
	store Store
}

func NewActivities(store Store) *Activities {
	return &Activities{store: store}
}

// Register 注册跨语言 invocation callback Activities。
func Register(w temporalx.Registry, a *Activities) {
	w.RegisterActivityWithOptions(a.ReportProgressActivity,
		activity.RegisterOptions{Name: sdkinvocation.ReportProgressActivityType})
}

// ReportProgressActivity 把 capability workflow 的一次进度回报幂等写入会话事件日志。
func (a *Activities) ReportProgressActivity(
	ctx context.Context,
	input sdkinvocation.ReportProgressInput,
) error {
	const op = "report invocation progress"
	if a.store == nil {
		return temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("invocation store is required")))
	}
	if err := validateProgressInput(input); err != nil {
		return temporalx.ClassifyActivityError(op, err)
	}
	_, err := a.store.AppendEvents(ctx, input.SessionID, types.ExecutionID(input.InvocationID),
		progressBatchID(input.InvocationID, input.StepID),
		[]event.Draft{{Payload: event.InvocationProgress{
			InvocationID:   input.InvocationID,
			InvocationKind: string(input.InvocationKind),
			InvocationName: input.InvocationName,
			StepID:         input.StepID,
			Message:        input.Message,
			Data:           event.RawJSON(input.Data),
		}}})
	if err != nil {
		return temporalx.ClassifyActivityError(op, err)
	}
	return nil
}

func validateProgressInput(input sdkinvocation.ReportProgressInput) error {
	if input.SessionID <= 0 || input.InvocationID == "" || input.InvocationName == "" ||
		input.StepID == "" {
		return temporalx.InvalidActivityInput(
			"sessionId, invocationId, invocationName and stepId are required")
	}
	if len(input.InvocationID) > 256 {
		return temporalx.InvalidActivityInput("invocationId exceeds 256 characters")
	}
	if !strings.HasPrefix(input.InvocationID, "exe_") {
		return temporalx.InvalidActivityInput("invocationId must be an ExecutionID")
	}
	if input.InvocationKind != sdkinvocation.KindPipeline &&
		input.InvocationKind != sdkinvocation.KindTool {
		return temporalx.InvalidActivityInput("invalid invocationKind %q", input.InvocationKind)
	}
	if !canonicalNamePattern.MatchString(input.InvocationName) {
		return temporalx.InvalidActivityInput("invalid invocationName %q", input.InvocationName)
	}
	if len(input.StepID) > 256 {
		return temporalx.InvalidActivityInput("stepId exceeds 256 characters")
	}
	if len(input.Data) > 0 && !json.Valid(input.Data) {
		return temporalx.InvalidActivityInput("data must be valid JSON")
	}
	return nil
}

func progressBatchID(invocationID, stepID string) event.BatchID {
	sum := sha256.Sum256([]byte(invocationID + "\x00" + stepID))
	return event.BatchID("ivp/" + base64.RawURLEncoding.EncodeToString(sum[:]))
}
