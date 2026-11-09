package route

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type Authorizer interface {
	Authorize(context.Context, *http.Request) error
}
type Route struct {
	base    *url.URL
	headers http.Header
	auth    Authorizer
}

func Transport(e Route, next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	e.headers = e.headers.Clone()
	return roundTripper{next: next, endpoint: e}
}

type roundTripper struct {
	next     http.RoundTripper
	endpoint Route
}

func (r roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// http.Client 跨 host 重定向会剥离 Authorization，此处不能重新注入凭据。
	if !strings.EqualFold(req.URL.Scheme, r.endpoint.base.Scheme) ||
		!strings.EqualFold(req.URL.Host, r.endpoint.base.Host) {
		return r.next.RoundTrip(req)
	}
	req = req.Clone(req.Context())
	for key, values := range r.endpoint.headers {
		req.Header[key] = append([]string(nil), values...)
	}
	if r.endpoint.auth != nil {
		if err := r.endpoint.auth.Authorize(req.Context(), req); err != nil {
			return nil, err
		}
	}
	return r.next.RoundTrip(req)
}

func (r roundTripper) CloseIdleConnections() {
	if closer, ok := r.next.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func New(base *url.URL, headers http.Header, auth Authorizer) Route {
	var baseCopy *url.URL
	if base != nil {
		value := *base
		baseCopy = &value
	}
	return Route{base: baseCopy, headers: headers.Clone(), auth: auth}
}
func Base(r Route) (*url.URL, error) {
	if r.base == nil || (r.base.Scheme != "http" && r.base.Scheme != "https") || r.base.Host == "" {
		return nil, fmt.Errorf("upstream: route must be an absolute HTTP(S) URL")
	}
	value := *r.base
	return &value, nil
}
