package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/telemetry"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/invocation"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/activity"
	temporalotel "go.temporal.io/sdk/contrib/opentelemetry"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	temporalworker "go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

var telemetryTestMeta = invocation.Meta{
	AppID:          "app_telemetry",
	InvocationID:   "inv_telemetry",
	InvocationKind: invocation.KindDirect,
	InvocationName: "contrib.demo.probe",
}

type telemetryProbe struct {
	reader *sdkmetric.ManualReader
	spans  *tracetest.SpanRecorder
}

var (
	probeOnce sync.Once
	probe     telemetryProbe
)

// installTelemetryProbe points the global providers at in-memory ones so the
// interceptor's metrics and span attributes can be asserted. The global meter
// delegates to the first provider installed, so the probe is process-wide;
// the reader uses delta temporality and each call drains what came before.
func installTelemetryProbe(t *testing.T) telemetryProbe {
	t.Helper()
	probeOnce.Do(func() {
		probe.reader = sdkmetric.NewManualReader(sdkmetric.WithTemporalitySelector(
			func(sdkmetric.InstrumentKind) metricdata.Temporality { return metricdata.DeltaTemporality },
		))
		probe.spans = tracetest.NewSpanRecorder()
		otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(probe.reader)))
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(probe.spans)))
	})
	var drain metricdata.ResourceMetrics
	if err := probe.reader.Collect(context.Background(), &drain); err != nil {
		t.Fatal(err)
	}
	probe.spans.Reset()
	return probe
}

// collect drains the reader once; sum then reads individual metrics from the
// snapshot without a second collection.
func (p telemetryProbe) collect(t *testing.T) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := p.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	return rm
}

func sum(t *testing.T, rm metricdata.ResourceMetrics, name string) []metricdata.DataPoint[int64] {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok {
				return sum.DataPoints
			}
			t.Fatalf("metric %s is %T, want Sum[int64]", name, m.Data)
		}
	}
	return nil
}

func probeManifest() capability.ManifestV1 {
	return capability.ManifestV1{
		Capabilities: []capability.CapabilityDef{
			{Kind: capability.CapabilityKindActivity, TemporalType: "demo.probe.activity"},
			{Kind: capability.CapabilityKindWorkflow, TemporalType: "demo.probe.workflow"},
		},
	}
}

func newProbeEnv(t *testing.T, header invocation.Meta, withMeta bool) *testsuite.TestWorkflowEnvironment {
	t.Helper()
	tracing, err := temporalotel.NewTracingInterceptor(temporalotel.TracerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var suite testsuite.WorkflowTestSuite
	suite.SetContextPropagators([]workflow.ContextPropagator{invocation.MetaPropagator{}})
	env := suite.NewTestWorkflowEnvironment()
	// Same order as a real worker: client-installed tracing first, then the
	// SDK interceptor, so the span being annotated is the tracing one.
	env.SetWorkerOptions(temporalworker.Options{
		Interceptors: []interceptor.WorkerInterceptor{tracing, TelemetryInterceptor(probeManifest())},
	})
	if withMeta {
		ctx := invocation.WithMeta(context.Background(), header)
		carrier := headerCarrier{}
		if err := (invocation.MetaPropagator{}).Inject(ctx, carrier); err != nil {
			t.Fatal(err)
		}
		env.SetHeader(&commonpb.Header{Fields: carrier})
	}
	return env
}

type headerCarrier map[string]*commonpb.Payload

func (h headerCarrier) Set(key string, value *commonpb.Payload) { h[key] = value }
func (h headerCarrier) Get(key string) (*commonpb.Payload, bool) {
	p, ok := h[key]
	return p, ok
}
func (h headerCarrier) ForEachKey(handler func(string, *commonpb.Payload) error) error {
	for k, v := range h {
		if err := handler(k, v); err != nil {
			return err
		}
	}
	return nil
}

func TestTelemetryInterceptorAttributesCapabilityExecutionsToApp(t *testing.T) {
	probe := installTelemetryProbe(t)
	env := newProbeEnv(t, telemetryTestMeta, true)

	env.RegisterActivityWithOptions(
		func(ctx context.Context) error { return nil },
		activity.RegisterOptions{Name: "demo.probe.activity"},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context) error { return errors.New("boom") },
		activity.RegisterOptions{Name: "demo.internal.activity"},
	)
	env.RegisterWorkflowWithOptions(func(ctx workflow.Context) error {
		ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 5 * time.Second,
			RetryPolicy:         &temporalRetryOnce,
		})
		if err := workflow.ExecuteActivity(ctx, "demo.probe.activity").Get(ctx, nil); err != nil {
			return err
		}
		_ = workflow.ExecuteActivity(ctx, "demo.internal.activity").Get(ctx, nil)
		return nil
	}, workflow.RegisterOptions{Name: "demo.probe.workflow"})

	env.ExecuteWorkflow("demo.probe.workflow")
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}

	rm := probe.collect(t)
	points := sum(t, rm, telemetry.MetricWorkerExecutions)
	if len(points) != 2 {
		t.Fatalf("executions data points = %d, want activity and workflow", len(points))
	}
	for _, dp := range points {
		assertAttr(t, dp.Attributes, telemetry.AttrApp, telemetryTestMeta.AppID)
		assertAttr(t, dp.Attributes, telemetry.AttrStatus, telemetry.StatusCompleted)
		if dp.Value != 1 {
			t.Fatalf("execution count = %d", dp.Value)
		}
	}
	if missing := sum(t, rm, telemetry.MetricWorkerMetaMissing); len(missing) != 0 {
		t.Fatalf("meta_missing must be empty, got %v", missing)
	}

	var annotated int
	for _, span := range probe.spans.Ended() {
		attrs := attribute.NewSet(span.Attributes()...)
		if v, ok := attrs.Value(telemetry.AppID); ok && v.AsString() == telemetryTestMeta.AppID {
			annotated++
			if v, _ := attrs.Value(telemetry.InvocationID); v.AsString() != telemetryTestMeta.InvocationID {
				t.Fatalf("span %s invocation id = %q", span.Name(), v.AsString())
			}
		}
	}
	if annotated < 2 {
		t.Fatalf("annotated spans = %d, want activity and workflow spans", annotated)
	}
}

func TestTelemetryInterceptorCountsMissingMetaAndFailures(t *testing.T) {
	probe := installTelemetryProbe(t)
	env := newProbeEnv(t, invocation.Meta{}, false)

	env.RegisterActivityWithOptions(
		func(ctx context.Context) error { return errors.New("boom") },
		activity.RegisterOptions{Name: "demo.probe.activity"},
	)
	env.RegisterWorkflowWithOptions(func(ctx workflow.Context) error {
		ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 5 * time.Second,
			RetryPolicy:         &temporalRetryOnce,
		})
		return workflow.ExecuteActivity(ctx, "demo.probe.activity").Get(ctx, nil)
	}, workflow.RegisterOptions{Name: "demo.probe.workflow"})

	env.ExecuteWorkflow("demo.probe.workflow")
	if env.GetWorkflowError() == nil {
		t.Fatal("workflow must fail")
	}

	rm := probe.collect(t)
	points := sum(t, rm, telemetry.MetricWorkerExecutions)
	if len(points) != 2 {
		t.Fatalf("executions data points = %d, want activity and workflow", len(points))
	}
	for _, dp := range points {
		assertAttr(t, dp.Attributes, telemetry.AttrStatus, telemetry.StatusFailed)
		if _, ok := dp.Attributes.Value(telemetry.AttrApp); ok {
			t.Fatalf("app attribute must be absent without meta: %v", dp.Attributes)
		}
	}
	missing := sum(t, rm, telemetry.MetricWorkerMetaMissing)
	if len(missing) != 2 {
		t.Fatalf("meta_missing data points = %d, want activity and workflow", len(missing))
	}
}

func assertAttr(t *testing.T, set attribute.Set, key attribute.Key, want string) {
	t.Helper()
	v, ok := set.Value(key)
	if !ok || v.AsString() != want {
		t.Fatalf("%s = %q (%v), want %q", key, v.AsString(), ok, want)
	}
}

var temporalRetryOnce = temporal.RetryPolicy{MaximumAttempts: 1}
