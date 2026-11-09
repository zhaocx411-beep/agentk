package capability

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/invopop/jsonschema"
)

// SchemaOf returns a deterministic, fully inlined JSON Schema for T. Field
// names and optional fields follow encoding/json: fields without omitempty are
// required, and generated object schemas reject unknown properties.
func SchemaOf[T any]() json.RawMessage {
	reflector := jsonschema.Reflector{
		DoNotReference:            true,
		AllowAdditionalProperties: false,
	}
	schema := reflector.Reflect(new(T))
	raw, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("capability: generate JSON schema: %v", err))
	}
	return raw
}

func normalizeSchema(raw json.RawMessage, requireObject bool) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("schema is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var schema map[string]any
	if err := decoder.Decode(&schema); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if schema == nil {
		return nil, fmt.Errorf("schema must be a JSON object")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values are not allowed")
		}
		return nil, fmt.Errorf("invalid trailing JSON: %w", err)
	}
	if requireObject {
		typ, ok := schema["type"].(string)
		if !ok || typ != "object" {
			return nil, fmt.Errorf(`schema type must be "object"`)
		}
	}
	normalized, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("normalize: %w", err)
	}
	return normalized, nil
}
