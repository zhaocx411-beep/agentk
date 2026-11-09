// Package grpc connects to AgentK over gRPC using KESS service discovery.
package grpc

import (
	"context"
	"net/http"

	"connectrpc.com/connect"
	ksrpc "git.corp.kuaishou.com/klingai-agentk/ksresolve.git/go/rpc"
	ksconnect "git.corp.kuaishou.com/klingai-agentk/ksresolve.git/go/rpc/connect"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/client"
)

const (
	serviceName      = "klingai-agentk-api"
	carrierHeaderKey = "trace_context-bin"
)

// New creates an AgentK Client that uses unencrypted HTTP/2 gRPC. It connects
// directly to options.Endpoint when set, or uses KESS service discovery
// otherwise. The KESS transport attaches the process's platform identity to
// every request and must be released with Client.Close. Both modes forward
// the request lane set with ksresolve's rpc.WithLane.
func New(options client.Options) (*client.Client, error) {
	if options.Endpoint != "" {
		protocols := new(http.Protocols)
		protocols.SetUnencryptedHTTP2(true)
		return client.New(
			&http.Client{Transport: &carrierTransport{&http.Transport{Protocols: protocols}}},
			options.Endpoint,
			connect.WithGRPC(),
		)
	}
	discovered, err := ksconnect.NewClient(context.Background(), serviceName)
	if err != nil {
		return nil, err
	}
	c, err := client.New(discovered, discovered.BaseURL(), connect.WithGRPC())
	if err != nil {
		_ = discovered.Close()
		return nil, err
	}
	return c, nil
}

// carrierTransport writes the context's trace carrier as a gRPC binary header
// for direct connections that bypass ksresolve.
type carrierTransport struct {
	inner http.RoundTripper
}

func (t *carrierTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	raw, ok, err := ksrpc.CarrierFromContext(request.Context())
	if err != nil {
		return nil, err
	}
	if !ok || request.Header.Get(carrierHeaderKey) != "" {
		return t.inner.RoundTrip(request)
	}
	clone := request.Clone(request.Context())
	clone.Header.Set(carrierHeaderKey, connect.EncodeBinaryHeader(raw))
	return t.inner.RoundTrip(clone)
}
