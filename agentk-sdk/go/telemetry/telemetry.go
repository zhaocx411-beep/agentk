// Package telemetry holds the OpenTelemetry attribute keys, metric names and
// status values shared by agentk and every worker built on this SDK. Both
// sides must use the same keys so spans and metrics from different processes
// can be joined by app, session and invocation.
package telemetry

import "go.opentelemetry.io/otel/attribute"

// Span attributes carrying agentk business identifiers. They are high
// cardinality and belong on spans only, never on metrics.
const (
	AppID          = attribute.Key("agentk.app.id")
	SessionID      = attribute.Key("agentk.session.id")
	ExecutionID    = attribute.Key("agentk.execution.id")
	InvocationID   = attribute.Key("agentk.invocation.id")
	InvocationKind = attribute.Key("agentk.invocation.kind")
	InvocationName = attribute.Key("agentk.invocation.name")
	CapabilityID   = attribute.Key("agentk.capability.id")
	Tool           = attribute.Key("agentk.tool")
	Model          = attribute.Key("agentk.model")

	CallerKSN         = attribute.Key("agentk.caller.ksn")
	CallerServiceID   = attribute.Key("agentk.caller.service.id")
	CallerServiceName = attribute.Key("agentk.caller.service.name")
	Lane              = attribute.Key("agentk.lane")
	InputKind         = attribute.Key("agentk.input.kind")
	RPCErrorMessage   = attribute.Key("agentk.rpc.error.message")
	StreamFramesSent  = attribute.Key("agentk.rpc.stream.frames_sent")
	StreamCloseReason = attribute.Key("agentk.rpc.stream.close_reason")
)

// Low-cardinality metric attributes.
const (
	AttrApp          = attribute.Key("app")
	AttrKind         = attribute.Key("kind")
	AttrTemporalType = attribute.Key("temporal_type")
	AttrStatus       = attribute.Key("status")
)

// Worker-side metric instrument names.
const (
	MetricWorkerExecutions      = "agentk.worker.executions"
	MetricWorkerDuration        = "agentk.worker.duration"
	MetricWorkerMetaMissing     = "agentk.worker.meta_missing"
	MetricWorkerHeartbeats      = "agentk.worker.heartbeats"
	MetricWorkerProgressReports = "agentk.worker.progress_reports"
	MetricWorkerAnnounces       = "agentk.worker.announces"
)

// Values of AttrStatus.
const (
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// Values of AttrKind for worker executions.
const (
	KindActivity = "activity"
	KindWorkflow = "workflow"
)
