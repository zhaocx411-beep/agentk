package brightdata_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/brightdata"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
)

func TestSearch(t *testing.T) {
	for _, interval := range []string{"qdr:d", ""} {
		t.Run(interval, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/request" || r.Header.Get("Authorization") != "Bearer secret" {
					t.Errorf("request=%s %s %v", r.Method, r.URL, r.Header)
				}
				var body struct{ Zone, URL, Format string }
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				target, err := url.Parse(body.URL)
				if err != nil {
					t.Error(err)
					return
				}
				q := target.Query()
				if body.Zone != "zone" ||
					body.Format != "raw" ||
					target.Host != "www.google.com" ||
					target.Path != "/search" ||
					!strings.HasPrefix(
						target.RawQuery,
						"q=",
					) ||
					q.Get("q") != "深圳 天气&雨" ||
					q.Get("hl") != "zh-CN" ||
					q.Get("gl") != "cn" ||
					q.Get("brd_json") != "1" ||
					q.Get("tbs") != interval ||
					q.Has("num") ||
					(interval == "" &&
						q.Has("tbs")) {
					t.Errorf("body=%+v", body)
				}
				_, _ = io.WriteString(
					w,
					`{"organic":[{"title":"天气","link":"https://weather.test","description":"晴"},{"title":"无链接"}]}`,
				)
			}))
			defer srv.Close()
			api, err := brightdata.NewSearch(
				route.New(
					gateway.MustURL(srv.URL+"/request"),
					nil,
					gateway.Bearer("secret"),
				),
				upstream.WithHTTPClient(srv.Client()),
			)
			if err != nil {
				t.Fatal(err)
			}
			got, err := api.Search(
				t.Context(),
				brightdata.Request{
					Query:     "深圳 天气&雨",
					Zone:      "zone",
					Language:  "zh-CN",
					Country:   "cn",
					TimeRange: interval,
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 ||
				len(got) != 2 ||
				got[0].Title != "天气" ||
				got[0].Link != "https://weather.test" ||
				got[0].Description != "晴" {
				t.Fatalf("results=%+v calls=%d", got, calls)
			}
		})
	}
}
func TestFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{
			"empty",
			200,
			"",
		}, {
			"whitespace",
			200,
			"  \n ",
		}, {
			"cooldown",
			200,
			"Please try again after 15 seconds",
		}, {
			"http",
			401,
			"bad zone",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("x-request-id", "req-id")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			api, err := brightdata.NewSearch(
				route.New(
					gateway.MustURL(srv.URL),
					nil,
					nil,
				),
				upstream.WithHTTPClient(srv.Client()),
			)
			if err != nil {
				t.Fatal(err)
			}
			_, err = api.Search(t.Context(), brightdata.Request{Query: "q", Zone: "z"})
			if err == nil || calls != 1 {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
			if tc.name == "cooldown" && !strings.Contains(err.Error(), "15 seconds") {
				t.Fatal(err)
			}
			if tc.status != 200 {
				var e *upstream.APIError
				if !errors.As(err, &e) || e.StatusCode != 401 || e.Message != "bad zone" || e.RequestID != "req-id" {
					t.Fatalf("error=%v", err)
				}
			}
		})
	}
}
