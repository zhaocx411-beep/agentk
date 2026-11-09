package klink_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway/klink"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
)

func TestOpenAI(t *testing.T) {
	for _, test := range []struct {
		stage, base string
	}{
		{"", "https://boom.corp.kuaishou.com/klink/v1"},
		{"STAGING", "http://klink.internal/v1"},
		{"PROD", "http://klink.internal/v1"},
		{"CANDIDATE", "http://klink.internal/v1"},
		{"unknown", "http://klink.internal/v1"},
	} {
		t.Run(test.stage, func(t *testing.T) {
			t.Setenv("KWS_SERVICE_STAGE", test.stage)
			ep := klink.OpenAI("k")
			base, err := route.Base(ep)
			if err != nil {
				t.Fatal(err)
			}
			if base.String() != test.base {
				t.Errorf("base = %s, want %s", base, test.base)
			}
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, test.base, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = route.Transport(ep, capture(func(r *http.Request) { req = r })).RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			if req.Header.Get("Authorization") != "Bearer k" {
				t.Errorf("authorization headers = %v", req.Header)
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
