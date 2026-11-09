package invocation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

var testMeta = Meta{
	SessionID:         7,
	Session:           "ses_test",
	AppID:             "0198d72a-7c00-7000-8000-000000000001",
	InvocationID:      "exe_test",
	InvocationKind:    KindPipeline,
	InvocationName:    "summarize",
	CallbackTaskQueue: "agentk-test",
}

// headerCarrier 是测试用的最小 HeaderWriter/HeaderReader。
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

func TestMetaPropagatorInjectExtractRoundTrip(t *testing.T) {
	carrier := headerCarrier{}
	ctx := WithMeta(context.Background(), testMeta)
	if err := (MetaPropagator{}).Inject(ctx, carrier); err != nil {
		t.Fatal(err)
	}
	if _, ok := carrier[MetaHeaderKey]; !ok {
		t.Fatalf("header %q not written", MetaHeaderKey)
	}

	out, err := (MetaPropagator{}).Extract(context.Background(), carrier)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := out.Value(metaKey{}).(Meta)
	if !ok {
		t.Fatal("meta missing after extract")
	}
	if got != testMeta {
		t.Fatalf("round trip meta = %+v, want %+v", got, testMeta)
	}
}

func TestMetaPropagatorNoMetaIsNoop(t *testing.T) {
	carrier := headerCarrier{}
	if err := (MetaPropagator{}).Inject(context.Background(), carrier); err != nil {
		t.Fatal(err)
	}
	if len(carrier) != 0 {
		t.Fatalf("header must stay empty, got %v", carrier)
	}
	out, err := (MetaPropagator{}).Extract(context.Background(), carrier)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.Value(metaKey{}).(Meta); ok {
		t.Fatal("meta must be absent")
	}
}

func TestReportProgressUsesInvocationContract(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	suite.SetContextPropagators([]workflow.ContextPropagator{MetaPropagator{}})
	env := suite.NewTestWorkflowEnvironment()

	carrier := headerCarrier{}
	if err := setMetaHeader(carrier, testMeta); err != nil {
		t.Fatal(err)
	}
	env.SetHeader(&commonpb.Header{Fields: carrier})

	var got ReportProgressInput
	var gotTaskQueue string
	env.SetOnActivityStartedListener(func(info *activity.Info, _ context.Context, _ converter.EncodedValues) {
		gotTaskQueue = info.TaskQueue
	})
	env.RegisterActivityWithOptions(
		func(_ context.Context, input ReportProgressInput) error {
			got = input
			return nil
		},
		activity.RegisterOptions{Name: ReportProgressActivityType},
	)
	wf := func(ctx workflow.Context) error {
		return ReportProgress(ctx, "clip/0", "done", json.RawMessage(`{"ok":true}`))
	}
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "demo.progress"})

	env.ExecuteWorkflow("demo.progress")
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	if got.SessionID != testMeta.SessionID ||
		got.InvocationID != testMeta.InvocationID ||
		got.InvocationKind != testMeta.InvocationKind ||
		got.InvocationName != testMeta.InvocationName ||
		got.StepID != "clip/0" || got.Message != "done" ||
		string(got.Data) != `{"ok":true}` {
		t.Fatalf("progress input = %+v", got)
	}
	if gotTaskQueue != testMeta.CallbackTaskQueue {
		t.Fatalf("task queue = %q, want %q", gotTaskQueue, testMeta.CallbackTaskQueue)
	}
}

func TestInvocationV1FixturesRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		target any
	}{
		{name: "meta", schema: "meta", target: &Meta{}},
		{name: "meta-direct", schema: "meta", target: &Meta{}},
		{name: "report-progress-input", schema: "report-progress-input", target: &ReportProgressInput{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.Join("..", "..", "..", "protocol", "agentk", "invocation", "v1")
			schemaRaw, err := os.ReadFile(filepath.Join(root, tt.schema+".schema.json"))
			if err != nil {
				t.Fatal(err)
			}
			var schema map[string]any
			if err := json.Unmarshal(schemaRaw, &schema); err != nil {
				t.Fatalf("schema: %v", err)
			}
			if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
				t.Fatalf("schema draft = %v", schema["$schema"])
			}

			fixture, err := os.ReadFile(filepath.Join(root, "testdata", tt.name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(fixture, tt.target); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(tt.target)
			if err != nil {
				t.Fatal(err)
			}
			assertSameJSON(t, fixture, encoded)
		})
	}
}

func assertSameJSON(t *testing.T, left, right []byte) {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(left, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(right, &b); err != nil {
		t.Fatal(err)
	}
	if !jsonEqual(a, b) {
		t.Fatalf("JSON differs:\n%s\n%s", left, right)
	}
}

func jsonEqual(a, b any) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	return err == nil && string(left) == string(right)
}

var testDirectMeta = Meta{
	AppID:          "0198d72a-7c00-7000-8000-000000000001",
	InvocationID:   "inv_test",
	InvocationKind: KindDirect,
	InvocationName: "contrib.acme.summarize",
}

func TestHasSession(t *testing.T) {
	if !testMeta.HasSession() {
		t.Fatal("session meta must report a session")
	}
	if testDirectMeta.HasSession() {
		t.Fatal("direct meta must not report a session")
	}
}

func TestMetaFromActivityReadsExtractedHeader(t *testing.T) {
	carrier := headerCarrier{}
	if err := setMetaHeader(carrier, testDirectMeta); err != nil {
		t.Fatal(err)
	}
	ctx, err := (MetaPropagator{}).Extract(context.Background(), carrier)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := MetaFromActivity(ctx)
	if !ok || got != testDirectMeta {
		t.Fatalf("MetaFromActivity = %+v, %v", got, ok)
	}
	if _, ok := MetaFromActivity(context.Background()); ok {
		t.Fatal("meta must be absent without header")
	}
}

func TestWithMetaWorkflowInjectsHeaderOnDispatch(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	suite.SetContextPropagators([]workflow.ContextPropagator{MetaPropagator{}})
	env := suite.NewTestWorkflowEnvironment()

	var got Meta
	var gotOK bool
	env.RegisterActivityWithOptions(
		func(ctx context.Context) error {
			got, gotOK = MetaFromActivity(ctx)
			return nil
		},
		activity.RegisterOptions{Name: "demo.probe"},
	)
	wf := func(ctx workflow.Context) error {
		ctx = WithMetaWorkflow(ctx, testDirectMeta)
		ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 5 * time.Second,
		})
		return workflow.ExecuteActivity(ctx, "demo.probe").Get(ctx, nil)
	}
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "demo.dispatch"})

	env.ExecuteWorkflow("demo.dispatch")
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	if !gotOK || got != testDirectMeta {
		t.Fatalf("activity meta = %+v, %v", got, gotOK)
	}
}

// TestReportProgressWithoutMetaIsNoop 验证不在 agent-k 调度下运行（无 header）时
// ReportProgress 不调 activity 也不报错。
func TestReportProgressWithoutMetaIsNoop(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	suite.SetContextPropagators([]workflow.ContextPropagator{MetaPropagator{}})
	env := suite.NewTestWorkflowEnvironment()

	called := false
	env.RegisterActivityWithOptions(
		func(context.Context, ReportProgressInput) error {
			called = true
			return nil
		},
		activity.RegisterOptions{Name: ReportProgressActivityType},
	)
	wf := func(ctx workflow.Context) error {
		return ReportProgress(ctx, "step-1", "进行中", nil)
	}
	env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "demo.noop"})

	env.ExecuteWorkflow("demo.noop")
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("progress activity must not be called without meta")
	}
}
