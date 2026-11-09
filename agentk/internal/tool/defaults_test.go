package tool

import (
	"encoding/json"
	"strings"
	"testing"
)

func defaultsSpec() Spec {
	return Spec{
		Name: "video",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"resolution":{"type":"string","enum":["720p","1080p"],"description":"输出分辨率。"},
				"audio":{"type":"boolean"},
				"duration":{"type":"integer","minimum":3,"maximum":15},
				"references":{"type":"array","maxItems":2,"items":{"type":"string"}}
			},
			"required":["resolution","references"]
		}`),
		Binding: InlineBinding{},
	}
}

func TestWithDefaultsExposesAndAppliesProfileValues(t *testing.T) {
	spec, err := WithDefaults(defaultsSpec(), map[string]any{
		"resolution": "1080p",
		"audio":      true,
		"duration":   float64(5),
	})
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]struct {
			Default     any    `json:"default"`
			Description string `json:"description"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(spec.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Properties["resolution"].Default != "1080p" ||
		schema.Properties["audio"].Default != true ||
		len(schema.Required) != 1 || schema.Required[0] != "references" ||
		!strings.Contains(schema.Properties["resolution"].Description, "没有覆盖需求时省略") {
		t.Fatalf("configured schema = %s", spec.InputSchema)
	}

	var merged map[string]any
	if err := json.Unmarshal(spec.ApplyDefaults(json.RawMessage(
		`{"resolution":"720p","references":["first.png"]}`)), &merged); err != nil {
		t.Fatal(err)
	}
	if merged["resolution"] != "720p" || merged["audio"] != true ||
		merged["duration"] != float64(5) {
		t.Fatalf("merged args = %#v", merged)
	}
}

func TestWithDefaultsRejectsInvalidConfiguration(t *testing.T) {
	cases := []map[string]any{
		{"unknown": "x"},
		{"resolution": "4k"},
		{"audio": "yes"},
		{"duration": float64(2)},
		{"references": []any{"a", "b", "c"}},
	}
	for _, defaults := range cases {
		if _, err := WithDefaults(defaultsSpec(), defaults); err == nil {
			t.Errorf("defaults %#v: expected error", defaults)
		}
	}
	if _, err := WithDefaults(defaultsSpec(), nil); err == nil {
		t.Fatal("nil defaults must be rejected")
	}
}

func TestApplyDefaultsLeavesMalformedArgumentsForToolValidation(t *testing.T) {
	spec, err := WithDefaults(defaultsSpec(), map[string]any{"resolution": "1080p"})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []json.RawMessage{json.RawMessage(`not-json`), json.RawMessage(`null`)} {
		if got := spec.ApplyDefaults(input); !strings.EqualFold(string(got), string(input)) {
			t.Fatalf("ApplyDefaults(%s) = %s", input, got)
		}
	}
}
