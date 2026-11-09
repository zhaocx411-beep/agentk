"""OpenTelemetry attribute keys, metric names and status values shared by
agentk and every worker built on this SDK.

Both sides must use the same keys so spans and metrics from different
processes can be joined by app, session and invocation. The Go SDK exposes
the same constants in ``go/telemetry``.
"""

# Span attributes carrying agentk business identifiers. They are high
# cardinality and belong on spans only, never on metrics.
APP_ID = "agentk.app.id"
SESSION_ID = "agentk.session.id"
EXECUTION_ID = "agentk.execution.id"
INVOCATION_ID = "agentk.invocation.id"
INVOCATION_KIND = "agentk.invocation.kind"
INVOCATION_NAME = "agentk.invocation.name"
CAPABILITY_ID = "agentk.capability.id"
TOOL = "agentk.tool"
MODEL = "agentk.model"

CALLER_KSN = "agentk.caller.ksn"
CALLER_SERVICE_ID = "agentk.caller.service.id"
CALLER_SERVICE_NAME = "agentk.caller.service.name"
LANE = "agentk.lane"
INPUT_KIND = "agentk.input.kind"
RPC_ERROR_MESSAGE = "agentk.rpc.error.message"
STREAM_FRAMES_SENT = "agentk.rpc.stream.frames_sent"
STREAM_CLOSE_REASON = "agentk.rpc.stream.close_reason"

# Low-cardinality metric attributes.
ATTR_APP = "app"
ATTR_KIND = "kind"
ATTR_TEMPORAL_TYPE = "temporal_type"
ATTR_STATUS = "status"

# Worker-side metric instrument names.
METRIC_WORKER_EXECUTIONS = "agentk.worker.executions"
METRIC_WORKER_DURATION = "agentk.worker.duration"
METRIC_WORKER_META_MISSING = "agentk.worker.meta_missing"
METRIC_WORKER_HEARTBEATS = "agentk.worker.heartbeats"
METRIC_WORKER_PROGRESS_REPORTS = "agentk.worker.progress_reports"
METRIC_WORKER_ANNOUNCES = "agentk.worker.announces"

# Values of ATTR_STATUS.
STATUS_COMPLETED = "completed"
STATUS_FAILED = "failed"
STATUS_CANCELLED = "cancelled"

# Values of ATTR_KIND for worker executions.
KIND_ACTIVITY = "activity"
KIND_WORKFLOW = "workflow"
