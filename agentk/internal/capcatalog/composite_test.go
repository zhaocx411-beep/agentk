package capcatalog

import (
	"encoding/json"
	"errors"
	"testing"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
)

func coreTestManifest() sdkcapability.ManifestV1 {
	return sdkcapability.ManifestV1{
		ProtocolVersion: sdkcapability.ManifestProtocolVersion,
		TaskQueue:       coreTaskQueue,
		Capabilities: []sdkcapability.CapabilityDef{{
			ID:             "agentk.core_echo",
			Kind:           sdkcapability.CapabilityKindActivity,
			TemporalType:   "agentk.capability.core_echo",
			Description:    "core",
			InputSchema:    json.RawMessage(`{"type":"object"}`),
			OutputSchema:   json.RawMessage(`{"type":"object"}`),
			TimeoutSeconds: 30,
			Tool: &sdkcapability.ToolDecl{
				Mode: sdkcapability.ToolModeBlocking,
			},
		}},
	}
}

func TestCompositeRoutesCoreAndContribWithoutFallback(t *testing.T) {
	dynamic := NewMemory()
	catalog, err := NewComposite(coreTestManifest(), dynamic)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Register(
		t.Context(), testManifest("contrib-queue", "external"),
	); err != nil {
		t.Fatal(err)
	}

	core, err := catalog.ResolveTool(t.Context(), "agentk.core_echo")
	if err != nil {
		t.Fatal(err)
	}
	if core.TaskQueue != coreTaskQueue || !core.LastReportedAt.IsZero() {
		t.Fatalf("core = %+v", core)
	}
	external, err := catalog.ResolveCapability(
		t.Context(), "contrib.test.generate_brand_ad")
	if err != nil {
		t.Fatal(err)
	}
	if external.TaskQueue != "contrib-queue" {
		t.Fatalf("external = %+v", external)
	}
	listed := listCapabilities(t, catalog)
	if len(listed) != 2 ||
		listed[0].Capability.ID != "agentk.core_echo" ||
		listed[1].Capability.ID != "contrib.test.generate_brand_ad" {
		t.Fatalf("list = %+v", listed)
	}

	if _, err := dynamic.Register(t.Context(), coreTestManifest()); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ResolveCapability(
		t.Context(), "agentk.missing",
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("core miss fell through to dynamic: %v", err)
	}
	if _, err := catalog.ListCapabilities(t.Context()); err == nil {
		t.Fatal("list accepted a core capability from the dynamic catalog")
	}
}

func TestCompositeRejectsInvalidContribManifest(t *testing.T) {
	catalog, err := NewComposite(coreTestManifest(), NewMemory())
	if err != nil {
		t.Fatal(err)
	}
	for _, manifest := range []sdkcapability.ManifestV1{
		testManifest(coreTaskQueue, "reserved queue"),
		coreTestManifest(),
	} {
		if _, err := catalog.Register(t.Context(), manifest); err == nil {
			t.Fatalf("manifest unexpectedly accepted: %+v", manifest)
		}
	}
	background := testManifest("contrib-queue", "background")
	background.Capabilities[0].Tool.Mode = sdkcapability.ToolModeBackground
	if _, err := catalog.Register(t.Context(), background); err == nil {
		t.Fatal("manifest with background tool unexpectedly accepted")
	}
}

func TestCompositeRejectsStoredContribOnCoreQueue(t *testing.T) {
	dynamic := NewMemory()
	if _, err := dynamic.Register(
		t.Context(), testManifest(coreTaskQueue, "reserved queue"),
	); err != nil {
		t.Fatal(err)
	}
	catalog, err := NewComposite(coreTestManifest(), dynamic)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ResolveCapability(
		t.Context(), "contrib.test.generate_brand_ad",
	); err == nil {
		t.Fatal("resolve accepted a contrib capability on the core queue")
	}
}

func TestCompositeRejectsStoredBackgroundContrib(t *testing.T) {
	dynamic := NewMemory()
	background := testManifest("contrib-queue", "background")
	background.Capabilities[0].Tool.Mode = sdkcapability.ToolModeBackground
	if _, err := dynamic.Register(t.Context(), background); err != nil {
		t.Fatal(err)
	}
	catalog, err := NewComposite(coreTestManifest(), dynamic)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ResolveCapability(
		t.Context(), "contrib.test.generate_brand_ad",
	); err == nil {
		t.Fatal("resolve accepted a stored background capability")
	}
	if _, err := catalog.ListCapabilities(t.Context()); err == nil {
		t.Fatal("list accepted a stored background capability")
	}
}
