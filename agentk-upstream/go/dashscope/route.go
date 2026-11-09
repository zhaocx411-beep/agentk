package dashscope

import (
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
)

func Direct(key string) gateway.Route {
	return route.New(gateway.MustURL("https://dashscope.aliyuncs.com"), nil, gateway.Bearer(key))
}

func OpenAI(key string) gateway.Route {
	return route.New(gateway.MustURL("https://dashscope.aliyuncs.com/compatible-mode/v1"), nil, gateway.Bearer(key))
}
