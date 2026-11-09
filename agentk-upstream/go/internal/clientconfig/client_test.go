package clientconfig_test

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/clientconfig"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestClientTemplates(t *testing.T) {
	for _, kind := range []string{"custom", "default", "nil"} {
		t.Run(kind, func(t *testing.T) {
			jar, _ := cookiejar.New(nil)
			base := gateway.MustURL("https://example.com")
			jar.SetCookies(base, []*http.Cookie{{Name: "session", Value: "cookie"}})
			var auths []string
			redirected := false
			transport := transportFunc(func(r *http.Request) (*http.Response, error) {
				auths = append(auths, r.Header.Get("Authorization"))
				if r.Header.Get("Cookie") != "session=cookie" {
					t.Errorf("cookies = %s", r.Header.Get("Cookie"))
				}
				if _, ok := r.Context().Deadline(); !ok {
					t.Error("client timeout not applied")
				}
				return &http.Response{
					StatusCode: 302,
					Header: http.Header{
						"Location": []string{
							"/next",
						},
					},
					Body:    io.NopCloser(strings.NewReader("")),
					Request: r,
				}, nil
			})
			original := &http.Client{
				Transport:     transport,
				Timeout:       time.Minute,
				Jar:           jar,
				CheckRedirect: func(*http.Request, []*http.Request) error { redirected = true; return http.ErrUseLastResponse },
			}
			opts := []upstream.Option{upstream.WithHTTPClient(original)}
			if kind != "custom" {
				old := http.DefaultClient
				http.DefaultClient = original
				t.Cleanup(func() { http.DefaultClient = old })
				opts = nil
				if kind == "nil" {
					opts = []upstream.Option{upstream.WithHTTPClient(nil)}
				}
			}
			for _, key := range []string{"one", "two"} {
				_, client, err := clientconfig.New(route.New(base, nil, gateway.Bearer(key)), opts...)
				if err != nil {
					t.Fatal(err)
				}
				resp, err := client.Get(base.String())
				if err != nil {
					t.Fatal(err)
				}
				if err := resp.Body.Close(); err != nil {
					t.Fatal(err)
				}
			}
			resp, err := original.Get(base.String())
			if err != nil {
				t.Fatal(err)
			}
			if err := resp.Body.Close(); err != nil {
				t.Fatal(err)
			}
			if !redirected || len(auths) != 3 || auths[0] != "Bearer one" || auths[1] != "Bearer two" || auths[2] != "" {
				t.Fatalf("redirect=%v auth=%v", redirected, auths)
			}
			if original.Timeout != time.Minute || original.Jar != jar {
				t.Fatal("caller client changed")
			}
		})
	}
}
func TestInvalidRoute(t *testing.T) {
	for _, raw := range []string{"", "relative/path", "ftp://example.com", "https:///missing-host"} {
		_, _, err := clientconfig.New(
			route.New(
				gateway.MustURL(raw),
				nil,
				nil,
			),
			upstream.WithHTTPClient(&http.Client{
				Transport: transportFunc(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected request"); return nil, nil }),
			}),
		)
		if err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}
