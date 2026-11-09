package invocation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestInvocationFixturesConformToProtocolSchemas(t *testing.T) {
	fixtures := map[string]string{
		"meta":                  "meta",
		"meta-direct":           "meta",
		"report-progress-input": "report-progress-input",
	}
	for name, schemaName := range fixtures {
		t.Run(name, func(t *testing.T) {
			schema := loadInvocationProtocolSchema(t, schemaName+".schema.json")
			value := loadInvocationProtocolJSON(t, filepath.Join("testdata", name+".json"))
			if err := schema.Validate(value); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInvocationProtocolSchemasRejectInvalidWireValues(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		value  string
	}{
		{
			name: "unknown meta field", schema: "meta.schema.json",
			value: `{
				"sessionId":7,"session":"ses_test","appId":"0198d72a-7c00-7000-8000-000000000001",
				"invocationId":"exe_test","invocationKind":"pipeline",
				"invocationName":"summarize","callbackTaskQueue":"agentk-test",
				"pipeline":"legacy"
			}`,
		},
		{
			name: "legacy tenant meta field", schema: "meta.schema.json",
			value: `{
				"sessionId":7,"session":"ses_test","tenantId":"org_legacy",
				"invocationId":"exe_test","invocationKind":"pipeline",
				"invocationName":"summarize","callbackTaskQueue":"agentk-test"
			}`,
		},
		{
			name: "session without callback queue", schema: "meta.schema.json",
			value: `{
				"sessionId":7,"session":"ses_test","appId":"0198d72a-7c00-7000-8000-000000000001",
				"invocationId":"exe_test","invocationKind":"pipeline","invocationName":"summarize"
			}`,
		},
		{
			name: "direct meta without app", schema: "meta.schema.json",
			value: `{
				"invocationId":"inv_test","invocationKind":"direct",
				"invocationName":"contrib.acme.summarize"
			}`,
		},
		{
			name: "invalid kind", schema: "report-progress-input.schema.json",
			value: `{
				"sessionId":7,"invocationId":"exe_test","invocationKind":"batch",
				"invocationName":"summarize","stepId":"clip/0"
			}`,
		},
		{
			name: "missing step", schema: "report-progress-input.schema.json",
			value: `{
				"sessionId":7,"invocationId":"exe_test","invocationKind":"pipeline",
				"invocationName":"summarize"
			}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := loadInvocationProtocolSchema(t, tt.schema)
			var value any
			if err := json.Unmarshal([]byte(tt.value), &value); err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(value); err == nil {
				t.Fatal("value unexpectedly passed validation")
			}
		})
	}
}

func loadInvocationProtocolSchema(t *testing.T, path string) *jsonschema.Resolved {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(invocationProtocolRoot(), path))
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

func loadInvocationProtocolJSON(t *testing.T, path string) any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(invocationProtocolRoot(), path))
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func invocationProtocolRoot() string {
	return filepath.Join("..", "..", "..", "protocol", "agentk", "invocation", "v1")
}
