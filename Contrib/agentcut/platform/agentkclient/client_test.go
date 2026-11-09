package agentkclient

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"connectrpc.com/connect"
	"git.corp.kuaishou.com/infra/infra-framework-go.git/utils/kws/kws_pb"
	sdkclient "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/client"
	agentkhttp "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/client/http"
	apiv1 "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1/apiv1connect"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/requestlane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const (
	kwsServiceNameEnv       = "KWS_SERVICE_NAME"
	kwsServiceCredentialEnv = "KWS_SERVICE_CREDENTIAL"
	kwsBinaryHeader         = "kws-bin"
	kwsCredentialHeader     = "kwsCredential"
)

type recordingAgentK struct {
	apiv1connect.UnimplementedAgentKServiceHandler

	mu      sync.Mutex
	headers map[string]http.Header
}

func (s *recordingAgentK) record(operation string, header http.Header) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.headers[operation] = header.Clone()
}

func (s *recordingAgentK) WhoAmI(
	_ context.Context,
	req *connect.Request[apiv1.WhoAmIRequest],
) (*connect.Response[apiv1.WhoAmIResponse], error) {
	s.record("WhoAmI", req.Header())
	return connect.NewResponse(&apiv1.WhoAmIResponse{
		Id:          "service-id",
		AppId:       "app-agentcut",
		DisplayName: "AgentCut",
	}), nil
}

func (s *recordingAgentK) CreateSession(
	_ context.Context,
	req *connect.Request[apiv1.CreateSessionRequest],
) (*connect.Response[apiv1.CreateSessionResponse], error) {
	s.record("CreateSession", req.Header())
	return connect.NewResponse(&apiv1.CreateSessionResponse{
		Session: &apiv1.Session{Id: "ses_test"},
	}), nil
}

func (s *recordingAgentK) StartExecution(
	_ context.Context,
	req *connect.Request[apiv1.StartExecutionRequest],
) (*connect.Response[apiv1.StartExecutionResponse], error) {
	s.record("StartExecution", req.Header())
	return connect.NewResponse(&apiv1.StartExecutionResponse{
		Execution: &apiv1.Execution{Id: "exe_test", SessionId: req.Msg.GetSessionId()},
	}), nil
}

func (s *recordingAgentK) ListEvents(
	_ context.Context,
	req *connect.Request[apiv1.ListEventsRequest],
) (*connect.Response[apiv1.ListEventsResponse], error) {
	s.record("ListEvents", req.Header())
	return connect.NewResponse(&apiv1.ListEventsResponse{}), nil
}

func (s *recordingAgentK) ListExecutions(
	_ context.Context,
	req *connect.Request[apiv1.ListExecutionsRequest],
) (*connect.Response[apiv1.ListExecutionsResponse], error) {
	s.record("ListExecutions", req.Header())
	return connect.NewResponse(&apiv1.ListExecutionsResponse{}), nil
}

func (s *recordingAgentK) WatchEvents(
	_ context.Context,
	req *connect.Request[apiv1.WatchEventsRequest],
	stream *connect.ServerStream[apiv1.WatchEventsResponse],
) error {
	s.record("WatchEvents", req.Header())
	return stream.Send(&apiv1.WatchEventsResponse{
		Frame: &apiv1.WatchEventsResponse_StreamEnd{
			StreamEnd: &apiv1.StreamEnd{ExecutionId: req.Msg.GetExecutionId()},
		},
	})
}

func (s *recordingAgentK) CancelExecution(
	_ context.Context,
	req *connect.Request[apiv1.CancelExecutionRequest],
) (*connect.Response[apiv1.CancelExecutionResponse], error) {
	s.record("CancelExecution", req.Header())
	return connect.NewResponse(&apiv1.CancelExecutionResponse{}), nil
}

func newRecordingServer(t *testing.T) (*recordingAgentK, *httptest.Server) {
	t.Helper()
	service := &recordingAgentK{headers: make(map[string]http.Header)}
	path, handler := apiv1connect.NewAgentKServiceHandler(service)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return service, server
}

func TestClientAddsKESSIdentityToUnaryAndStreamRequests(t *testing.T) {
	t.Setenv(kwsServiceNameEnv, "agentcut-test")
	t.Setenv(kwsServiceCredentialEnv, "credential-test")
	service, server := newRecordingServer(t)

	client, err := New(server.URL, "legacy-api-key-must-not-be-used")
	require.NoError(t, err)
	identity, err := client.ValidateAuth(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "app-agentcut", identity.AppID)
	assert.Equal(t, "AgentCut", identity.DisplayName)

	_, err = client.CreateSession(t.Context(), "test", &apiv1.Profile{})
	require.NoError(t, err)
	_, err = client.StartMessage(t.Context(), "ses_test", "hello")
	require.NoError(t, err)
	_, err = client.ListEvents(t.Context(), "ses_test", 10, "")
	require.NoError(t, err)
	_, err = client.LatestExecution(t.Context(), "ses_test")
	require.NoError(t, err)
	stream, err := client.WatchEvents(t.Context(), "ses_test", "exe_test")
	require.NoError(t, err)
	require.True(t, stream.Receive())
	require.NoError(t, stream.Err())
	require.NoError(t, stream.Close())
	require.NoError(t, client.CancelExecution(t.Context(), "ses_test"))

	for _, operation := range []string{
		"WhoAmI",
		"CreateSession",
		"StartExecution",
		"ListEvents",
		"ListExecutions",
		"WatchEvents",
		"CancelExecution",
	} {
		header := service.headers[operation]
		require.NotNil(t, header, operation)
		assert.Equal(t, "credential-test", header.Get(kwsCredentialHeader), operation)
		assert.Empty(t, header.Get("Authorization"), operation)

		raw, decodeErr := base64.RawStdEncoding.DecodeString(header.Get(kwsBinaryHeader))
		require.NoError(t, decodeErr, operation)
		var metadata kws_pb.KwsModelPb
		require.NoError(t, proto.Unmarshal(raw, &metadata), operation)
		assert.Equal(t, "agentcut-test", metadata.GetServiceName(), operation)
	}
}

func TestClientPropagatesRequestLaneToUnaryAndStreamRequests(t *testing.T) {
	t.Setenv(kwsServiceNameEnv, "")
	t.Setenv(kwsServiceCredentialEnv, "")
	service, server := newRecordingServer(t)
	client, err := New(server.URL, "")
	require.NoError(t, err)

	lane, err := requestlane.Parse("dev_agentcut")
	require.NoError(t, err)
	ctx := requestlane.ContextWithLane(t.Context(), lane)

	_, err = client.CreateSession(ctx, "lane test", &apiv1.Profile{})
	require.NoError(t, err)
	stream, err := client.WatchEvents(ctx, "ses_test", "exe_test")
	require.NoError(t, err)
	require.True(t, stream.Receive())
	require.NoError(t, stream.Close())

	assert.Equal(t, "dev_agentcut",
		service.headers["CreateSession"].Get(requestlane.RPCMetadataKey))
	assert.Equal(t, "dev_agentcut",
		service.headers["WatchEvents"].Get(requestlane.RPCMetadataKey))
}

func TestNewFromEnvAllowsLocalAuthOffWithoutKWS(t *testing.T) {
	t.Setenv(kwsServiceNameEnv, "")
	t.Setenv(kwsServiceCredentialEnv, "")
	t.Setenv("AGENTK_API_KEY", "")
	service, server := newRecordingServer(t)
	t.Setenv("AGENTK_BASE_URL", server.URL)

	client, err := NewFromEnv()
	require.NoError(t, err)
	require.NotNil(t, client)
	identity, err := client.ValidateAuth(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "app-agentcut", identity.AppID)
	assert.Equal(t, "AgentCut", identity.DisplayName)
	assert.Empty(t, service.headers["WhoAmI"].Get(kwsBinaryHeader))
	assert.Empty(t, service.headers["WhoAmI"].Get(kwsCredentialHeader))
	assert.Empty(t, service.headers["WhoAmI"].Get("Authorization"))
}

func TestNewFromEnvUsesSDKDefaultWhenEndpointIsUnset(t *testing.T) {
	_, server := newRecordingServer(t)
	var (
		defaultOptions sdkclient.Options
		defaultCalled  bool
		endpointCalled bool
	)
	getenv := func(name string) string {
		values := map[string]string{
			"KWS_SERVICE_NAME": "klingai-agentk-agentcut-api",
		}
		return values[name]
	}
	defaultFactory := func(options sdkclient.Options) (*sdkclient.Client, error) {
		defaultCalled = true
		defaultOptions = options
		return agentkhttp.New(sdkclient.Options{Endpoint: server.URL})
	}
	endpointFactory := func(sdkclient.Options) (*sdkclient.Client, error) {
		endpointCalled = true
		return nil, assert.AnError
	}

	client, err := newFromEnv(getenv, defaultFactory, endpointFactory)
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.True(t, defaultCalled)
	assert.False(t, endpointCalled)
	assert.Empty(t, defaultOptions.Endpoint)
}

func TestNewFromEnvUsesHTTPOverrideWhenEndpointIsSet(t *testing.T) {
	_, server := newRecordingServer(t)
	var (
		defaultCalled  bool
		endpointCalled bool
		gotOptions     sdkclient.Options
	)
	getenv := func(name string) string {
		if name == "AGENTK_BASE_URL" {
			return server.URL
		}
		return ""
	}
	defaultFactory := func(sdkclient.Options) (*sdkclient.Client, error) {
		defaultCalled = true
		return nil, assert.AnError
	}
	endpointFactory := func(options sdkclient.Options) (*sdkclient.Client, error) {
		endpointCalled = true
		gotOptions = options
		return agentkhttp.New(options)
	}

	client, err := newFromEnv(getenv, defaultFactory, endpointFactory)
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.False(t, defaultCalled)
	assert.True(t, endpointCalled)
	assert.Equal(t, server.URL, gotOptions.Endpoint)
}

func TestNewFromEnvAllowsExplicitManualOnly(t *testing.T) {
	factory := func(sdkclient.Options) (*sdkclient.Client, error) {
		t.Fatal("SDK factory must not be called in manual-only mode")
		return nil, nil
	}
	client, err := newFromEnv(func(name string) string {
		if name == agentKDisabledEnv {
			return "true"
		}
		return ""
	}, factory, factory)
	require.NoError(t, err)
	assert.Nil(t, client)
}

func TestNewFromEnvRejectsInvalidDisabledFlag(t *testing.T) {
	factory := func(sdkclient.Options) (*sdkclient.Client, error) {
		t.Fatal("SDK factory must not be called for invalid configuration")
		return nil, nil
	}
	client, err := newFromEnv(func(name string) string {
		if name == agentKDisabledEnv {
			return "sometimes"
		}
		return ""
	}, factory, factory)
	assert.Nil(t, client)
	require.ErrorContains(t, err, agentKDisabledEnv)
}

func TestLegacyAPIKeyEnvironmentIsIgnored(t *testing.T) {
	t.Setenv(kwsServiceNameEnv, "")
	t.Setenv(kwsServiceCredentialEnv, "")
	t.Setenv("AGENTK_API_KEY", "retired-secret")
	service, server := newRecordingServer(t)
	t.Setenv("AGENTK_BASE_URL", server.URL)

	client, err := NewFromEnv()
	require.NoError(t, err)
	_, err = client.ValidateAuth(t.Context())
	require.NoError(t, err)
	assert.Empty(t, service.headers["WhoAmI"].Get("Authorization"))
}
