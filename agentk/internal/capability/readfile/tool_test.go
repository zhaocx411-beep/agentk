package readfile

import (
	"encoding/json"
	"testing"

	"agentk/internal/tool"
)

func TestBuildToolContract(t *testing.T) {
	spec := BuildTool()
	if spec.Name != ToolName {
		t.Fatalf("name = %q", spec.Name)
	}
	if spec.HistoryTrim == nil || spec.HistoryTrim.Field != "content" {
		t.Fatalf("history trim = %#v", spec.HistoryTrim)
	}
	binding, ok := spec.Binding.(tool.ActivityBinding)
	if !ok || binding.ActivityName != ActivityName {
		t.Fatalf("binding = %#v", spec.Binding)
	}
	var schema map[string]any
	if err := json.Unmarshal(spec.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	properties := schema["properties"].(map[string]any)
	for _, name := range []string{"fileUrl", "pages", "mode", "model", "sheet"} {
		if _, ok := properties[name]; !ok {
			t.Errorf("schema is missing %q", name)
		}
	}
	model := properties["model"].(map[string]any)
	enum := model["enum"].([]any)
	if len(enum) != 3 || enum[0] != "gemini-3.1-pro-preview" {
		t.Fatalf("model enum = %#v", enum)
	}
}
