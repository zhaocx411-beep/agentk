package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"agentk/internal/capcatalog"
	"agentk/internal/types"
)

func testEntry(name string) ProfileEntry {
	return ProfileEntry{
		Capability: "contrib.test." + name, Name: name, DisplayName: "Display " + name,
	}
}

func sessionWithEntries(entries ...ProfileEntry) types.Session {
	raw, _ := json.Marshal(map[string]any{"system": "s", "pipelines": entries})
	return types.Session{ID: 7, PublicID: "ses_test", TenantID: "org_test", Profile: raw}
}

func testCatalog(t *testing.T, entries ...ProfileEntry) *capcatalog.Memory {
	t.Helper()
	defs := make([]sdkcapability.CapabilityDef, 0, len(entries))
	for _, entry := range entries {
		defs = append(defs, sdkcapability.CapabilityDef{
			ID:             entry.Capability,
			Kind:           sdkcapability.CapabilityKindWorkflow,
			TemporalType:   "impl." + entry.Name,
			Description:    "Capability " + entry.Name,
			InputSchema:    json.RawMessage(`{"type":"object","additionalProperties":false,"required":["topic"],"properties":{"topic":{"type":"string"}}}`),
			OutputSchema:   json.RawMessage(`{"type":"object"}`),
			TimeoutSeconds: 3600,
		})
	}
	catalog := capcatalog.NewMemory()
	_, err := catalog.Register(context.Background(), sdkcapability.ManifestV1{
		ProtocolVersion: sdkcapability.ManifestProtocolVersion,
		TaskQueue:       "impl-queue",
		Capabilities:    defs,
	})
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func TestListReturnsResolvedItemsSortedAndOmitsMissing(t *testing.T) {
	alpha, zeta, missing := testEntry("alpha"), testEntry("zeta"), testEntry("missing")
	starter := NewStarter(StarterConfig{
		Catalog: testCatalog(t, alpha, zeta),
	})
	got, err := starter.List(t.Context(), sessionWithEntries(zeta, missing, alpha))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "alpha" || got[1].Name != "zeta" {
		t.Fatalf("list = %+v", got)
	}
	if got[0].CapabilityID != alpha.Capability ||
		got[0].Description != "Capability alpha" || len(got[0].InputSchema) == 0 {
		t.Fatalf("resolved item = %+v", got[0])
	}
}

func TestListReturnsUnavailableForCatalogFailure(t *testing.T) {
	starter := NewStarter(StarterConfig{})
	_, err := starter.List(t.Context(), sessionWithEntries(testEntry("one")))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestListResolvesActivityCapability(t *testing.T) {
	entry := testEntry("activity")
	catalog := capcatalog.NewMemory()
	_, err := catalog.Register(t.Context(), sdkcapability.ManifestV1{
		ProtocolVersion: sdkcapability.ManifestProtocolVersion,
		TaskQueue:       "activity-queue",
		Capabilities: []sdkcapability.CapabilityDef{{
			ID:             entry.Capability,
			Kind:           sdkcapability.CapabilityKindActivity,
			TemporalType:   "impl.activity",
			Description:    "Activity capability",
			InputSchema:    json.RawMessage(`{"type":"object"}`),
			OutputSchema:   json.RawMessage(`{"type":"object"}`),
			TimeoutSeconds: 60,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewStarter(StarterConfig{Catalog: catalog}).
		List(t.Context(), sessionWithEntries(entry))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].CapabilityID != entry.Capability ||
		got[0].Description != "Activity capability" {
		t.Fatalf("list = %+v", got)
	}
}

func TestStartRejectsUndeclaredBeforeTemporal(t *testing.T) {
	starter := NewStarter(StarterConfig{TaskQueue: "agentk"})
	_, err := starter.Start(t.Context(), sessionWithEntries(testEntry("one")),
		"missing", json.RawMessage(`{}`))
	if !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestStartResolvesAndValidatesInputSchema(t *testing.T) {
	one := testEntry("one")
	starter := NewStarter(StarterConfig{
		Catalog: testCatalog(t, one),
	})
	sess := sessionWithEntries(one)
	for _, args := range []json.RawMessage{
		json.RawMessage(`[1]`),
		json.RawMessage(`{}`),
		json.RawMessage(`{"topic":"猫","extra":true}`),
	} {
		if _, err := starter.Start(t.Context(), sess, "one", args); !errors.Is(err, ErrInvalidArguments) {
			t.Fatalf("args %s: err = %v, want ErrInvalidArguments", args, err)
		}
	}
	_, err := starter.Start(t.Context(), sess, "one", json.RawMessage(`{"topic":"猫"}`))
	if errors.Is(err, ErrInvalidArguments) || err == nil {
		t.Fatalf("valid args err = %v", err)
	}
}

func TestValidateArgsAcceptsNumbersWithoutRewritingPayload(t *testing.T) {
	schema := json.RawMessage(`{
		"type":"object",
		"required":["count","ratio"],
		"properties":{
			"count":{"type":"integer"},
			"ratio":{"type":"number"}
		}
	}`)
	args := json.RawMessage(`{"count":9007199254740993,"ratio":1.5}`)
	normalized, err := validateArgs(schema, args)
	if err != nil {
		t.Fatal(err)
	}

	decoder := json.NewDecoder(bytes.NewReader(normalized))
	decoder.UseNumber()
	var got map[string]any
	if err := decoder.Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["count"] != json.Number("9007199254740993") ||
		got["ratio"] != json.Number("1.5") {
		t.Fatalf("normalized args = %s", normalized)
	}
}

func TestStartReturnsUnavailableWhenRefIsMissing(t *testing.T) {
	one := testEntry("one")
	starter := NewStarter(StarterConfig{
		Catalog: capcatalog.NewMemory(),
	})
	_, err := starter.Start(t.Context(), sessionWithEntries(one), "one", json.RawMessage(`{}`))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

type fakeStartClient struct {
	options client.StartWorkflowOptions
	input   WorkflowInput
	err     error
}

func (f *fakeStartClient) ExecuteWorkflow(
	_ context.Context,
	options client.StartWorkflowOptions,
	_ any,
	args ...any,
) (client.WorkflowRun, error) {
	f.options = options
	f.input = args[0].(WorkflowInput)
	return nil, f.err
}

func TestStartUsesSessionExecutionSlotAndReturnsExecution(t *testing.T) {
	one := testEntry("one")
	temporalClient := &fakeStartClient{}
	starter := NewStarter(StarterConfig{
		Temporal: temporalClient, TaskQueue: "agentk", Catalog: testCatalog(t, one),
	})
	sess := sessionWithEntries(one)
	exec, err := starter.Start(t.Context(), sess, "one", json.RawMessage(`{"topic":"猫"}`))
	if err != nil {
		t.Fatal(err)
	}
	if exec.ID == "" || exec.Kind != types.ExecutionKindPipeline || exec.Pipeline == nil ||
		temporalClient.input.Execution.ID != exec.ID {
		t.Fatalf("execution = %+v, input = %+v", exec, temporalClient.input)
	}
	if temporalClient.options.ID != "session/"+sess.PublicID ||
		temporalClient.options.WorkflowIDConflictPolicy != enumspb.WORKFLOW_ID_CONFLICT_POLICY_FAIL ||
		temporalClient.options.WorkflowIDReusePolicy != enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE ||
		!temporalClient.options.WorkflowExecutionErrorWhenAlreadyStarted {
		t.Fatalf("start options = %+v", temporalClient.options)
	}
}

func TestStartMapsActiveExecution(t *testing.T) {
	one := testEntry("one")
	starter := NewStarter(StarterConfig{
		Temporal:  &fakeStartClient{err: &serviceerror.WorkflowExecutionAlreadyStarted{}},
		TaskQueue: "agentk", Catalog: testCatalog(t, one),
	})
	_, err := starter.Start(t.Context(), sessionWithEntries(one), "one",
		json.RawMessage(`{"topic":"猫"}`))
	if !errors.Is(err, types.ErrExecutionActive) {
		t.Fatalf("error = %v", err)
	}
}

func TestProfileEntryAndTargetValidate(t *testing.T) {
	if err := testEntry("ok").Validate(); err != nil {
		t.Fatal(err)
	}
	prefixed := testEntry("bad")
	prefixed.Capability = "pipeline:contrib.test.bad"
	if err := prefixed.Validate(); err == nil {
		t.Fatal("prefixed capability ID unexpectedly accepted")
	}
	target := Target{
		Kind: KindWorkflow, Type: "impl.ok", TaskQueue: "queue", TimeoutSeconds: 60,
	}
	if err := target.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Target){
		"bad kind":     func(d *Target) { d.Kind = "service" },
		"empty type":   func(d *Target) { d.Type = "" },
		"empty queue":  func(d *Target) { d.TaskQueue = "" },
		"zero timeout": func(d *Target) { d.TimeoutSeconds = 0 },
		"huge timeout": func(d *Target) { d.TimeoutSeconds = maxTimeoutSeconds + 1 },
	} {
		bad := target
		mutate(&bad)
		if err := bad.Validate(); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestEntriesNormalizesLegacyRef(t *testing.T) {
	entries := Entries([]byte(`{"pipelines":[{
		"ref":"pipeline:contrib.test.legacy",
		"name":"legacy"
	}]}`))
	if len(entries) != 1 ||
		entries[0].Capability != "contrib.test.legacy" ||
		entries[0].Ref != "" {
		t.Fatalf("entries = %+v", entries)
	}
	if got := Entries([]byte(`{"pipelines":[{
		"ref":"tool:contrib.test.bad",
		"name":"bad"
	}]}`)); got != nil {
		t.Fatalf("invalid legacy entries = %+v", got)
	}
}
