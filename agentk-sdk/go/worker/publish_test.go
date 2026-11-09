package worker

import (
	"encoding/json"
	"testing"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
)

var (
	testInputSchema  = json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`)
	testOutputSchema = json.RawMessage(`{"type":"object","properties":{"summary":{"type":"string"}},"required":["summary"]}`)
)

func validCapability(id string) capability.Capability {
	return capability.Capability{
		ID:             id,
		TemporalType:   id,
		Description:    "测试能力",
		InputSchema:    testInputSchema,
		OutputSchema:   testOutputSchema,
		TimeoutSeconds: 60,
	}
}

func TestPublishInjectsKindsAndCarriesWholeDeclaration(t *testing.T) {
	collector := newManifestCollector()
	workflowCapability := validCapability("contrib.demo.workflow")
	workflowCapability.Tool = &capability.Tool{
		MaxAttempts: 3,
	}
	collector.add(capability.CapabilityKindWorkflow, workflowCapability)

	activityCapability := validCapability("contrib.demo.activity")
	activityCapability.Tool = &capability.Tool{}
	collector.add(capability.CapabilityKindActivity, activityCapability)

	manifest, err := collector.buildAndFreeze("demo-queue", "dev_example")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Capabilities[0].Kind != capability.CapabilityKindActivity {
		t.Fatalf("activity kind = %q", manifest.Capabilities[0].Kind)
	}
	if manifest.TaskQueue != "demo-queue" || manifest.LaneID != "dev_example" {
		t.Fatalf("manifest route = queue %q lane %q", manifest.TaskQueue, manifest.LaneID)
	}
	workflow := manifest.Capabilities[1]
	if workflow.Kind != capability.CapabilityKindWorkflow {
		t.Fatalf("workflow kind = %q", workflow.Kind)
	}
	if workflow.Tool == nil || workflow.Tool.Mode != capability.ToolModeBlocking ||
		workflow.Tool.MaxAttempts != 3 {
		t.Fatalf("tool = %+v", workflow.Tool)
	}
}

func TestPublishSnapshotsCallerDeclaration(t *testing.T) {
	collector := newManifestCollector()
	input := append(json.RawMessage(nil), testInputSchema...)
	declared := validCapability("contrib.demo.copy")
	declared.InputSchema = input
	declared.Tool = &capability.Tool{
		Description: "copy",
	}
	collector.add(capability.CapabilityKindWorkflow, declared)

	input[0] = 'x'
	declared.Tool.Description = "mutated"

	manifest, err := collector.buildAndFreeze("demo-queue", "")
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(manifest.Capabilities[0].InputSchema) {
		t.Fatalf("input schema mutated: %s", manifest.Capabilities[0].InputSchema)
	}
	if manifest.Capabilities[0].Tool.Description != "copy" {
		t.Fatalf("tool mutated: %+v", manifest.Capabilities[0].Tool)
	}
}
