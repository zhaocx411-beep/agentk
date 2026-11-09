package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	sdktemporal "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/temporal"
	sdkinvocation "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/invocation"
	"go.temporal.io/sdk/client"
	temporalworker "go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"agentk/internal/agentloop"
	"agentk/internal/artifact"
	"agentk/internal/capability"
	"agentk/internal/discovery"
	"agentk/internal/invocation"
	"agentk/internal/pipeline"
)

const temporalTaskQueue = "agentk"

// temporalNamespace 与 SDK 的 KESS 拨号取值一致；本地直连不经 SDK，需自带一份。
const temporalNamespace = "agentk"

type temporalConfig struct {
	TaskQueue string
}

func dialTemporal(ctx context.Context) (client.Client, error) {
	// 没有 kess-agent 的开发机做不了 KESS 服务发现，设了地址就直连本机
	// `temporal server start-dev`；线上不设该变量，仍走 KESS。
	if address := strings.TrimSpace(os.Getenv("AGENTK_TEMPORAL_ADDRESS")); address != "" {
		c, err := client.DialContext(ctx, client.Options{
			HostPort:  address,
			Namespace: temporalNamespace,
			ContextPropagators: []workflow.ContextPropagator{
				sdkinvocation.MetaPropagator{},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("dial Temporal at %s: %w", address, err)
		}
		return c, nil
	}
	c, err := sdktemporal.Dial(ctx)
	if err != nil {
		return nil, fmt.Errorf("dial Temporal through KESS: %w", err)
	}
	return c, nil
}

// newTemporalWorker 是 standalone 和独立 Worker 进程共用的注册入口，统一注册
// Session loop、capability、Pipeline、invocation 和 discovery 执行体。
func newTemporalWorker(
	c client.Client,
	cfg temporalConfig,
	loopCfg agentloop.Config,
	modelClients capability.ModelClients,
	storage artifact.Storage,
	pipelineActivities *pipeline.Activities,
	invocationActivities *invocation.Activities,
	discoveryActivities *discovery.Activities,
) temporalworker.Worker {
	w := temporalworker.New(
		c,
		cfg.TaskQueue,
		temporalworker.Options{},
	)
	agentloop.Register(w, loopCfg)
	capability.RegisterAll(w, modelClients, storage)
	pipeline.Register(w, pipelineActivities)
	invocation.Register(w, invocationActivities)
	discovery.Register(w, discoveryActivities)
	return w
}
