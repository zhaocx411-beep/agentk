package grpc

import (
	"context"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	ksrpc "git.corp.kuaishou.com/klingai-agentk/ksresolve.git/go/rpc"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/protobuf/encoding/protowire"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/client"
	apiv1 "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1/apiv1connect"
)

type grpcService struct {
	apiv1connect.UnimplementedAgentKServiceHandler
	carriers chan<- string
}

func (s grpcService) WhoAmI(
	_ context.Context,
	request *connect.Request[apiv1.WhoAmIRequest],
) (*connect.Response[apiv1.WhoAmIResponse], error) {
	s.carriers <- request.Header().Get(carrierHeaderKey)
	return connect.NewResponse(&apiv1.WhoAmIResponse{}), nil
}

func (s grpcService) WatchEvents(
	_ context.Context,
	request *connect.Request[apiv1.WatchEventsRequest],
	stream *connect.ServerStream[apiv1.WatchEventsResponse],
) error {
	s.carriers <- request.Header().Get(carrierHeaderKey)
	for range 2 {
		if err := stream.Send(&apiv1.WatchEventsResponse{}); err != nil {
			return err
		}
	}
	return nil
}

func TestEndpointForwardsContextCarrier(t *testing.T) {
	// The direct endpoint must not color requests with the deployment lane.
	t.Setenv("KWS_SERVICE_STAGE", "PROD_REGRESSION_TEST")
	t.Setenv("KWS_LANE_ID", "deployment")
	carriers := make(chan string, 2)
	_, handler := apiv1connect.NewAgentKServiceHandler(grpcService{carriers: carriers})
	server := httptest.NewServer(h2c.NewHandler(handler, new(http2.Server)))
	defer server.Close()
	c, err := New(client.Options{Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	raw := protowire.AppendString(protowire.AppendTag(nil, 1, protowire.BytesType), "dev.child,other")
	raw = protowire.AppendVarint(protowire.AppendTag(raw, 99, protowire.VarintType), 7)
	colored, err := ksrpc.ContextWithCarrier(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	for name, ctx := range map[string]context.Context{"uncolored": t.Context(), "colored": colored} {
		t.Run(name, func(t *testing.T) {
			if _, err := c.WhoAmI(ctx, &apiv1.WhoAmIRequest{}); err != nil {
				t.Fatal(err)
			}
			stream, err := c.WatchEvents(ctx, &apiv1.WatchEventsRequest{})
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for stream.Receive() {
				count++
			}
			if stream.Err() != nil || count != 2 {
				t.Fatalf("count=%d err=%v", count, stream.Err())
			}
			stream.Close()
			for range 2 {
				got := <-carriers
				if name == "uncolored" {
					if got != "" {
						t.Fatalf("uncolored request sent carrier %q", got)
					}
					continue
				}
				decoded, err := connect.DecodeBinaryHeader(got)
				if err != nil || string(decoded) != string(raw) {
					t.Fatalf("carrier = %q (%x), err=%v", got, decoded, err)
				}
			}
		})
	}
}
