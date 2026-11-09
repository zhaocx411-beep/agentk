package asr

import (
	"strings"
	"testing"
)

func TestManifestCapability(t *testing.T) {
	manifest := ManifestCapability()
	if manifest.ID != CapabilityID || manifest.Tool == nil {
		t.Fatalf("manifest = %+v", manifest)
	}
	schema := string(manifest.InputSchema)
	for _, want := range []string{
		`"azure-whisper"`, `"audioUrl"`, `"task"`, `"transcribe"`, `"translate"`,
		`"language"`, `"prompt"`, `Azure Whisper`,
	} {
		if !strings.Contains(schema, want) {
			t.Fatalf("schema missing %q: %s", want, schema)
		}
	}
}
