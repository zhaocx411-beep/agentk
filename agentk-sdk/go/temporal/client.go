// Package temporal connects AgentK workers to their Temporal namespace.
package temporal

import (
	"context"
	"fmt"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/invocation"
	ksgrpc "git.corp.kuaishou.com/klingai-agentk/ksresolve.git/go/rpc/grpc"
	"go.opentelemetry.io/otel/propagation"
	"go.temporal.io/sdk/client"
	temporalotel "go.temporal.io/sdk/contrib/opentelemetry"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/workflow"
	"google.golang.org/grpc"
)

const (
	temporalKESSServiceName = "klingai-muse-temporal-server"
	temporalNamespace       = "agentk"
)

// Dial connects to the Temporal namespace used by AgentK through KESS service
// discovery. The client propagates AgentK invocation context to workflows and
// activities registered by the caller.
//
// The caller owns the returned client and must close it when it is no longer
// needed.
func Dial(ctx context.Context) (client.Client, error) {
	tracing, err := temporalotel.NewTracingInterceptor(
		temporalotel.TracerOptions{
			DisableBaggage:    true,
			TextMapPropagator: propagation.TraceContext{},
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"temporal: create OpenTelemetry interceptor: %w",
			err,
		)
	}

	resolver, err := ksgrpc.NewResolver()
	if err != nil {
		return nil, fmt.Errorf("temporal: create KESS resolver: %w", err)
	}
	target, err := resolver.Target(temporalKESSServiceName)
	if err != nil {
		return nil, fmt.Errorf("temporal: %w", err)
	}

	c, err := client.DialContext(ctx, client.Options{
		HostPort:  target,
		Namespace: temporalNamespace,
		ConnectionOptions: client.ConnectionOptions{
			DialOptions: []grpc.DialOption{grpc.WithResolvers(resolver)},
		},
		Interceptors: []interceptor.ClientInterceptor{
			tracing,
		},
		MetricsHandler: temporalotel.NewMetricsHandler(
			temporalotel.MetricsHandlerOptions{UseMonotonicCounters: true},
		),
		ContextPropagators: []workflow.ContextPropagator{
			invocation.MetaPropagator{},
			CarrierPropagator{},
		},
	})
	if err != nil {
		return nil, fmt.Errorf(
			"temporal: dial KESS service %q: %w",
			temporalKESSServiceName,
			err,
		)
	}
	return c, nil
}
