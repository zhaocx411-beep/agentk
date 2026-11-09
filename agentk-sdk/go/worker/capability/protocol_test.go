package capability

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestManifestMatchesProtocolFixtures(t *testing.T) {
	schema := loadManifestProtocolSchema(t)
	for _, name := range []string{"basic.json", "tool.json"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(capabilityProtocolRoot(), "testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			var expected map[string]any
			if err := json.Unmarshal(raw, &expected); err != nil {
				t.Fatal(err)
			}
			manifest := ManifestV1{ProtocolVersion: 1, TaskQueue: expected["taskQueue"].(string)}
			manifest.LaneID, _ = expected["laneId"].(string)
			for _, item := range expected["capabilities"].([]any) {
				value := item.(map[string]any)
				input, err := json.Marshal(value["inputSchema"])
				if err != nil {
					t.Fatal(err)
				}
				output, err := json.Marshal(value["outputSchema"])
				if err != nil {
					t.Fatal(err)
				}
				def := CapabilityDef{
					ID: value["id"].(string), Kind: CapabilityKind(value["kind"].(string)),
					TemporalType: value["temporalType"].(string), Description: value["description"].(string),
					InputSchema: input, OutputSchema: output, TimeoutSeconds: int64(value["timeoutSeconds"].(float64)),
				}
				if tool, ok := value["tool"].(map[string]any); ok {
					def.Tool = &ToolDecl{Mode: ToolMode(tool["mode"].(string)), MaxAttempts: int32(tool["maxAttempts"].(float64))}
				}
				manifest.Capabilities = append(manifest.Capabilities, def)
			}
			normalized, err := manifest.Normalize()
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(normalized)
			if err != nil {
				t.Fatal(err)
			}
			var actual any
			if err := json.Unmarshal(encoded, &actual); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, expected) {
				t.Fatalf("manifest differs from %s:\n%s", name, encoded)
			}
			if err := schema.Validate(actual); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestManifestProtocolSchemaRejectsUnknownFields(t *testing.T) {
	schema := loadManifestProtocolSchema(t)
	raw, err := os.ReadFile(filepath.Join(capabilityProtocolRoot(), "testdata", "basic.json"))
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	value["legacy"] = true
	if err := schema.Validate(value); err == nil {
		t.Fatal("unknown field unexpectedly passed validation")
	}
}

func TestManifestProtocolSchemaRejectsInvalidLane(t *testing.T) {
	schema := loadManifestProtocolSchema(t)
	raw, err := os.ReadFile(filepath.Join(capabilityProtocolRoot(), "testdata", "tool.json"))
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	value["laneId"] = "dev lane"
	if err := schema.Validate(value); err == nil {
		t.Fatal("invalid laneId unexpectedly passed validation")
	}
}

func TestManifestProtocolSchemaRejectsArtifactDeclaration(t *testing.T) {
	schema := loadManifestProtocolSchema(t)
	raw, err := os.ReadFile(filepath.Join(capabilityProtocolRoot(), "testdata", "basic.json"))
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	capabilities := value["capabilities"].([]any)
	definition := capabilities[0].(map[string]any)
	definition["artifact"] = map[string]any{
		"type":     "video",
		"uriField": "videoUrl",
	}
	if err := schema.Validate(value); err == nil {
		t.Fatal("artifact declaration unexpectedly passed validation")
	}
}

func TestManifestProtocolSchemaRejectsRemovedExposureFields(t *testing.T) {
	schema := loadManifestProtocolSchema(t)
	raw, err := os.ReadFile(filepath.Join(capabilityProtocolRoot(), "testdata", "tool.json"))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		edit func(map[string]any)
	}{
		{
			name: "tool name",
			edit: func(definition map[string]any) {
				definition["tool"].(map[string]any)["name"] = "legacy_tool"
			},
		},
		{
			name: "pipeline",
			edit: func(definition map[string]any) {
				definition["pipeline"] = map[string]any{
					"name":        "legacy_pipeline",
					"description": "Legacy pipeline",
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(raw, &value); err != nil {
				t.Fatal(err)
			}
			definition := value["capabilities"].([]any)[0].(map[string]any)
			tt.edit(definition)
			if err := schema.Validate(value); err == nil {
				t.Fatal("removed exposure field unexpectedly passed validation")
			}
		})
	}
}

func TestManifestProtocolSchemaRejectsInvalidCapabilityFields(t *testing.T) {
	schema := loadManifestProtocolSchema(t)
	raw, err := os.ReadFile(filepath.Join(capabilityProtocolRoot(), "testdata", "tool.json"))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		edit func(map[string]any)
	}{
		{
			name: "id",
			edit: func(definition map[string]any) {
				definition["id"] = "example.generate"
			},
		},
		{
			name: "max attempts",
			edit: func(definition map[string]any) {
				definition["tool"].(map[string]any)["maxAttempts"] = -1
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(raw, &value); err != nil {
				t.Fatal(err)
			}
			definition := value["capabilities"].([]any)[0].(map[string]any)
			tt.edit(definition)
			if err := schema.Validate(value); err == nil {
				t.Fatal("invalid capability field unexpectedly passed validation")
			}
		})
	}
}

func TestInvokeInputFixtureConformsToProtocolSchema(t *testing.T) {
	schema := loadProtocolSchema(t, "invoke-input.schema.json")
	raw, err := os.ReadFile(filepath.Join(capabilityProtocolRoot(), "testdata", "invoke-input.json"))
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(value); err != nil {
		t.Fatal(err)
	}
}

func TestInvokeInputFixtureMatchesGoWire(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join(capabilityProtocolRoot(), "testdata", "invoke-input.json"))
	if err != nil {
		t.Fatal(err)
	}
	var input invokeInput
	if err := json.Unmarshal(fixture, &input); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	if err := json.Unmarshal(fixture, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	wantJSON, _ := json.Marshal(want)
	gotJSON, _ := json.Marshal(got)
	if string(wantJSON) != string(gotJSON) {
		t.Fatalf("wire mismatch:\n%s\n%s", fixture, encoded)
	}
}

func loadManifestProtocolSchema(t *testing.T) *jsonschema.Resolved {
	t.Helper()
	return loadProtocolSchema(t, "manifest.schema.json")
}

func loadProtocolSchema(t *testing.T, name string) *jsonschema.Resolved {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(capabilityProtocolRoot(), name))
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func capabilityProtocolRoot() string {
	return filepath.Join("..", "..", "..", "protocol", "agentk", "capability", "v1")
}
