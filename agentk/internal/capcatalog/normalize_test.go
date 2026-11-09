package capcatalog

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestNormalizeManifestDigestIsStable(t *testing.T) {
	raw := testManifest("queue-a", "same")
	first, digestA, err := NormalizeManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw.Capabilities[0].InputSchema = json.RawMessage(
		`{"additionalProperties":false,"type":"object"}`)
	second, digestB, err := NormalizeManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if digestA != digestB {
		t.Fatalf("digests differ: %s != %s", digestA, digestB)
	}
	if string(first.Capabilities[0].InputSchema) !=
		string(second.Capabilities[0].InputSchema) {
		t.Fatalf("normalized schemas differ: %s != %s",
			first.Capabilities[0].InputSchema, second.Capabilities[0].InputSchema)
	}
}

func TestDecodeManifestObjectRejectsUnknownFields(t *testing.T) {
	var raw map[string]any
	encoded, err := json.Marshal(testManifest("queue-a", "v1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatal(err)
	}
	raw["legacy"] = true
	if _, err := DecodeManifestObject(raw); err == nil {
		t.Fatal("unknown field unexpectedly accepted")
	}
}

func TestDecodeManifestObjectAcceptsLegacySurfaceFields(t *testing.T) {
	var raw map[string]any
	encoded, err := json.Marshal(testManifest("queue-a", "v1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatal(err)
	}
	capability := raw["capabilities"].([]any)[0].(map[string]any)
	tool := capability["tool"].(map[string]any)
	tool["name"] = "generate_brand_ad"
	capability["pipeline"] = map[string]any{"name": "generate_brand_ad"}

	decoded, err := DecodeManifestObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Capabilities[0].Tool == nil ||
		decoded.Capabilities[0].Tool.Description != "v1" {
		t.Fatalf("decoded capability = %+v", decoded.Capabilities[0])
	}
	if _, ok := tool["name"]; !ok {
		t.Fatal("decoder mutated legacy tool object")
	}
	if _, ok := capability["pipeline"]; !ok {
		t.Fatal("decoder mutated legacy capability object")
	}
}

func TestResolveRejectsInvalidOrMissingSurface(t *testing.T) {
	catalog := NewMemory()
	manifest := testManifest("queue-a", "v1")
	manifest.Capabilities[0].Tool = nil
	if _, err := catalog.Register(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ResolveTool(
		t.Context(), "contrib.test.generate_brand_ad",
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing surface error = %v", err)
	}
	if _, err := catalog.ResolveTool(
		t.Context(), "tool:contrib.test.generate_brand_ad",
	); err == nil {
		t.Fatal("prefixed capability ID unexpectedly accepted")
	}
}
