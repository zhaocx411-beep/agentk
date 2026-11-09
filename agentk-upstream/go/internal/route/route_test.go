package route_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
)

func capture(t *testing.T) (*httptest.Server, *http.Request) {
	t.Helper()
	var got http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = *r.Clone(r.Context())
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestTransportInjectsHeadersAndAuth(t *testing.T) {
	srv, got := capture(t)
	headers := http.Header{}
	headers.Set("x-static", "1")
	ep := route.New(gateway.MustURL(srv.URL), headers, gateway.Bearer("k"))

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/x", nil)
	req.Header.Set("x-caller", "c")
	resp, err := (&http.Client{Transport: route.Transport(ep, nil)}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if got.Header.Get("x-static") != "1" || got.Header.Get("Authorization") != "Bearer k" || got.Header.Get("x-caller") != "c" {
		t.Fatalf("headers = %v", got.Header)
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatal("caller request must not be mutated")
	}
}

func TestTransportKeepsCredentialOffOtherHosts(t *testing.T) {
	other, got := capture(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/moved", http.StatusFound)
	}))
	t.Cleanup(origin.Close)
	headers := http.Header{}
	headers.Set("x-static", "1")
	ep := route.New(gateway.MustURL(origin.URL), headers, gateway.Bearer("k"))

	resp, err := (&http.Client{Transport: route.Transport(ep, nil)}).Get(origin.URL + "/x")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got.URL.Path != "/moved" {
		t.Fatalf("redirect not followed, path = %q", got.URL.Path)
	}
	if got.Header.Get("Authorization") != "" || got.Header.Get("x-static") != "" {
		t.Fatalf("credential leaked to other host: %v", got.Header)
	}
}
