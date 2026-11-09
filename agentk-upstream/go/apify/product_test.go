package apify_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/apify"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
)

func TestActors(t *testing.T) {
	for _, amazon := range []bool{false, true} {
		t.Run(map[bool]string{false: "taobao", true: "amazon"}[amazon], func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" ||
					r.URL.Path != "/prefix/v2/acts/vendor~actor/run-sync-get-dataset-items" ||
					r.URL.Query().Get("clean") != "true" ||
					r.URL.Query().Has("maxItems") ||
					r.Header.Get("Authorization") != "Bearer token" ||
					r.Header.Get("Accept") != "application/json" {
					t.Errorf("request=%s %s %v", r.Method, r.URL, r.Header)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				if amazon {
					urls, ok := body["categoryOrProductUrls"].([]any)
					if !ok || len(urls) != 1 {
						t.Errorf("URLs=%v", body)
						return
					}
					u, _ := urls[0].(map[string]any)
					if u["url"] != "https://amazon.test/dp/1" ||
						body["maxItemsPerStartUrl"] != float64(1) ||
						body["maxSearchPagesPerStartUrl"] != float64(2) ||
						body["maxProductVariantsAsSeparateResults"] != float64(3) ||
						body["maxOffers"] != float64(4) ||
						body["scrapeProductDetails"] != true {
						t.Errorf("body=%v", body)
					}
				} else {
					items, ok := body["items"].([]any)
					if !ok || len(items) != 1 || items[0] != "100" {
						t.Errorf("body=%v", body)
					}
				}
				_, _ = io.WriteString(
					w,
					`[{"asin":"ASIN","title":"shirt","price":{"value":12.5},"mainPictureUrl":"//img.test/main.jpg",`+
						`"highResolutionImages":["https://img.test/high.jpg"],"pictures":["//img.test/side.jpg"],`+
						`"skus":[{"skuId":"200","propsIds":"color:red","propsNames":"红"},{"skuId":201}],`+
						`"propsImages":{"color:red":"//img.test/red.jpg"},"resolvedUrl":"https://item.test/100"}]`,
				)
			}))
			defer srv.Close()
			api, err := apify.NewProduct(
				route.New(
					gateway.MustURL(srv.URL+"/prefix"),
					nil,
					gateway.Bearer("token"),
				),
				upstream.WithHTTPClient(srv.Client()),
			)
			if err != nil {
				t.Fatal(err)
			}
			var got []apify.Product
			if amazon {
				got, err = api.Amazon(
					t.Context(),
					apify.AmazonRequest{
						Actor:          "vendor~actor",
						URL:            "https://amazon.test/dp/1",
						MaxItems:       1,
						MaxSearchPages: 2,
						MaxVariants:    3,
						MaxOffers:      4,
						ScrapeDetails:  true,
					},
				)
			} else {
				got, err = api.Taobao(t.Context(), apify.TaobaoRequest{Actor: "vendor~actor", Items: []string{"100"}})
			}
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || len(got) != 1 {
				t.Fatalf("calls=%d products=%v", calls, got)
			}
			p := got[0]
			if p.ASIN != "ASIN" ||
				p.Raw["title"] != "shirt" ||
				len(p.SKUs) != 2 ||
				string(p.SKUs[0].SKUID) != `"200"` ||
				string(p.SKUs[1].SKUID) != "201" ||
				p.SKUs[0].PropsNames != "红" ||
				p.PropsImages["color:red"] != "//img.test/red.jpg" ||
				p.MainPictureURL != "//img.test/main.jpg" ||
				len(p.HighResolutionImages) != 1 ||
				len(p.Pictures) != 1 ||
				p.ResolvedURL != "https://item.test/100" {
				t.Fatalf("product=%+v", p)
			}
		})
	}
}
func TestResponses(t *testing.T) {
	for _, tc := range []struct {
		name                string
		status              int
		body, code, message string
		wantError           bool
	}{
		{"empty dataset", 200, `[]`, "", "", false},
		{
			"actor error",
			200,
			`[{"error":"NOT_FOUND","errorDescription":"missing product"}]`,
			"NOT_FOUND",
			"NOT_FOUND: missing product",
			true,
		},
		{"http error", 403, `denied`, "", "denied", true},
		{"invalid json", 200, `[`, "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("x-request-id", "req-id")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			api, err := apify.NewProduct(route.New(gateway.MustURL(srv.URL), nil, nil), upstream.WithHTTPClient(srv.Client()))
			if err != nil {
				t.Fatal(err)
			}
			got, err := api.Taobao(t.Context(), apify.TaobaoRequest{Actor: "vendor~actor", Items: []string{"1"}})
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v", err)
			}
			if !tc.wantError && len(got) != 0 {
				t.Fatalf("products=%v", got)
			}
			if tc.message != "" {
				var e *upstream.APIError
				if !errors.As(
					err,
					&e,
				) || e.StatusCode != tc.status || e.Code != tc.code || e.Message != tc.message || e.RequestID != "req-id" {
					t.Fatalf("error=%v", err)
				}
			}
		})
	}
}
