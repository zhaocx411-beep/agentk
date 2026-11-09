package volcsearch

import (
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
)

func Direct(key string) gateway.Route {
	return route.New(gateway.MustURL("https://open.feedcoopapi.com/search_api/web_search"), nil, gateway.Bearer(key))
}
