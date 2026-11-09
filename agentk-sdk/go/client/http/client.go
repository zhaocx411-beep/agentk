// Package http connects to AgentK through the HTTP gateway.
package http

import (
	stdhttp "net/http"
	"os"
	"strings"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/client"
)

const (
	stagingGateway    = "https://agentk-api.staging.kuaishou.com"
	productionGateway = "http://agentk-api.internal"
)

// New creates a Client that connects to options.Endpoint when set, or through
// the environment's AgentK gateway otherwise. The gateway transport carries no
// caller identity.
func New(options client.Options) (*client.Client, error) {
	endpoint := options.Endpoint
	if endpoint == "" {
		endpoint = gatewayURL(os.Getenv)
	}
	return client.New(stdhttp.DefaultClient, endpoint)
}

func gatewayURL(getenv func(string) string) string {
	switch strings.ToUpper(strings.TrimSpace(getenv("KWS_SERVICE_STAGE"))) {
	case "STAGING", "CANDIDATE":
		return stagingGateway
	default:
		return productionGateway
	}
}
