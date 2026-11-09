// Package client provides the AgentK client API. Transport packages such as
// client/grpc and client/http create Clients for the platform's endpoints; New
// accepts a caller-supplied HTTP client for other transports.
package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"

	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"
	"go.opentelemetry.io/otel/propagation"

	apiv1 "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1/apiv1connect"
)

// Options configures an AgentK client created by a transport package.
type Options struct {
	// Endpoint overrides the transport's default AgentK endpoint. It must be an
	// absolute URL. When empty, each transport uses its environment default.
	Endpoint string
}

// Client provides transport-independent access to the AgentK API. Transport
// constructors configure the connection and any caller identity it carries.
// Methods return transport and server errors unchanged.
type Client struct {
	rpc    apiv1connect.AgentKServiceClient
	closer io.Closer
}

// New creates a Client that sends requests to the AgentK service at baseURL
// through httpClient. The caller is responsible for any identity httpClient
// attaches. When httpClient also implements io.Closer, Close releases it.
func New(
	httpClient connect.HTTPClient,
	baseURL string,
	options ...connect.ClientOption,
) (*Client, error) {
	if httpClient == nil {
		return nil, errors.New("agentk client: HTTP client is required")
	}
	endpoint, err := url.Parse(baseURL)
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return nil, fmt.Errorf("agentk client: invalid endpoint %q", baseURL)
	}
	tracing, err := otelconnect.NewInterceptor(
		otelconnect.WithPropagator(propagation.TraceContext{}),
		otelconnect.WithoutTraceEvents(),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"agentk client: create OpenTelemetry interceptor: %w",
			err,
		)
	}

	options = append(
		[]connect.ClientOption{connect.WithInterceptors(tracing)},
		options...,
	)
	closer, _ := httpClient.(io.Closer)
	return &Client{
		rpc:    apiv1connect.NewAgentKServiceClient(httpClient, baseURL, options...),
		closer: closer,
	}, nil
}

// Close releases transport resources such as service discovery. It is a no-op
// for transports that own none.
func (c *Client) Close() error {
	if c.closer == nil {
		return nil
	}
	return c.closer.Close()
}

// CreateSession creates a Session from the complete Profile in request. The
// Profile is fixed for the lifetime of the Session.
func (c *Client) CreateSession(
	ctx context.Context,
	request *apiv1.CreateSessionRequest,
) (*apiv1.CreateSessionResponse, error) {
	response, err := c.rpc.CreateSession(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, err
	}
	return response.Msg, nil
}

// ListSessions returns a page of Sessions, ordered from newest to oldest.
func (c *Client) ListSessions(
	ctx context.Context,
	request *apiv1.ListSessionsRequest,
) (*apiv1.ListSessionsResponse, error) {
	response, err := c.rpc.ListSessions(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, err
	}
	return response.Msg, nil
}

// StartExecution starts an Execution in an existing Session using the input in
// request and returns the new Execution.
func (c *Client) StartExecution(
	ctx context.Context,
	request *apiv1.StartExecutionRequest,
) (*apiv1.StartExecutionResponse, error) {
	response, err := c.rpc.StartExecution(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, err
	}
	return response.Msg, nil
}

// CancelExecution requests cancellation of the running Execution in a Session.
func (c *Client) CancelExecution(
	ctx context.Context,
	request *apiv1.CancelExecutionRequest,
) (*apiv1.CancelExecutionResponse, error) {
	response, err := c.rpc.CancelExecution(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, err
	}
	return response.Msg, nil
}

// ListEvents returns a page of persisted Session events, ordered from newest to
// oldest.
func (c *Client) ListEvents(
	ctx context.Context,
	request *apiv1.ListEventsRequest,
) (*apiv1.ListEventsResponse, error) {
	response, err := c.rpc.ListEvents(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, err
	}
	return response.Msg, nil
}

// WatchEvents opens a live stream for the Session and Execution named in
// request. The caller must receive from and close the returned stream.
func (c *Client) WatchEvents(
	ctx context.Context,
	request *apiv1.WatchEventsRequest,
) (*EventStream, error) {
	stream, err := c.rpc.WatchEvents(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, err
	}
	return newEventStream(stream), nil
}

// ListPipelines returns a page of Pipelines available to a Session, ordered by
// name.
func (c *Client) ListPipelines(
	ctx context.Context,
	request *apiv1.ListPipelinesRequest,
) (*apiv1.ListPipelinesResponse, error) {
	response, err := c.rpc.ListPipelines(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, err
	}
	return response.Msg, nil
}

// ListExecutions returns a page of Executions for a Session, ordered from newest
// to oldest.
func (c *Client) ListExecutions(
	ctx context.Context,
	request *apiv1.ListExecutionsRequest,
) (*apiv1.ListExecutionsResponse, error) {
	response, err := c.rpc.ListExecutions(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, err
	}
	return response.Msg, nil
}

// GetExecution returns one Execution identified by its Session and Execution
// IDs.
func (c *Client) GetExecution(
	ctx context.Context,
	request *apiv1.GetExecutionRequest,
) (*apiv1.GetExecutionResponse, error) {
	response, err := c.rpc.GetExecution(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, err
	}
	return response.Msg, nil
}

// ListCapabilities returns a page of registered capabilities, ordered by
// capability ID.
func (c *Client) ListCapabilities(
	ctx context.Context,
	request *apiv1.ListCapabilitiesRequest,
) (*apiv1.ListCapabilitiesResponse, error) {
	response, err := c.rpc.ListCapabilities(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, err
	}
	return response.Msg, nil
}

// InvokeCapability invokes a registered capability without creating a Session
// or Execution and waits for its result.
func (c *Client) InvokeCapability(
	ctx context.Context,
	request *apiv1.InvokeCapabilityRequest,
) (*apiv1.InvokeCapabilityResponse, error) {
	response, err := c.rpc.InvokeCapability(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, err
	}
	return response.Msg, nil
}

// WhoAmI returns the service and App identity resolved by AgentK.
func (c *Client) WhoAmI(
	ctx context.Context,
	request *apiv1.WhoAmIRequest,
) (*apiv1.WhoAmIResponse, error) {
	response, err := c.rpc.WhoAmI(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, err
	}
	return response.Msg, nil
}
