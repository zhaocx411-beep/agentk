package profile

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"

	corecapability "agentk/internal/capability"
	"agentk/internal/capcatalog"
	"agentk/internal/tool"
)

func resolvedTool(kind sdkcapability.CapabilityKind, mode sdkcapability.ToolMode) capcatalog.ResolvedTool {
	return capcatalog.ResolvedTool{
		ResolvedCapability: capcatalog.ResolvedCapability{
			TaskQueue: "external-worker",
			Capability: sdkcapability.CapabilityDef{
				ID:             "contrib.test.generate_video",
				Kind:           kind,
				TemporalType:   "contrib.generate_video.v1",
				Description:    "生成视频",
				InputSchema:    json.RawMessage(`{"type":"object","properties":{"topic":{"type":"string"}}}`),
				OutputSchema:   json.RawMessage(`{"type":"object"}`),
				TimeoutSeconds: 300,
			},
		},
		Tool: sdkcapability.ToolDecl{
			Description: "调用外部视频生成能力",
			Mode:        mode, MaxAttempts: 2,
		},
	}
}

func TestSpecFromResolved(t *testing.T) {
	entry := ToolEntry{
		Capability: "contrib.test.generate_video", Name: "make_video",
		Instructions: "仅在用户提供素材后调用",
	}
	tests := []struct {
		name string
		kind sdkcapability.CapabilityKind
		mode sdkcapability.ToolMode
		want tool.BindingKind
	}{
		{"blocking activity", sdkcapability.CapabilityKindActivity, sdkcapability.ToolModeBlocking, tool.BindActivity},
		{"blocking workflow", sdkcapability.CapabilityKindWorkflow, sdkcapability.ToolModeBlocking, tool.BindWorkflow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := specFromResolved(entry, resolvedTool(tt.kind, tt.mode))
			if err != nil {
				t.Fatal(err)
			}
			if spec.Name != "make_video" || spec.BindingKind() != tt.want ||
				spec.Timeout != 5*time.Minute ||
				!strings.Contains(spec.Description, entry.Instructions) {
				t.Fatalf("spec = %+v", spec)
			}
			switch binding := spec.Binding.(type) {
			case tool.ActivityBinding:
				if binding.ActivityName != "contrib.generate_video.v1" ||
					binding.TaskQueue != "external-worker" ||
					binding.StartToCloseTimeout != 5*time.Minute ||
					binding.MaxAttempts != 2 {
					t.Fatalf("activity binding = %+v", binding)
				}
			case tool.WorkflowBinding:
				if binding.WorkflowType != "contrib.generate_video.v1" ||
					binding.TaskQueue != "external-worker" ||
					binding.ExecutionTimeout != 5*time.Minute {
					t.Fatalf("workflow binding = %+v", binding)
				}
			}
		})
	}
}

func TestSpecFromResolvedRejectsUnsupportedCombination(t *testing.T) {
	if _, err := specFromResolved(ToolEntry{Capability: "contrib.x", Name: "x"},
		resolvedTool(sdkcapability.CapabilityKindActivity, sdkcapability.ToolModeBackground)); err == nil {
		t.Fatal("background activity must be rejected")
	}
	if _, err := specFromResolved(ToolEntry{Capability: "contrib.x", Name: "x"},
		resolvedTool(sdkcapability.CapabilityKindWorkflow, sdkcapability.ToolModeBackground)); err == nil {
		t.Fatal("background workflow must be rejected")
	}
	if _, err := specFromResolved(ToolEntry{Capability: "contrib.x", Name: "x"},
		resolvedTool(sdkcapability.CapabilityKind("other"), sdkcapability.ToolModeBlocking)); err == nil {
		t.Fatal("unknown capability kind must be rejected")
	}
}

func TestCoreToolCapabilityPreservesActivityPolicy(t *testing.T) {
	manifest, err := corecapability.CoreManifest()
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := capcatalog.NewComposite(manifest, capcatalog.NewMemory())
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := catalog.ResolveTool(
		t.Context(), "agentk.compose_video")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := specFromResolved(ToolEntry{
		Capability: "agentk.compose_video", Name: "compose_video",
	}, resolved)
	if err != nil {
		t.Fatal(err)
	}
	binding, ok := spec.Binding.(tool.ActivityBinding)
	if !ok || binding.TaskQueue != "agentk" ||
		binding.ActivityName != "agentk.capability.compose_video" ||
		binding.MaxAttempts != 2 {
		t.Fatalf("binding = %+v", spec.Binding)
	}
}
