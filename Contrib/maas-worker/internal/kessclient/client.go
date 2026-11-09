package kessclient

import (
	"context"
	"fmt"
	"sort"

	"git.corp.kuaishou.com/infra/infra-framework-go.git/kess"
	inferpb "git.corp.kuaishou.com/klingai-agentk/contrib/maas-worker.git/gen/infer"
	"git.corp.kuaishou.com/klingai-agentk/contrib/maas-worker.git/internal/config"
)

type clientEntry struct {
	connection kess.ClientConnSelector
	client     inferpb.LocalInferServiceClient
}

type Pool struct {
	clients map[string]clientEntry
}

func New(routes map[string]config.Route) (*Pool, error) {
	services := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		services[route.ServiceName] = struct{}{}
	}
	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	sort.Strings(names)

	pool := &Pool{clients: make(map[string]clientEntry, len(names))}
	for _, serviceName := range names {
		connection, err := kess.NewRPCClient(serviceName)
		if err != nil {
			pool.Close()
			return nil, fmt.Errorf("create KESS client for %q: %w", serviceName, err)
		}
		pool.clients[serviceName] = clientEntry{
			connection: connection,
			client:     inferpb.NewLocalInferServiceClient(connection),
		}
	}
	return pool, nil
}

func (p *Pool) Infer(ctx context.Context, serviceName, lane string, request *inferpb.InferRequest) (*inferpb.InferResponse, error) {
	entry, ok := p.clients[serviceName]
	if !ok {
		return nil, fmt.Errorf("KESS service %q is not configured", serviceName)
	}
	if lane != "" {
		ctx = kess.AddLaneID(ctx, lane)
	}
	return entry.client.Infer(ctx, request)
}

func (p *Pool) Close() {
	if p == nil {
		return
	}
	for _, entry := range p.clients {
		entry.connection.Close()
	}
}
