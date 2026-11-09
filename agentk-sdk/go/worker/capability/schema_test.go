package capability

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"
)

type schemaFixture struct {
	Title  string `json:"title" jsonschema:"description=任务标题"`
	Note   string `json:"note,omitempty" jsonschema:"description=可选备注"`
	Nested struct {
		Count int `json:"count"`
	} `json:"nested"`
}

func TestSchemaOfUsesJSONContract(t *testing.T) {
	raw := SchemaOf[schemaFixture]()
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	if schema["type"] != "object" {
		t.Fatalf("type = %v", schema["type"])
	}
	if schema["additionalProperties"] != false {
		t.Fatalf("additionalProperties = %v", schema["additionalProperties"])
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %#v", schema["properties"])
	}
	title, ok := properties["title"].(map[string]any)
	if !ok || title["description"] != "任务标题" {
		t.Fatalf("title schema = %#v", properties["title"])
	}
	nested, ok := properties["nested"].(map[string]any)
	if !ok || nested["additionalProperties"] != false {
		t.Fatalf("nested schema = %#v", properties["nested"])
	}
	required := stringList(t, schema["required"])
	slices.Sort(required)
	if !slices.Equal(required, []string{"nested", "title"}) {
		t.Fatalf("required = %v", required)
	}
	if bytes.Contains(raw, []byte(`"$ref"`)) || bytes.Contains(raw, []byte(`"$defs"`)) {
		t.Fatalf("schema must be inlined: %s", raw)
	}
}

func TestSchemaOfIsDeterministic(t *testing.T) {
	first := SchemaOf[schemaFixture]()
	for i := 0; i < 5; i++ {
		if got := SchemaOf[schemaFixture](); !bytes.Equal(got, first) {
			t.Fatalf("schema changed:\nfirst=%s\ngot=%s", first, got)
		}
	}
}

func TestNormalizeSchemaCanonicalizesExplicitJSON(t *testing.T) {
	left, err := normalizeSchema(json.RawMessage(`{
		"properties": {"name": {"minLength": 1, "type": "string"}},
		"type": "object"
	}`), true)
	if err != nil {
		t.Fatal(err)
	}
	right, err := normalizeSchema(json.RawMessage(
		`{"type":"object","properties":{"name":{"type":"string","minLength":1}}}`,
	), true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(left, right) {
		t.Fatalf("normalized schemas differ:\n%s\n%s", left, right)
	}
}

func TestNormalizeSchemaRejectsInvalidInputSchema(t *testing.T) {
	tests := []struct {
		name   string
		schema string
	}{
		{name: "empty", schema: ""},
		{name: "invalid JSON", schema: `{`},
		{name: "multiple values", schema: `{} {}`},
		{name: "array document", schema: `[]`},
		{name: "missing type", schema: `{"properties":{}}`},
		{name: "non-object input", schema: `{"type":"string"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := normalizeSchema(json.RawMessage(tt.schema), true); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func stringList(t *testing.T, value any) []string {
	t.Helper()
	raw, ok := value.([]any)
	if !ok {
		t.Fatalf("want []any, got %#v", value)
	}
	out := make([]string, len(raw))
	for i, item := range raw {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("item %d = %#v", i, item)
		}
		out[i] = s
	}
	return out
}
