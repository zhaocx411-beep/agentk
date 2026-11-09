package worker

import (
	"context"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
	sdkdiscovery "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/discovery"
	"go.temporal.io/sdk/activity"
)

func (w *Worker) registerDescribeActivity(manifest capability.ManifestV1) {
	w.RegisterActivityWithOptions(
		func(context.Context) (capability.ManifestV1, error) {
			return manifest.Normalize()
		},
		activity.RegisterOptions{Name: sdkdiscovery.DescribeActivityType},
	)
}
