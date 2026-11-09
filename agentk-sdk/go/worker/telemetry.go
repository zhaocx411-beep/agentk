package worker

import (
	"context"
	"errors"
	"time"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/internal/workermetrics"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/telemetry"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/invocation"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/activity"
	temporalotel "go.temporal.io/sdk/contrib/opentelemetry"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// capabilityLookup 报告某个 Temporal 类型是否是本 Worker 发布的 capability。
type capabilityLookup func(kind capability.CapabilityKind, temporalType string) bool

func manifestLookup(manifest capability.ManifestV1) capabilityLookup {
	types := make(map[capability.CapabilityKind]map[string]struct{}, 2)
	for _, def := range manifest.Capabilities {
		set, ok := types[def.Kind]
		if !ok {
			set = make(map[string]struct{})
			types[def.Kind] = set
		}
		set[def.TemporalType] = struct{}{}
	}
	return func(kind capability.CapabilityKind, temporalType string) bool {
		_, ok := types[kind][temporalType]
		return ok
	}
}

// TelemetryInterceptor returns the worker interceptor that Worker installs
// automatically, for use with a plain Temporal worker that hosts the
// capabilities declared in manifest.
//
// The interceptor records agentk.worker.executions and agentk.worker.duration
// for every capability activity and workflow in manifest, attributed to the
// calling app through the agentk invocation meta header, and annotates the
// current span with the invocation identifiers. Executions of Temporal types
// not in manifest are ignored. Metrics and spans go through the global
// OpenTelemetry providers.
func TelemetryInterceptor(manifest capability.ManifestV1) interceptor.WorkerInterceptor {
	return newTelemetryInterceptor(manifestLookup(manifest))
}

func newTelemetryInterceptor(isCapability capabilityLookup) interceptor.WorkerInterceptor {
	return &telemetryInterceptor{isCapability: isCapability}
}

type telemetryInterceptor struct {
	interceptor.WorkerInterceptorBase
	isCapability capabilityLookup
}

func (t *telemetryInterceptor) InterceptActivity(
	_ context.Context,
	next interceptor.ActivityInboundInterceptor,
) interceptor.ActivityInboundInterceptor {
	in := &telemetryActivityInbound{isCapability: t.isCapability}
	in.Next = next
	return in
}

func (t *telemetryInterceptor) InterceptWorkflow(
	_ workflow.Context,
	next interceptor.WorkflowInboundInterceptor,
) interceptor.WorkflowInboundInterceptor {
	in := &telemetryWorkflowInbound{isCapability: t.isCapability}
	in.Next = next
	return in
}

type telemetryActivityInbound struct {
	interceptor.ActivityInboundInterceptorBase
	isCapability capabilityLookup
}

func (a *telemetryActivityInbound) ExecuteActivity(
	ctx context.Context,
	in *interceptor.ExecuteActivityInput,
) (any, error) {
	temporalType := activity.GetInfo(ctx).ActivityType.Name
	if !a.isCapability(capability.CapabilityKindActivity, temporalType) {
		return a.Next.ExecuteActivity(ctx, in)
	}
	meta, hasMeta := invocation.MetaFromActivity(ctx)
	if hasMeta {
		trace.SpanFromContext(ctx).SetAttributes(metaSpanAttributes(meta)...)
	}
	start := time.Now()
	out, err := a.Next.ExecuteActivity(ctx, in)
	workermetrics.Execution(ctx, telemetry.KindActivity, temporalType, meta.AppID,
		activityStatus(err, ctx.Err()), time.Since(start))
	return out, err
}

type telemetryWorkflowInbound struct {
	interceptor.WorkflowInboundInterceptorBase
	isCapability capabilityLookup
}

func (w *telemetryWorkflowInbound) ExecuteWorkflow(
	ctx workflow.Context,
	in *interceptor.ExecuteWorkflowInput,
) (any, error) {
	temporalType := workflow.GetInfo(ctx).WorkflowType.Name
	if !w.isCapability(capability.CapabilityKindWorkflow, temporalType) {
		return w.Next.ExecuteWorkflow(ctx, in)
	}
	meta, hasMeta := invocation.MetaFromWorkflow(ctx)
	if hasMeta {
		if span, ok := temporalotel.SpanFromWorkflowContext(ctx); ok {
			span.SetAttributes(metaSpanAttributes(meta)...)
		}
	}
	start := workflow.Now(ctx)
	out, err := w.Next.ExecuteWorkflow(ctx, in)
	// A run reaches this point outside replay exactly once, on the worker that
	// completes it; replays of a finished history (queries) must not count again.
	if !workflow.IsReplaying(ctx) {
		workermetrics.Execution(context.Background(), telemetry.KindWorkflow, temporalType, meta.AppID,
			workflowStatus(ctx, err), workflow.Now(ctx).Sub(start))
	}
	return out, err
}

func metaSpanAttributes(m invocation.Meta) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		telemetry.AppID.String(m.AppID),
		telemetry.InvocationID.String(m.InvocationID),
		telemetry.InvocationKind.String(string(m.InvocationKind)),
		telemetry.InvocationName.String(m.InvocationName),
	}
	if m.HasSession() {
		attrs = append(attrs, telemetry.SessionID.String(m.Session))
	}
	return attrs
}

func activityStatus(err, ctxErr error) string {
	switch {
	case err == nil:
		return telemetry.StatusCompleted
	case temporal.IsCanceledError(err), errors.Is(ctxErr, context.Canceled):
		return telemetry.StatusCancelled
	default:
		return telemetry.StatusFailed
	}
}

func workflowStatus(ctx workflow.Context, err error) string {
	switch {
	case err == nil, workflow.IsContinueAsNewError(err):
		return telemetry.StatusCompleted
	case temporal.IsCanceledError(err), errors.Is(ctx.Err(), workflow.ErrCanceled):
		return telemetry.StatusCancelled
	default:
		return telemetry.StatusFailed
	}
}
