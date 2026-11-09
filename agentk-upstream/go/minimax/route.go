package minimax

import (
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
)

func Direct(key string) gateway.Route {
	return route.New(gateway.MustURL("https://api.minimax.chat"), nil, gateway.Bearer(key))
}
