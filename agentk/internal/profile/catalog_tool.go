package profile

import (
	"encoding/json"
	"fmt"
	"time"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"

	"agentk/internal/capcatalog"
	"agentk/internal/tool"
)

func specFromResolved(entry ToolEntry, resolved capcatalog.ResolvedTool) (tool.Spec, error) {
	timeout := time.Duration(resolved.Capability.TimeoutSeconds) * time.Second
	spec := tool.Spec{
		Name:        entry.Name,
		Description: resolved.Tool.Description,
		InputSchema: append(json.RawMessage(nil), resolved.Capability.InputSchema...),
		Timeout:     timeout,
	}
	spec = withInstructions(spec, entry.Instructions)

	switch resolved.Capability.Kind {
	case sdkcapability.CapabilityKindActivity:
		if resolved.Tool.Mode != sdkcapability.ToolModeBlocking {
			return tool.Spec{}, fmt.Errorf(
				"activity tool capability %q has unsupported mode %q",
				entry.Capability, resolved.Tool.Mode)
		}
		spec.Binding = tool.ActivityBinding{
			ActivityName:        resolved.Capability.TemporalType,
			TaskQueue:           resolved.TaskQueue,
			StartToCloseTimeout: timeout,
			MaxAttempts:         int(resolved.Tool.MaxAttempts),
		}
	case sdkcapability.CapabilityKindWorkflow:
		if resolved.Tool.Mode != sdkcapability.ToolModeBlocking {
			return tool.Spec{}, fmt.Errorf(
				"workflow tool capability %q has unsupported mode %q",
				entry.Capability, resolved.Tool.Mode)
		}
		spec.Binding = tool.WorkflowBinding{
			WorkflowType:     resolved.Capability.TemporalType,
			TaskQueue:        resolved.TaskQueue,
			ExecutionTimeout: timeout,
		}
	default:
		return tool.Spec{}, fmt.Errorf(
			"tool capability %q has unsupported kind %q",
			entry.Capability, resolved.Capability.Kind)
	}
	return spec, nil
}
