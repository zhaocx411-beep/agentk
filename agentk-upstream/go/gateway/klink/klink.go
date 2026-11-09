package klink

import (
	"os"

	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
)

const (
	officeBase = "https://boom.corp.kuaishou.com/klink"
	idcBase    = "http://klink.internal"
)

func OpenAI(apiKey string) gateway.Route {
	return route.New(gateway.MustURL(baseURL()).JoinPath("v1"), nil, gateway.Bearer(apiKey))
}

func Anthropic(apiKey string) gateway.Route {
	return route.New(gateway.MustURL(baseURL()), nil, gateway.Bearer(apiKey))
}

func Gemini(apiKey string) gateway.Route {
	return route.New(gateway.MustURL(baseURL()).JoinPath("v1beta"), nil,
		gateway.HeaderKey{Name: "x-goog-api-key", Value: apiKey})
}

// Gemini async image submissions use Bearer authentication on KLink.
func GeminiAsync(apiKey string) gateway.Route {
	return route.New(gateway.MustURL(baseURL()).JoinPath("v1beta"), nil, gateway.Bearer(apiKey))
}

func Ark(apiKey string) gateway.Route {
	return route.New(gateway.MustURL(baseURL()).JoinPath("ark", "api", "v3"), nil,
		gateway.Bearer(apiKey))
}

func baseURL() string {
	if os.Getenv("KWS_SERVICE_STAGE") == "" {
		return officeBase
	}
	return idcBase
}
