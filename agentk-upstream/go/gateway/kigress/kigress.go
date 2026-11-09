package kigress

import (
	"net/http"
	"net/url"
	"os"

	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
)

// BizScene is the x-ks-biz-scene value a tenant was provisioned with.
type BizScene string

const (
	Online  BizScene = "online"
	Offline BizScene = "offline"
)

const (
	officeMain = "https://kigress-gateway.corp.kuaishou.com"
	officeSGP  = "https://kigress-gateway-sgp.corp.kuaishou.com"
	idcMain    = "http://kigress-gateway.internal"
	idcSGP     = "http://kigress-gateway-sgp.internal"
)

type Tenant struct {
	main    *url.URL
	sgp     *url.URL
	userKey string
	scene   BizScene
	apiKey  string
}

func NewTenant(userKey string, scene BizScene, apiKey string) Tenant {
	main, sgp := idcMain, idcSGP
	switch os.Getenv("KWS_SERVICE_STAGE") {
	case "":
		main, sgp = officeMain, officeSGP
	case "STAGING":
		sgp = officeSGP
	}
	return Tenant{
		main: gateway.MustURL(main), sgp: gateway.MustURL(sgp),
		userKey: userKey, scene: scene, apiKey: apiKey,
	}
}

// UserKey returns the x-ks-user-key routing prefix.
func (t Tenant) UserKey() string { return t.userKey }

func (t Tenant) endpoint(host *url.URL, llmModel string, path ...string) gateway.Route {
	headers := http.Header{}
	headers.Set("x-ks-user-key", t.userKey)
	headers.Set("x-ks-biz-scene", string(t.scene))
	headers.Set("x-ks-llm-model", llmModel)
	return route.New(host.JoinPath(path...), headers, gateway.HeaderKey{Name: "x-api-key", Value: t.apiKey})
}

func (t Tenant) OpenAI(llmModel string) gateway.Route {
	return t.endpoint(t.main, llmModel, t.userKey, "v1")
}

func (t Tenant) Vertex(project, llmModel string) gateway.Route {
	return t.endpoint(t.sgp, llmModel,
		"normal", t.userKey, "v1beta1", "projects", project, "locations", "global", "publishers", "google")
}

func (t Tenant) AzureDeployment(deployment string) gateway.Route {
	return t.endpoint(t.main, deployment, t.userKey, "openai", "deployments", deployment)
}

func (t Tenant) Ark(product, endpointID string) gateway.Route {
	return t.endpoint(t.main, endpointID, t.userKey, product, "api", "v3")
}

func (t Tenant) DashScope(model string) gateway.Route {
	return t.endpoint(t.main, model, t.userKey, model, "api", "v1")
}
