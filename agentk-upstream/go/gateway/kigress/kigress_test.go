package kigress_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway/kigress"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
)

func TestRoutes(t *testing.T) {
	for _, env := range []struct {
		stage, main, sgp string
	}{
		{"", "https://kigress-gateway.corp.kuaishou.com", "https://kigress-gateway-sgp.corp.kuaishou.com"},
		{"STAGING", "http://kigress-gateway.internal", "https://kigress-gateway-sgp.corp.kuaishou.com"},
		{"PROD", "http://kigress-gateway.internal", "http://kigress-gateway-sgp.internal"},
		{"CANDIDATE", "http://kigress-gateway.internal", "http://kigress-gateway-sgp.internal"},
		{"unknown", "http://kigress-gateway.internal", "http://kigress-gateway-sgp.internal"},
	} {
		t.Run(env.stage, func(t *testing.T) {
			t.Setenv("KWS_SERVICE_STAGE", env.stage)
			tenant := kigress.NewTenant("t-1", kigress.Online, "secret")
			cases := []struct {
				ep       gateway.Route
				base     string
				llmModel string
			}{
				{tenant.OpenAI("gpt-5.4"), env.main + "/t-1/v1", "gpt-5.4"},
				{
					tenant.Vertex(
						"proj",
						"gemini-3",
					),
					env.sgp + "/normal/t-1/v1beta1/projects/proj/locations/global/publishers/google",
					"gemini-3",
				},
				{tenant.AzureDeployment("dep"), env.main + "/t-1/openai/deployments/dep", "dep"},
				{tenant.Ark("doubao-seedance-2.0", "ep-1"), env.main + "/t-1/doubao-seedance-2.0/api/v3", "ep-1"},
				{tenant.DashScope("qwen-image-edit"), env.main + "/t-1/qwen-image-edit/api/v1", "qwen-image-edit"},
			}
			for _, c := range cases {
				base, err := route.Base(c.ep)
				if err != nil {
					t.Fatal(err)
				}
				if base.String() != c.base {
					t.Errorf("base = %s, want %s", base, c.base)
				}

				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, c.base, nil)
				if err != nil {
					t.Fatal(err)
				}
				_, err = route.Transport(c.ep, capture(func(r *http.Request) { req = r })).RoundTrip(req)
				if err != nil {
					t.Fatal(err)
				}
				if req.Header.Get("x-ks-llm-model") != c.llmModel ||
					req.Header.Get("x-ks-user-key") != "t-1" ||
					req.Header.Get("x-ks-biz-scene") != "online" {
					t.Errorf("headers = %v", req.Header)
				}
				if req.Header.Get("x-api-key") != "secret" {
					t.Errorf("authorization headers = %v", req.Header)
				}
			}
		})
	}
}

type capture func(*http.Request)

func (f capture) RoundTrip(r *http.Request) (*http.Response, error) {
	f(r)
	return &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    r,
	}, nil
}
