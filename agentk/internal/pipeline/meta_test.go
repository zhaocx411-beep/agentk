package pipeline

import (
	"context"
	"testing"

	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/invocation"
)

// 本文件覆盖依赖 ExecWorkflow container 的 Meta 透传集成。

var testInvocationMeta = invocation.Meta{
	SessionID:         7,
	Session:           testSessionID,
	TenantID:          "tnt_test",
	InvocationID:      string(testExecID),
	InvocationKind:    invocation.KindPipeline,
	InvocationName:    "demo",
	CallbackTaskQueue: "agentk-test",
}

// metaTestHeader 构造包含 invocation Meta 的 workflow header。
func metaTestHeader(t *testing.T, m invocation.Meta) *commonpb.Header {
	t.Helper()
	payload, err := converter.GetDefaultDataConverter().ToPayload(m)
	if err != nil {
		t.Fatal(err)
	}
	return &commonpb.Header{Fields: map[string]*commonpb.Payload{
		invocation.MetaHeaderKey: payload,
	}}
}

// TestReportProgressThroughChildWorkflow 验证 Meta 经 container 传入 child workflow。
func TestReportProgressThroughChildWorkflow(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	suite.SetContextPropagators([]workflow.ContextPropagator{invocation.MetaPropagator{}})
	env := suite.NewTestWorkflowEnvironment()
	env.SetHeader(metaTestHeader(t, testInvocationMeta))

	var finished []FinishExecutionInput
	registerExecLifecycle(t, env, &finished)
	env.RegisterWorkflowWithOptions(ExecWorkflow, workflow.RegisterOptions{Name: ExecWorkflowType})

	var reported []invocation.ReportProgressInput
	env.RegisterActivityWithOptions(
		func(_ context.Context, in invocation.ReportProgressInput) error {
			reported = append(reported, in)
			return nil
		},
		activity.RegisterOptions{Name: invocation.ReportProgressActivityType},
	)
	child := func(ctx workflow.Context, _ map[string]any) (map[string]any, error) {
		if err := invocation.ReportProgress(ctx, "step-1", "进行中", nil); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true}, nil
	}
	env.RegisterWorkflowWithOptions(child, workflow.RegisterOptions{Name: "demo.progress"})

	env.ExecuteWorkflow(ExecWorkflowType, execInput(KindWorkflow, "demo.progress", `{}`))
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	if len(reported) != 1 {
		t.Fatalf("progress calls = %+v", reported)
	}
	got := reported[0]
	if got.SessionID != testInvocationMeta.SessionID ||
		got.InvocationID != testInvocationMeta.InvocationID ||
		got.InvocationKind != testInvocationMeta.InvocationKind ||
		got.InvocationName != testInvocationMeta.InvocationName ||
		got.StepID != "step-1" || got.Message != "进行中" {
		t.Fatalf("progress input = %+v", got)
	}
}
