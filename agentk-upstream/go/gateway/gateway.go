package gateway

import (
	"context"
	"net/http"
	"net/url"

	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
)

type Route = route.Route
type Authorizer = route.Authorizer
type Bearer string

func (b Bearer) Authorize(_ context.Context, req *http.Request) error {
	req.Header.Set("Authorization", "Bearer "+string(b))
	return nil
}

type HeaderKey struct {
	Name  string
	Value string
}

func (h HeaderKey) Authorize(_ context.Context, req *http.Request) error {
	req.Header.Set(h.Name, h.Value)
	return nil
}

func MustURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic("gateway: invalid URL " + raw + ": " + err.Error())
	}
	return u
}
