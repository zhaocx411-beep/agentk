package videogen

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
)

func TestWorkflowManifests(t *testing.T) {
	want := []struct {
		id           string
		workflowType string
		model        string
		required     string
	}{
		{"agentk.kling_text_to_video", KlingT2VWorkflowType,
			"kling-3.0-t2v", `"required":["model","prompt"]`},
		{"agentk.kling_image_to_video", KlingI2VWorkflowType,
			"kling-3.0", `"required":["model","firstFrame","prompt"]`},
		{"agentk.kling_omni_video", KlingOmniWorkflowType,
			"kling-3.0-omni", `"required":["model","prompt"]`},
		{"agentk.seedance_generate_video", SeedanceWorkflowType,
			"doubao-seedance-2.0", `"required":["model","prompt"]`},
	}
	manifests := ManifestWorkflowCapabilities()
	if len(manifests) != len(want) {
		t.Fatalf("manifests = %+v", manifests)
	}
	for i, expected := range want {
		manifest := manifests[i]
		if manifest.ID != expected.id || manifest.TemporalType != expected.workflowType ||
			manifest.Tool == nil || manifest.Tool.Mode != sdkcapability.ToolModeBlocking {
			t.Fatalf("manifest[%d] = %+v", i, manifest)
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, manifest.InputSchema); err != nil {
			t.Fatalf("%s input schema: %v", manifest.ID, err)
		}
		schema := compact.String()
		if !strings.Contains(schema, `"`+expected.model+`"`) ||
			!strings.Contains(schema, expected.required) {
			t.Fatalf("%s schema = %s", manifest.ID, schema)
		}
		if !json.Valid(manifest.OutputSchema) ||
			!strings.Contains(string(manifest.OutputSchema), `"artifacts"`) {
			t.Fatalf("%s output schema = %s", manifest.ID, manifest.OutputSchema)
		}
	}
}

func TestActivityManifestRemainsBlockingCapabilityOnly(t *testing.T) {
	manifest := ManifestActivityCapability()
	if manifest.ID != "agentk.generate_video" || manifest.TemporalType != ActivityName ||
		manifest.Tool != nil || manifest.TimeoutSeconds <= 0 {
		t.Fatalf("manifest = %+v", manifest)
	}
}
