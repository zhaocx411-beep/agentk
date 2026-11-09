package route_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
)

type authFunc func(context.Context, *http.Request) error

func (f authFunc) Authorize(c context.Context, r *http.Request) error { return f(c, r) }

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type idleTransport struct{ closed int }

func (t *idleTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unused")
}
func (t *idleTransport) CloseIdleConnections() { t.closed++ }
func TestConfigCopyAndAuthorizationOrder(t *testing.T) {
	base := gateway.MustURL("https://example.com/v1")
	headers := http.Header{"X-Static": []string{"original"}}
	ep := route.New(base, headers, authFunc(func(_ context.Context, r *http.Request) error {
		if r.Header.Get("X-Static") != "original" {
			t.Errorf("header before auth = %v", r.Header)
		}
		r.Header.Set("X-Static", "authorized")
		return nil
	}))
	base.Host = "changed.example"
	headers.Set("X-Static", "changed")
	req, _ := http.NewRequest(http.MethodGet, "https://example.com/v1", nil)
	req.Header.Set("X-Caller", "retained")
	sentinel := errors.New("transport error")
	_, err := route.Transport(ep, transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("X-Static") != "authorized" || r.Header.Get("X-Caller") != "retained" {
			t.Errorf("headers = %v", r.Header)
		}
		return nil, sentinel
	})).RoundTrip(req)
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v", err)
	}
	if req.Header.Get("X-Static") != "" {
		t.Fatal("request mutated")
	}
}
func TestAuthorizationFailureDoesNotSend(t *testing.T) {
	sentinel := errors.New("auth failed")
	ep := route.New(
		gateway.MustURL("https://example.com"),
		nil,
		authFunc(func(context.Context, *http.Request) error { return sentinel }),
	)
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	_, err := route.Transport(
		ep,
		transportFunc(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected send"); return nil, nil }),
	).RoundTrip(req)
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v", err)
	}
}
func TestCloseIdleConnectionsDelegates(t *testing.T) {
	base := &idleTransport{}
	client := &http.Client{Transport: route.Transport(route.New(gateway.MustURL("https://example.com"), nil, nil), base)}
	client.CloseIdleConnections()
	if base.closed != 1 {
		t.Fatalf("close calls = %d", base.closed)
	}
}
