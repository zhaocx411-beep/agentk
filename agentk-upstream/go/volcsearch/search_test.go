package volcsearch_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/volcsearch"
)

func TestSearch(t *testing.T) {
	for _, interval := range []string{"OneWeek", ""} {
		t.Run(interval, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" ||
					r.URL.Path != "/search" ||
					r.Header.Get("Authorization") != "Bearer secret" ||
					r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("request = %s %s %v", r.Method, r.URL, r.Header)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				filter, _ := body["Filter"].(map[string]any)
				if body["Query"] != "深圳 天气" ||
					body["Count"] != float64(7) ||
					body["SearchType"] != "web" ||
					body["NeedSummary"] != true ||
					filter["NeedUrl"] != true ||
					filter["NeedContent"] != false {
					t.Errorf("body = %#v", body)
				}
				value, exists := body["TimeRange"]
				if interval == "" && exists || interval != "" && value != interval {
					t.Errorf("TimeRange = %v", value)
				}
				_, _ = io.WriteString(
					w,
					`{"Result":{"WebResults":[{"Title":"天气","Url":"https://weather.test","Summary":"多云"},`+
						`{"Title":"片段","Snippet":"晴"},{"Title":"没有链接"}]}}`,
				)
			}))
			defer srv.Close()
			api, err := volcsearch.NewSearch(
				route.New(
					gateway.MustURL(srv.URL+"/search"),
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
				volcsearch.Request{
					Query:       "深圳 天气",
					Count:       7,
					TimeRange:   interval,
					NeedSummary: true,
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 ||
				len(got) != 3 ||
				got[0].URL != "https://weather.test" ||
				got[0].Summary != "多云" ||
				got[1].Snippet != "晴" ||
				got[2].URL != "" {
				t.Fatalf("calls=%d results=%+v", calls, got)
			}
		})
	}
}
func TestSearchErrors(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		status                  int
		body, code, message, id string
	}{
		{
			"business",
			200,
			`{"ResponseMetadata":{"RequestId":"body-id","Error":{"Code":"10406","Message":"quota exhausted"}}}`,
			"10406",
			"quota exhausted",
			"body-id",
		},
		{
			"http",
			403,
			`{"ResponseMetadata":{"RequestId":"body-id","Error":{"Code":"Denied","Message":"forbidden"}}}`,
			"Denied",
			"forbidden",
			"body-id",
		},
		{
			"header preserved",
			429,
			`{"ResponseMetadata":{"Error":{"Code":"Limited","Message":"slow down"}}}`,
			"Limited",
			"slow down",
			"header-id",
		},
		{"non json", 502, `bad gateway`, "", "bad gateway", "header-id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("x-request-id", "header-id")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			api, err := volcsearch.NewSearch(
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
			_, err = api.Search(t.Context(), volcsearch.Request{Query: "q"})
			var e *upstream.APIError
			if !errors.As(err, &e) {
				t.Fatalf("error=%v", err)
			}
			if e.StatusCode != tc.status ||
				e.Code != tc.code ||
				e.Message != tc.message ||
				e.RequestID != tc.id ||
				calls != 1 {
				t.Fatalf("error=%+v calls=%d", e, calls)
			}
		})
	}
}
