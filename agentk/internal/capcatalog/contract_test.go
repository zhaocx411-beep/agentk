package capcatalog

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
)

func testManifest(taskQueue, description string) sdkcapability.ManifestV1 {
	return sdkcapability.ManifestV1{
		ProtocolVersion: sdkcapability.ManifestProtocolVersion,
		TaskQueue:       taskQueue,
		Capabilities: []sdkcapability.CapabilityDef{{
			ID:             "contrib.test.generate_brand_ad",
			Kind:           sdkcapability.CapabilityKindWorkflow,
			TemporalType:   "brandad.generate",
			Description:    description,
			InputSchema:    json.RawMessage(`{"type":"object","additionalProperties":false}`),
			OutputSchema:   json.RawMessage(`{"type":"object"}`),
			TimeoutSeconds: 600,
			Tool: &sdkcapability.ToolDecl{
				Mode:        sdkcapability.ToolModeBlocking,
				Description: description,
			},
		}},
	}
}

func runCatalogContract(
	t *testing.T,
	catalog Catalog,
	advance func(),
) {
	t.Helper()
	ctx := context.Background()

	first, err := catalog.Register(ctx, testManifest("queue-a", "v1"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest == "" || first.Manifest.TaskQueue != "queue-a" {
		t.Fatalf("record = %+v", first)
	}
	tool, err := catalog.ResolveTool(ctx, "contrib.test.generate_brand_ad")
	if err != nil {
		t.Fatal(err)
	}
	if tool.Tool.Mode != sdkcapability.ToolModeBlocking {
		t.Fatalf("tool = %+v", tool)
	}
	listed := listCapabilities(t, catalog)
	if len(listed) != 1 ||
		listed[0].Capability.ID != "contrib.test.generate_brand_ad" ||
		listed[0].TaskQueue != "queue-a" ||
		listed[0].Capability.Description != "v1" {
		t.Fatalf("list = %+v", listed)
	}

	updated, err := catalog.Register(ctx, testManifest("queue-a", "v2"))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Digest == first.Digest {
		t.Fatal("digest did not change")
	}
	got, err := catalog.ResolveCapability(ctx, "contrib.test.generate_brand_ad")
	if err != nil {
		t.Fatal(err)
	}
	if got.Capability.Description != "v2" {
		t.Fatalf("description = %q", got.Capability.Description)
	}
	listed = listCapabilities(t, catalog)
	if len(listed) != 1 || listed[0].Capability.Description != "v2" {
		t.Fatalf("list after update = %+v", listed)
	}

	if _, err := catalog.Register(
		ctx, testManifest("queue-b", "conflict"),
	); !errors.Is(err, ErrRefConflict) {
		t.Fatalf("conflict error = %v", err)
	}
	got, err = catalog.ResolveCapability(ctx, "contrib.test.generate_brand_ad")
	if err != nil || got.TaskQueue != "queue-a" {
		t.Fatalf("old owner changed: got=%+v err=%v", got, err)
	}

	advance()
	if _, err := catalog.ResolveCapability(
		ctx, "contrib.test.generate_brand_ad",
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired resolve error = %v", err)
	}
	if listed = listCapabilities(t, catalog); len(listed) != 0 {
		t.Fatalf("list after expiry = %+v", listed)
	}
	if _, err := catalog.Register(
		ctx, testManifest("queue-b", "takeover"),
	); err != nil {
		t.Fatalf("takeover: %v", err)
	}
	got, err = catalog.ResolveCapability(ctx, "contrib.test.generate_brand_ad")
	if err != nil || got.TaskQueue != "queue-b" {
		t.Fatalf("takeover result: got=%+v err=%v", got, err)
	}
	listed = listCapabilities(t, catalog)
	if len(listed) != 1 || listed[0].TaskQueue != "queue-b" {
		t.Fatalf("list after takeover = %+v", listed)
	}
}

func listCapabilities(
	t *testing.T,
	catalog Catalog,
) []ResolvedCapability {
	t.Helper()
	items, err := catalog.ListCapabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func TestMemoryCatalogContract(t *testing.T) {
	now := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	catalog := newMemory(func() time.Time { return now }, time.Hour)
	runCatalogContract(t, catalog, func() { now = now.Add(time.Hour + time.Second) })
}

func TestKedisCatalogContract(t *testing.T) {
	resource := os.Getenv("AGENTK_REDIS_RESOURCE")
	if resource == "" {
		t.Skip("set AGENTK_REDIS_RESOURCE to run Kedis catalog contract")
	}
	catalog, err := NewKedis(resource)
	if err != nil {
		t.Fatal(err)
	}
	token, err := lockToken()
	if err != nil {
		t.Fatal(err)
	}
	catalog.keyPrefix = "agentk:capcatalog:test:" + token + ":"
	// Redis EXPIRE 的最小粒度是 1 秒，TTL 必须取整秒才能覆盖 task queue 集合。
	catalog.ttl = 2 * time.Second
	runCatalogContract(t, catalog, func() { time.Sleep(3 * time.Second) })
}
