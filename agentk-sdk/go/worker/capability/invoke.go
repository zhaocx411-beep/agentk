package capability

import (
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/workflow"
)

const (
	invokeWorkflowType = "agentk.capability.invoke.v1"
	coreTaskQueue      = "agentk"
)

type invokeInput struct {
	CapabilityID string `json:"capabilityId"`
	Input        any    `json:"input"`
}

// Invoke asks AgentK Core to resolve and execute capabilityID, then waits for
// its result in output. Call Invoke only from Temporal Workflow code; output
// must be a pointer compatible with the capability result.
//
// Workflow targets run as child workflows and receive a cancellation request
// when their parent closes. Activity targets wait for cancellation to complete.
// Invoke returns errors from capability resolution and execution.
func Invoke(
	ctx workflow.Context,
	capabilityID string,
	input any,
	output any,
) error {
	childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		TaskQueue:         coreTaskQueue,
		ParentClosePolicy: enumspb.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
	})
	return workflow.ExecuteChildWorkflow(
		childCtx,
		invokeWorkflowType,
		invokeInput{CapabilityID: capabilityID, Input: input},
	).Get(childCtx, output)
}
