//go:build integration

package worker

import (
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"testing"
	"time"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/telemetry"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/invocation"
	"go.opentelemetry.io/otel/attribute"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	temporalotel "go.temporal.io/sdk/contrib/opentelemetry"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/testsuite"
	temporalworker "go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

type telemetryIntegrationInput struct {
	Value string `json:"value"`
}

func telemetryIntegrationWorkflow(ctx workflow.Context, input telemetryIntegrationInput) (string, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
		RetryPolicy:         &temporalRetryOnce,
	})
	var result string
	err := workflow.ExecuteActivity(ctx, "integration.activity", input).Get(ctx, &result)
	return result, err
}

func TestNewWorkerEmitsCapabilityTelemetry(t *testing.T) {
	t.Setenv("KWS_SERVICE_STAGE", "STAGING")
	t.Setenv("KWS_LANE_ID", "")
	probe := installTelemetryProbe(t)
	binary, err := exec.LookPath("temporal")
	if err != nil {
		t.Fatal(err)
	}
	tracing, err := temporalotel.NewTracingInterceptor(temporalotel.TracerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	server, err := testsuite.StartDevServer(ctx, testsuite.DevServerOptions{
		ExistingPath: binary,
		LogLevel:     "error",
		Stdout:       io.Discard,
		Stderr:       io.Discard,
		ClientOptions: &client.Options{
			Interceptors:       []interceptor.ClientInterceptor{tracing},
			ContextPropagators: []workflow.ContextPropagator{invocation.MetaPropagator{}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := server.Stop(); err != nil {
			t.Error(err)
		}
	})
	c := server.Client()
	w := New(c, "telemetry-integration", temporalworker.Options{})
	w.RegisterWorkflowWithOptions(telemetryIntegrationWorkflow, workflow.RegisterOptions{Name: "integration.workflow"})
	w.RegisterActivityWithOptions(
		func(_ context.Context, input telemetryIntegrationInput) (string, error) {
			return "done: " + input.Value, nil
		},
		activity.RegisterOptions{Name: "integration.activity"},
	)
	declared := capability.Capability{
		ID:             "contrib.integration.workflow",
		TemporalType:   "integration.workflow",
		Description:    "telemetry probe",
		InputSchema:    capability.SchemaOf[telemetryIntegrationInput](),
		OutputSchema:   json.RawMessage(`{"type":"string"}`),
		TimeoutSeconds: 30,
	}
	w.PublishWorkflow(declared)
	declared.ID = "contrib.integration.activity"
	declared.TemporalType = "integration.activity"
	w.PublishActivity(declared)
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Stop)
	run, err := c.ExecuteWorkflow(invocation.WithMeta(ctx, telemetryTestMeta), client.StartWorkflowOptions{
		ID: "telemetry-integration", TaskQueue: "telemetry-integration", WorkflowExecutionTimeout: 20 * time.Second,
	}, "integration.workflow", telemetryIntegrationInput{Value: "input"})
	if err != nil {
		t.Fatal(err)
	}
	var result string
	if err := run.Get(ctx, &result); err != nil {
		t.Fatal(err)
	}
	if result != "done: input" {
		t.Fatalf("result = %q", result)
	}
	points := sum(t, probe.collect(t), telemetry.MetricWorkerExecutions)
	seen := map[string]bool{}
	for _, point := range points {
		typ, _ := point.Attributes.Value(telemetry.AttrTemporalType)
		if typ.AsString() != "integration.workflow" && typ.AsString() != "integration.activity" {
			continue
		}
		assertAttr(t, point.Attributes, telemetry.AttrApp, telemetryTestMeta.AppID)
		assertAttr(t, point.Attributes, telemetry.AttrStatus, telemetry.StatusCompleted)
		if point.Value != 1 {
			t.Fatalf("execution count = %d", point.Value)
		}
		seen[typ.AsString()] = true
	}
	if !seen["integration.workflow"] || !seen["integration.activity"] {
		t.Fatalf("missing capability metrics: %v", seen)
	}
	annotated := 0
	for _, span := range probe.spans.Ended() {
		attrs := attribute.NewSet(span.Attributes()...)
		id, _ := attrs.Value(telemetry.InvocationID)
		if id.AsString() == telemetryTestMeta.InvocationID {
			assertAttr(t, attrs, telemetry.AppID, telemetryTestMeta.AppID)
			annotated++
		}
	}
	if annotated < 2 {
		t.Fatalf("annotated spans = %d, want workflow and activity spans", annotated)
	}
}
