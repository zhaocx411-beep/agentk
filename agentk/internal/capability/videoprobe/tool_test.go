package videoprobe

import (
	"context"
	"strings"
	"testing"

	"agentk/internal/tool"
)

func TestBuildToolDeclaration(t *testing.T) {
	spec := BuildTool()
	if spec.Name != ToolName {
		t.Fatalf("spec name = %q, want %q", spec.Name, ToolName)
	}
	binding, ok := spec.Binding.(tool.ActivityBinding)
	if !ok {
		t.Fatalf("Binding = %T, want ActivityBinding", spec.Binding)
	}
	if binding.ActivityName != ActivityName || binding.StartToCloseTimeout <= 0 {
		t.Fatalf("activity binding = %+v", binding)
	}
	if schema := string(spec.InputSchema); !strings.Contains(schema, `"videoUrl"`) {
		t.Fatalf("schema = %s", schema)
	}
	if _, err := spec.Run(context.Background(), nil, tool.Context{}); err == nil {
		t.Fatal("ActivityBinding must not be runnable in-process")
	}
}
