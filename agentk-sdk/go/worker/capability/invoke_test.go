package capability_test

import (
	"fmt"
	"testing"

	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
	"google.golang.org/protobuf/encoding/protowire"

	sdktemporal "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/temporal"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/invocation"
)

type demoInput struct {
	Value string `json:"value"`
}

type invokeOutput struct {
	Value string `json:"value"`
}

func TestInvokeExecutesCoreWorkflow(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var received capability.InvokeInput
	env.RegisterWorkflowWithOptions(
		func(_ workflow.Context, input capability.InvokeInput) (invokeOutput, error) {
			received = input
			return invokeOutput{Value: "done"}, nil
		},
		workflow.RegisterOptions{Name: capability.InvokeWorkflowType},
	)
	env.ExecuteWorkflow(func(ctx workflow.Context) (invokeOutput, error) {
		var output invokeOutput
		err := capability.Invoke(ctx, "contrib.demo.target", demoInput{Value: "input"}, &output)
		return output, err
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	if received.CapabilityID != "contrib.demo.target" {
		t.Fatalf("invoke input = %+v", received)
	}
	input, ok := received.Input.(map[string]any)
	if !ok || input["value"] != "input" {
		t.Fatalf("capability input = %#v", received.Input)
	}
	var output invokeOutput
	if err := env.GetWorkflowResult(&output); err != nil {
		t.Fatal(err)
	}
	if output.Value != "done" {
		t.Fatalf("output = %+v", output)
	}
}

func TestInvokePropagatesTemporalMetadataToCoreWorkflow(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	suite.SetContextPropagators([]workflow.ContextPropagator{
		invocation.MetaPropagator{}, sdktemporal.CarrierPropagator{},
	})
	env := suite.NewTestWorkflowEnvironment()
	meta := invocation.Meta{
		SessionID: 7, Session: "ses_test", AppID: "0198d72a-7c00-7000-8000-000000000001",
		InvocationID: "exe_test", InvocationKind: invocation.KindPipeline,
		InvocationName: "demo", CallbackTaskQueue: "agentk",
	}
	payload, err := converter.GetDefaultDataConverter().ToPayload(meta)
	if err != nil {
		t.Fatal(err)
	}
	carrier := protowire.AppendString(protowire.AppendTag(nil, 1, protowire.BytesType), "dev_example")
	carrierPayload, err := converter.GetDefaultDataConverter().ToPayload(carrier)
	if err != nil {
		t.Fatal(err)
	}
	env.SetHeader(&commonpb.Header{Fields: map[string]*commonpb.Payload{
		invocation.MetaHeaderKey:     payload,
		sdktemporal.CarrierHeaderKey: carrierPayload,
	}})
	env.RegisterWorkflowWithOptions(
		func(ctx workflow.Context, _ capability.InvokeInput) (invokeOutput, error) {
			gotMeta, ok := invocation.MetaFromWorkflow(ctx)
			if !ok || gotMeta != meta {
				return invokeOutput{}, fmt.Errorf("invocation meta = %+v, %t", gotMeta, ok)
			}
			gotLane, ok, err := sdktemporal.LaneFromWorkflow(ctx)
			if err != nil || !ok || gotLane != "dev_example" {
				return invokeOutput{}, fmt.Errorf("request lane = %q, %t, %v", gotLane, ok, err)
			}
			return invokeOutput{Value: "ok"}, nil
		},
		workflow.RegisterOptions{Name: capability.InvokeWorkflowType},
	)
	env.ExecuteWorkflow(func(ctx workflow.Context) error {
		return capability.Invoke(ctx, "contrib.demo.target", demoInput{}, &invokeOutput{})
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
}
