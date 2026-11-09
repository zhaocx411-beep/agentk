package http

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/client"
	apiv1 "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1/apiv1connect"
)

type gatewayService struct {
	apiv1connect.UnimplementedAgentKServiceHandler
}

func (s gatewayService) WhoAmI(
	_ context.Context,
	request *connect.Request[apiv1.WhoAmIRequest],
) (*connect.Response[apiv1.WhoAmIResponse], error) {
	return connect.NewResponse(&apiv1.WhoAmIResponse{
		Id:          request.Header().Get("kws-bin"),
		AppId:       request.Header().Get("kwsCredential"),
		DisplayName: request.Header().Get("Authorization"),
	}), nil
}

func TestGatewayURL(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		stage string
		want  string
	}{
		"staging":   {stage: "STAGING", want: stagingGateway},
		"candidate": {stage: "candidate", want: stagingGateway},
		"production": {
			stage: "PRODUCTION",
			want:  productionGateway,
		},
		"unset":   {want: productionGateway},
		"unknown": {stage: "development", want: productionGateway},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got := gatewayURL(func(key string) string {
				if key != "KWS_SERVICE_STAGE" {
					t.Fatalf("getenv key = %q", key)
				}
				return test.stage
			})
			if got != test.want {
				t.Fatalf("gatewayURL() = %q, want %q", got, test.want)
			}
		})
	}
}

// The gateway transport carries no KWS identity even when the process has one.
func TestHTTPConnectRoundTrip(t *testing.T) {
	t.Setenv("KWS_SERVICE_NAME", "agentk.sdk.fixture/原样")
	t.Setenv("KWS_SERVICE_CREDENTIAL", "gateway-credential")

	path, handler := apiv1connect.NewAgentKServiceHandler(gatewayService{})
	mux := stdhttp.NewServeMux()
	mux.Handle(path, handler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	sdkClient, err := New(client.Options{
		Endpoint: server.URL,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	identity, err := sdkClient.WhoAmI(t.Context(), &apiv1.WhoAmIRequest{})
	if err != nil {
		t.Fatalf("WhoAmI() error = %v", err)
	}
	if identity.GetId() != "" {
		t.Fatalf("kws-bin = %q, want empty", identity.GetId())
	}
	if identity.GetAppId() != "" {
		t.Fatalf("kwsCredential = %q, want empty", identity.GetAppId())
	}
	if identity.GetDisplayName() != "" {
		t.Fatalf("Authorization = %q, want empty", identity.GetDisplayName())
	}
}
