package discovery

import (
	"encoding/json"
	"errors"
	"testing"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
	"go.temporal.io/sdk/temporal"

	"agentk/internal/capcatalog"
)

func discoveryManifest(taskQueue string) sdkcapability.ManifestV1 {
	return sdkcapability.ManifestV1{
		ProtocolVersion: sdkcapability.ManifestProtocolVersion,
		TaskQueue:       taskQueue,
		Capabilities: []sdkcapability.CapabilityDef{{
			ID:             "contrib.example.summarize",
			Kind:           sdkcapability.CapabilityKindWorkflow,
			TemporalType:   "example.summarize",
			Description:    "Summarize text",
			InputSchema:    json.RawMessage(`{"type":"object"}`),
			OutputSchema:   json.RawMessage(`{"type":"object"}`),
			TimeoutSeconds: 300,
		}},
	}
}

func manifestObject(t *testing.T, manifest sdkcapability.ManifestV1) map[string]any {
	t.Helper()
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	return object
}

func TestRegisterManifestActivity(t *testing.T) {
	catalog := capcatalog.NewMemory()
	activities := NewActivities(catalog)
	if err := activities.RegisterManifest(t.Context(), RegisterActivityInput{
		Manifest: manifestObject(t, discoveryManifest("business")),
	}); err != nil {
		t.Fatal(err)
	}
	got, err := catalog.ResolveCapability(
		t.Context(), "contrib.example.summarize")
	if err != nil {
		t.Fatal(err)
	}
	if got.TaskQueue != "business" || got.Capability.Description != "Summarize text" {
		t.Fatalf("resolved = %+v", got)
	}
}

func TestRegisterManifestActivityRejectsInvalidAndConflict(t *testing.T) {
	catalog := capcatalog.NewMemory()
	activities := NewActivities(catalog)
	valid := manifestObject(t, discoveryManifest("queue-a"))
	if err := activities.RegisterManifest(t.Context(), RegisterActivityInput{
		Manifest: valid,
	}); err != nil {
		t.Fatal(err)
	}

	unknown := manifestObject(t, discoveryManifest("queue-a"))
	unknown["legacy"] = true
	assertNonRetryable(t, activities.RegisterManifest(t.Context(), RegisterActivityInput{
		Manifest: unknown,
	}))
	assertNonRetryable(t, activities.RegisterManifest(t.Context(), RegisterActivityInput{
		Manifest: manifestObject(t, discoveryManifest("queue-b")),
	}))
	assertNonRetryable(t, activities.RegisterManifest(t.Context(), RegisterActivityInput{
		Manifest: manifestObject(t, discoveryManifest("agentk")),
	}))
	core := discoveryManifest("other-queue")
	core.Capabilities[0].ID = "agentk.summarize"
	assertNonRetryable(t, activities.RegisterManifest(t.Context(), RegisterActivityInput{
		Manifest: manifestObject(t, core),
	}))
	assertNonRetryable(t, NewActivities(nil).RegisterManifest(
		t.Context(), RegisterActivityInput{
			Manifest: valid,
		}))
}

func assertNonRetryable(t *testing.T, err error) {
	t.Helper()
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) || !appErr.NonRetryable() {
		t.Fatalf("error = %v, want non-retryable ApplicationError", err)
	}
}
