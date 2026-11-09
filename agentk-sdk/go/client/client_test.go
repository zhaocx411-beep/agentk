package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"connectrpc.com/connect"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	apiv1 "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1/apiv1connect"
)

type testService struct {
	apiv1connect.UnimplementedAgentKServiceHandler
	traceparent   chan<- string
	watchRequests chan<- *apiv1.WatchEventsRequest
}

// The Client attaches no caller identity or lane of its own; that is the
// transport's job.
func requireNoIdentity(header http.Header) error {
	if header.Get("kws-bin") != "" || header.Get("kwsCredential") != "" {
		return errors.New("unexpected KWS identity header")
	}
	if header.Get("Authorization") != "" {
		return errors.New("unexpected authorization header")
	}
	if header.Get("trace_context-bin") != "" {
		return errors.New("unexpected lane header")
	}
	return nil
}

func (s testService) WhoAmI(
	_ context.Context,
	request *connect.Request[apiv1.WhoAmIRequest],
) (*connect.Response[apiv1.WhoAmIResponse], error) {
	if err := requireNoIdentity(request.Header()); err != nil {
		return nil, connect.NewError(
			connect.CodeUnauthenticated,
			err,
		)
	}
	if s.traceparent != nil {
		s.traceparent <- request.Header().Get("traceparent")
	}
	return connect.NewResponse(&apiv1.WhoAmIResponse{Id: "user-1"}), nil
}

func TestClientDoesNotColorRequestsWithDeploymentLane(t *testing.T) {
	t.Setenv("KWS_LANE_ID", "deployment")
	t.Setenv("KWS_SERVICE_STAGE", "PROD_REGRESSION_TEST")

	path, handler := apiv1connect.NewAgentKServiceHandler(testService{})
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	server := httptest.NewUnstartedServer(mux)
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)

	for _, protocol := range []string{"connect", "grpc"} {
		t.Run(protocol, func(t *testing.T) {
			var options []connect.ClientOption
			if protocol == "grpc" {
				options = append(options, connect.WithGRPC())
			}
			c, err := New(server.Client(), server.URL, options...)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.WhoAmI(t.Context(), &apiv1.WhoAmIRequest{}); err != nil {
				t.Fatal(err)
			}
			stream, err := c.WatchEvents(t.Context(), &apiv1.WatchEventsRequest{})
			if err != nil {
				t.Fatal(err)
			}
			for stream.Receive() {
			}
			if err := stream.Err(); err != nil {
				t.Fatal(err)
			}
			if err := stream.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func (s testService) WatchEvents(
	_ context.Context,
	request *connect.Request[apiv1.WatchEventsRequest],
	stream *connect.ServerStream[apiv1.WatchEventsResponse],
) error {
	if err := requireNoIdentity(request.Header()); err != nil {
		return connect.NewError(
			connect.CodeUnauthenticated,
			err,
		)
	}
	if s.watchRequests != nil {
		s.watchRequests <- request.Msg
	}
	for _, text := range []string{"hello", "world"} {
		frame := &apiv1.WatchEventsResponse{Frame: &apiv1.WatchEventsResponse_Delta{Delta: &apiv1.Delta{ExecutionId: "execution", Text: text}}}
		if err := stream.Send(frame); err != nil {
			return err
		}
	}
	return nil
}

func TestClientUnaryAndStream(t *testing.T) {

	requests := make(chan *apiv1.WatchEventsRequest, 1)
	path, handler := apiv1connect.NewAgentKServiceHandler(testService{watchRequests: requests})
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	c, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	identity, err := c.WhoAmI(t.Context(), &apiv1.WhoAmIRequest{})
	if err != nil {
		t.Fatalf("WhoAmI() error = %v", err)
	}
	if identity.GetId() != "user-1" {
		t.Fatalf("WhoAmI().Id = %q, want user-1", identity.GetId())
	}

	stream, err := c.WatchEvents(t.Context(), &apiv1.WatchEventsRequest{SessionId: "session", ExecutionId: "execution"})
	if err != nil {
		t.Fatalf("WatchEvents() error = %v", err)
	}
	var received []string
	for stream.Receive() {
		if stream.Msg() == nil {
			t.Fatal("EventStream.Msg() returned nil")
		}
		if stream.Msg().GetDelta().GetExecutionId() != "execution" {
			t.Fatalf("frame = %v", stream.Msg())
		}
		received = append(received, stream.Msg().GetDelta().GetText())
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("EventStream.Err() = %v", err)
	}
	if !slices.Equal(received, []string{"hello", "world"}) {
		t.Fatalf("frames = %v", received)
	}
	gotRequest := <-requests
	if gotRequest.GetSessionId() != "session" || gotRequest.GetExecutionId() != "execution" {
		t.Fatalf("request = %v", gotRequest)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("EventStream.Close() error = %v", err)
	}
}

func TestClientPropagatesTraceContext(t *testing.T) {

	receivedTraceparent := make(chan string, 1)
	path, handler := apiv1connect.NewAgentKServiceHandler(testService{
		traceparent: receivedTraceparent,
	})
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	c, err := New(server.Client(), server.URL)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	traceID, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	if err != nil {
		t.Fatalf("TraceIDFromHex() error = %v", err)
	}
	spanID, err := trace.SpanIDFromHex("0102030405060708")
	if err != nil {
		t.Fatalf("SpanIDFromHex() error = %v", err)
	}
	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(t.Context(), parent)

	if _, err := c.WhoAmI(ctx, &apiv1.WhoAmIRequest{}); err != nil {
		t.Fatalf("WhoAmI() error = %v", err)
	}

	header := make(http.Header)
	header.Set("traceparent", <-receivedTraceparent)
	extracted := propagation.TraceContext{}.Extract(
		context.Background(),
		propagation.HeaderCarrier(header),
	)
	got := trace.SpanContextFromContext(extracted)
	if !got.IsValid() {
		t.Fatal("traceparent contains no valid span context")
	}
	if got.TraceID() != traceID {
		t.Fatalf("trace ID = %s, want %s", got.TraceID(), traceID)
	}
}

func TestAllUnaryWrappers(t *testing.T) {
	testUnary(t, "CreateSession",
		&apiv1.CreateSessionRequest{Title: "new session", Profile: &apiv1.Profile{DefaultModel: "test-model"}},
		&apiv1.CreateSessionResponse{Session: &apiv1.Session{Id: "created", Title: "new session"}},
		(*Client).CreateSession,
	)
	testUnary(t, "ListSessions",
		&apiv1.ListSessionsRequest{PageSize: 7, PageToken: "sessions-before"},
		&apiv1.ListSessionsResponse{Sessions: []*apiv1.Session{{Id: "listed"}}, NextPageToken: "sessions-after"},
		(*Client).ListSessions,
	)
	testUnary(t, "StartExecution",
		&apiv1.StartExecutionRequest{SessionId: "start-session", Input: &apiv1.StartExecutionRequest_Message{Message: &apiv1.AgentMessageExecutionInput{Text: "hello"}}},
		&apiv1.StartExecutionResponse{Execution: &apiv1.Execution{Id: "started", SessionId: "start-session"}},
		(*Client).StartExecution,
	)
	testUnary(t, "CancelExecution",
		&apiv1.CancelExecutionRequest{SessionId: "cancel-session"},
		&apiv1.CancelExecutionResponse{},
		(*Client).CancelExecution,
	)
	testUnary(t, "ListEvents",
		&apiv1.ListEventsRequest{SessionId: "events-session", PageSize: 8, PageToken: "events-before"},
		&apiv1.ListEventsResponse{Events: []*apiv1.Event{{EventId: "event"}}, NextPageToken: "events-after"},
		(*Client).ListEvents,
	)
	testUnary(t, "ListPipelines",
		&apiv1.ListPipelinesRequest{SessionId: "pipelines-session", PageSize: 9, PageToken: "pipelines-before"},
		&apiv1.ListPipelinesResponse{Pipelines: []*apiv1.Pipeline{{Name: "render"}}, NextPageToken: "pipelines-after"},
		(*Client).ListPipelines,
	)
	testUnary(t, "ListExecutions",
		&apiv1.ListExecutionsRequest{SessionId: "executions-session", PageSize: 10, PageToken: "executions-before"},
		&apiv1.ListExecutionsResponse{Executions: []*apiv1.Execution{{Id: "listed-execution"}}, NextPageToken: "executions-after"},
		(*Client).ListExecutions,
	)
	testUnary(t, "GetExecution",
		&apiv1.GetExecutionRequest{SessionId: "get-session", ExecutionId: "get-execution"},
		&apiv1.GetExecutionResponse{Execution: &apiv1.Execution{Id: "get-execution", SessionId: "get-session"}},
		(*Client).GetExecution,
	)
	testUnary(t, "ListCapabilities",
		&apiv1.ListCapabilitiesRequest{PageSize: 11, PageToken: "capabilities-before", IncludeVariants: true},
		&apiv1.ListCapabilitiesResponse{Capabilities: []*apiv1.Capability{{Id: "contrib.test.render"}}, NextPageToken: "capabilities-after"},
		(*Client).ListCapabilities,
	)
	testUnary(t, "InvokeCapability",
		&apiv1.InvokeCapabilityRequest{CapabilityId: "contrib.test.render", Input: &structpb.Struct{Fields: map[string]*structpb.Value{"prompt": structpb.NewStringValue("draw")}}},
		&apiv1.InvokeCapabilityResponse{Output: structpb.NewStringValue("rendered")},
		(*Client).InvokeCapability,
	)
	testUnary(t, "WhoAmI",
		&apiv1.WhoAmIRequest{},
		&apiv1.WhoAmIResponse{Id: "caller", AppId: "app", DisplayName: "Caller"},
		(*Client).WhoAmI,
	)
}

func testUnary[Q, R any](t *testing.T, method string, request *Q, response *R, call func(*Client, context.Context, *Q) (*R, error)) {
	t.Helper()
	t.Run(method, func(t *testing.T) {
		received := make(chan *Q, 1)
		path := "/agentk.api.v1.AgentKService/" + method
		mux := http.NewServeMux()
		mux.Handle(path, connect.NewUnaryHandler(path, func(_ context.Context, req *connect.Request[Q]) (*connect.Response[R], error) {
			received <- req.Msg
			return connect.NewResponse(response), nil
		}))
		server := httptest.NewServer(mux)
		defer server.Close()
		c, err := New(server.Client(), server.URL)
		if err != nil {
			t.Fatal(err)
		}
		got, err := call(c, t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(any(got).(proto.Message), any(response).(proto.Message)) {
			t.Fatalf("response = %v, want %v", got, response)
		}
		select {
		case gotRequest := <-received:
			if !proto.Equal(any(gotRequest).(proto.Message), any(request).(proto.Message)) {
				t.Fatalf("request = %v, want %v", gotRequest, request)
			}
		default:
			t.Fatal("server did not receive the request")
		}
	})
}

func TestNewValidatesDependencies(t *testing.T) {
	t.Parallel()

	if _, err := New(nil, "http://agentk.test"); err == nil {
		t.Fatal("New(nil) error = nil")
	}
	if _, err := New(http.DefaultClient, "not-an-endpoint"); err == nil {
		t.Fatal("New(invalid endpoint) error = nil")
	}
}
