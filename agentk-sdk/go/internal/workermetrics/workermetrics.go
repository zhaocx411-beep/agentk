// Package workermetrics owns the OpenTelemetry instruments the worker SDK
// records into. All instruments go through the global MeterProvider; a host
// that never installs one gets the API's no-op behaviour.
package workermetrics

import (
	"context"
	"time"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const scope = "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker"

var (
	meter = otel.Meter(scope)

	executions      = int64Counter(telemetry.MetricWorkerExecutions, "{execution}", "Capability activities and workflows executed by this worker.")
	duration        = int64Histogram(telemetry.MetricWorkerDuration, "ms", "Wall-clock duration of capability executions.")
	metaMissing     = int64Counter(telemetry.MetricWorkerMetaMissing, "{execution}", "Capability executions that arrived without agentk invocation meta.")
	heartbeats      = int64Counter(telemetry.MetricWorkerHeartbeats, "{heartbeat}", "Activity heartbeats recorded through the SDK.")
	progressReports = int64Counter(telemetry.MetricWorkerProgressReports, "{report}", "Progress reports sent to agentk.")
	announces       = int64Counter(telemetry.MetricWorkerAnnounces, "{announce}", "Capability manifest announcements attempted.")
)

func int64Counter(name, unit, description string) metric.Int64Counter {
	c, err := meter.Int64Counter(name, metric.WithUnit(unit), metric.WithDescription(description))
	if err != nil {
		panic(err)
	}
	return c
}

func int64Histogram(name, unit, description string) metric.Int64Histogram {
	h, err := meter.Int64Histogram(name, metric.WithUnit(unit), metric.WithDescription(description))
	if err != nil {
		panic(err)
	}
	return h
}

// Execution records one finished capability execution. app is empty when the
// execution carried no invocation meta; it is then counted as missing instead
// of being attributed.
func Execution(ctx context.Context, kind, temporalType, app, status string, elapsed time.Duration) {
	attrs := []attribute.KeyValue{
		telemetry.AttrKind.String(kind),
		telemetry.AttrTemporalType.String(temporalType),
		telemetry.AttrStatus.String(status),
	}
	if app != "" {
		attrs = append(attrs, telemetry.AttrApp.String(app))
	} else {
		metaMissing.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrKind.String(kind),
			telemetry.AttrTemporalType.String(temporalType),
		))
	}
	set := metric.WithAttributes(attrs...)
	executions.Add(ctx, 1, set)
	duration.Record(ctx, elapsed.Milliseconds(), set)
}

func Heartbeat(ctx context.Context, app string) {
	heartbeats.Add(ctx, 1, metric.WithAttributes(appAttributes(app)...))
}

func ProgressReport(ctx context.Context, app, status string) {
	progressReports.Add(ctx, 1, metric.WithAttributes(
		append(appAttributes(app), telemetry.AttrStatus.String(status))...))
}

func Announce(ctx context.Context, status string) {
	announces.Add(ctx, 1, metric.WithAttributes(telemetry.AttrStatus.String(status)))
}

func appAttributes(app string) []attribute.KeyValue {
	if app == "" {
		return nil
	}
	return []attribute.KeyValue{telemetry.AttrApp.String(app)}
}

func Status(err error) string {
	if err != nil {
		return telemetry.StatusFailed
	}
	return telemetry.StatusCompleted
}
