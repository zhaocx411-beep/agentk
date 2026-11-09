package azure

import (
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
)

func Direct(base, key string) gateway.Route {
	return route.New(gateway.MustURL(base), nil, gateway.HeaderKey{Name: "api-key", Value: key})
}
