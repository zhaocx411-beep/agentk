package clientconfig

import (
	"context"
	"net/http"
	"net/url"

	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
)

type Options struct {
	HTTPClient *http.Client
	Warn       func(context.Context, string, ...any)
}
type Option func(*Options)

func New(r route.Route, opts ...Option) (*url.URL, *http.Client, error) {
	return Resolve(opts...).Client(r)
}
func Resolve(opts ...Option) Options {
	cfg := Options{HTTPClient: http.DefaultClient}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
func (cfg Options) Client(r route.Route) (*url.URL, *http.Client, error) {
	base, err := route.Base(r)
	if err != nil {
		return nil, nil, err
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	client := *cfg.HTTPClient
	client.Transport = route.Transport(r, client.Transport)
	return base, &client, nil
}
