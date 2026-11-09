// Package discovery receives capability manifests announced through Temporal.
package discovery

import (
	sdkdiscovery "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/discovery"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/workflow"

	"agentk/internal/temporalx"
)

const (
	RegisterWorkflowType = sdkdiscovery.RegisterWorkflowType
	RegisterActivityType = "agentk.discovery.persist-manifest.v1"
)

func Register(w temporalx.Registry, activities *Activities) {
	w.RegisterWorkflowWithOptions(RegisterWorkflow,
		workflow.RegisterOptions{Name: RegisterWorkflowType})
	w.RegisterActivityWithOptions(activities.RegisterManifest,
		activity.RegisterOptions{Name: RegisterActivityType})
}
