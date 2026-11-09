package capworker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	agentktemporal "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/temporal"
	agentkworker "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker"
	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
	"git.corp.kuaishou.com/klingai-agentk/contrib/maas-worker.git/internal/config"
	"go.temporal.io/sdk/activity"
	temporalclient "go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	temporalworker "go.temporal.io/sdk/worker"
)

type gatewayInferer interface {
	Infer(context.Context, string, string, map[string]any) (map[string]any, error)
}

type definition struct {
	route           string
	adapter         string
	artifactOutputs map[string]string
	temporalType    string
	capability      sdkcapability.Capability
}

// Runtime 持有 AgentK Temporal client 与 capability worker 的生命周期。
type Runtime struct {
	client temporalclient.Client
	worker *agentkworker.Worker
	once   sync.Once
}

func Start(ctx context.Context, cfg config.WorkerConfig, logger *slog.Logger) (*Runtime, error) {
	if logger == nil {
		logger = slog.Default()
	}

	gateway, err := NewGatewayClient(cfg.GatewayBaseURL, cfg.GatewayToken, nil)
	if err != nil {
		return nil, err
	}
	temporalClient, err := agentktemporal.Dial(ctx)
	if err != nil {
		return nil, fmt.Errorf("connect AgentK Temporal: %w", err)
	}

	w := agentkworker.New(temporalClient, cfg.TaskQueue, temporalworker.Options{})
	definitions := definitionsForRoutes(cfg.Capabilities)
	artifactURIs := newArtifactURIResolver()
	for _, item := range definitions {
		handler := &inferActivity{
			route: item.route, adapter: item.adapter, gateway: gateway,
			artifactURIs: artifactURIs, artifactOutputs: item.artifactOutputs,
		}
		w.RegisterActivityWithOptions(
			handler.Run, activity.RegisterOptions{Name: item.temporalType})
		w.PublishActivity(item.capability)
	}
	if err := w.Start(); err != nil {
		temporalClient.Close()
		return nil, fmt.Errorf("start AgentK capability worker: %w", err)
	}
	logger.Info("AgentK capability worker started",
		"task_queue", cfg.TaskQueue,
		"gateway_base_url", cfg.GatewayBaseURL.Redacted(),
		"capabilities", len(definitions))
	return &Runtime{client: temporalClient, worker: w}, nil
}

func (r *Runtime) Close() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		r.worker.Stop()
		r.client.Close()
	})
}

type inferActivity struct {
	route           string
	adapter         string
	gateway         gatewayInferer
	artifactURIs    artifactURIResolver
	artifactOutputs map[string]string
}

func (a *inferActivity) Run(ctx context.Context, features map[string]any) (map[string]any, error) {
	requestID := activityRequestID(ctx)
	var output map[string]any
	var err error
	switch a.adapter {
	case "process_video":
		output, err = a.processVideo(ctx, requestID, features)
	case "compose_video":
		output, err = a.composeVideo(ctx, requestID, features)
	case "extract_audio":
		output, err = a.extractAudio(ctx, requestID, features)
	default:
		output, err = a.gateway.Infer(ctx, a.route, requestID, features)
	}
	if err == nil {
		return a.attachManagedArtifacts(output)
	}
	var callError *CallError
	if errors.As(err, &callError) && callError.Permanent {
		return nil, temporal.NewNonRetryableApplicationError(
			err.Error(), "UpstreamRequestRejected", err)
	}
	return nil, err
}

func activityRequestID(ctx context.Context) string {
	identity := "non-temporal"
	if activity.IsActivity(ctx) {
		info := activity.GetInfo(ctx)
		identity = info.WorkflowExecution.ID + "/" +
			info.WorkflowExecution.RunID + "/" + info.ActivityID
	}
	digest := sha256.Sum256([]byte(identity))
	return "agentk_" + hex.EncodeToString(digest[:16])
}

func definitionsForRoutes(routes map[string]config.CapabilityRoute) []definition {
	names := make([]string, 0, len(routes))
	for name := range routes {
		names = append(names, name)
	}
	sort.Strings(names)
	definitions := make([]definition, 0, len(names))
	for _, name := range names {
		configured := routes[name]
		gatewayRoute := configured.GatewayRoute
		if gatewayRoute == "" {
			gatewayRoute = name
		}
		metadata := configured.AgentK
		temporalType := config.AgentKActivityType(name)
		capability := sdkcapability.Capability{
			ID:             metadata.ID,
			TemporalType:   temporalType,
			Description:    metadata.Description,
			InputSchema:    append(json.RawMessage(nil), metadata.InputSchema...),
			OutputSchema:   append(json.RawMessage(nil), metadata.OutputSchema...),
			TimeoutSeconds: int64(configured.TimeoutSeconds),
		}
		if metadata.ToolDescription != "" {
			maxAttempts := metadata.MaxAttempts
			if maxAttempts == 0 {
				// PRT Infer 没有通用去重保证；默认不重试有歧义的已发出请求。
				maxAttempts = 1
			}
			capability.Tool = &sdkcapability.Tool{
				Description: metadata.ToolDescription,
				MaxAttempts: maxAttempts,
			}
		}
		definitions = append(definitions, definition{
			route: gatewayRoute, adapter: configured.Adapter,
			artifactOutputs: cloneStringMap(configured.ArtifactOutputs),
			temporalType:    temporalType, capability: capability,
		})
	}
	return definitions
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
