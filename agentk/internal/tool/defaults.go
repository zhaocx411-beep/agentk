package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
)

// WithDefaults returns a profile-scoped copy of spec. Defaults are validated
// against the tool's top-level input properties and exposed in its JSON Schema,
// so the model and the execution path observe the same configuration.
func WithDefaults(spec Spec, defaults map[string]any) (Spec, error) {
	if defaults == nil {
		return Spec{}, fmt.Errorf("tool %q: defaults must be an object", spec.Name)
	}

	var schema map[string]any
	if len(bytes.TrimSpace(spec.InputSchema)) == 0 {
		schema = map[string]any{"type": "object"}
	} else if err := json.Unmarshal(spec.InputSchema, &schema); err != nil {
		return Spec{}, fmt.Errorf("tool %q: decode input schema: %w", spec.Name, err)
	}
	properties, _ := schema["properties"].(map[string]any)
	for name, value := range defaults {
		property, ok := properties[name]
		if !ok {
			return Spec{}, fmt.Errorf("tool %q: unknown default parameter %q", spec.Name, name)
		}
		propertySchema, ok := property.(map[string]any)
		if !ok {
			return Spec{}, fmt.Errorf("tool %q: parameter %q has an invalid schema", spec.Name, name)
		}
		if err := validateDefaultValue(value, propertySchema); err != nil {
			return Spec{}, fmt.Errorf("tool %q: default parameter %q: %w", spec.Name, name, err)
		}
		propertySchema["default"] = value
		if description, ok := propertySchema["description"].(string); ok {
			propertySchema["description"] = fmt.Sprintf(
				"%s Profile 默认值为 %v；没有覆盖需求时省略该参数。", description, value)
		}
	}
	if required, ok := schema["required"].([]any); ok {
		filtered := make([]any, 0, len(required))
		for _, item := range required {
			name, _ := item.(string)
			if _, configured := defaults[name]; !configured {
				filtered = append(filtered, item)
			}
		}
		if len(filtered) == 0 {
			delete(schema, "required")
		} else {
			schema["required"] = filtered
		}
	}

	configuredSchema, err := json.Marshal(schema)
	if err != nil {
		return Spec{}, fmt.Errorf("tool %q: encode configured input schema: %w", spec.Name, err)
	}
	defaultArgs, err := json.Marshal(defaults)
	if err != nil {
		return Spec{}, fmt.Errorf("tool %q: encode defaults: %w", spec.Name, err)
	}
	spec.InputSchema = configuredSchema
	spec.DefaultArgs = defaultArgs
	return spec, nil
}

// ApplyDefaults shallow-merges profile defaults with one model tool call.
// Explicit model fields win. Malformed or non-object model arguments are left
// untouched so the normal tool validation path can report them to the model.
func (s Spec) ApplyDefaults(input json.RawMessage) json.RawMessage {
	if len(s.DefaultArgs) == 0 {
		return input
	}
	var defaults, explicit map[string]json.RawMessage
	if err := json.Unmarshal(s.DefaultArgs, &defaults); err != nil {
		return input // WithDefaults guarantees this cannot happen.
	}
	if err := json.Unmarshal(input, &explicit); err != nil || explicit == nil {
		return input
	}
	for name, value := range explicit {
		defaults[name] = value
	}
	merged, err := json.Marshal(defaults)
	if err != nil {
		return input
	}
	return merged
}

// validateDefaultValue implements the JSON Schema subset used by agent-k tool
// declarations. Runtime tool validators remain the final authority.
func validateDefaultValue(value any, schema map[string]any) error {
	if enum, ok := schema["enum"].([]any); ok {
		found := false
		for _, candidate := range enum {
			if reflect.DeepEqual(value, candidate) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("value %v is not in enum %v", value, enum)
		}
	}

	switch schemaType, _ := schema["type"].(string); schemaType {
	case "":
		return nil
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("must be a string")
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("must be a boolean")
		}
	case "number":
		if _, ok := value.(float64); !ok {
			return fmt.Errorf("must be a number")
		}
	case "integer":
		number, ok := value.(float64)
		if !ok || math.Trunc(number) != number {
			return fmt.Errorf("must be an integer")
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("must be an array")
		}
		if min, ok := schemaNumber(schema["minItems"]); ok && float64(len(items)) < min {
			return fmt.Errorf("must contain at least %d items", int(min))
		}
		if max, ok := schemaNumber(schema["maxItems"]); ok && float64(len(items)) > max {
			return fmt.Errorf("must contain at most %d items", int(max))
		}
		if itemSchema, ok := schema["items"].(map[string]any); ok {
			for i, item := range items {
				if err := validateDefaultValue(item, itemSchema); err != nil {
					return fmt.Errorf("item %d: %w", i, err)
				}
			}
		}
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return fmt.Errorf("must be an object")
		}
	default:
		return fmt.Errorf("unsupported schema type %q", schemaType)
	}

	if number, ok := value.(float64); ok {
		if min, ok := schemaNumber(schema["minimum"]); ok && number < min {
			return fmt.Errorf("must be at least %v", min)
		}
		if max, ok := schemaNumber(schema["maximum"]); ok && number > max {
			return fmt.Errorf("must be at most %v", max)
		}
	}
	return nil
}

func schemaNumber(value any) (float64, bool) {
	number, ok := value.(float64)
	return number, ok
}
