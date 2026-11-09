package websearch

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go.temporal.io/sdk/temporal"
)

type searchCatalog map[string]Client

func (c searchCatalog) SearchClient(name string) (Client, bool) {
	client, ok := c[name]
	return client, ok
}

func validSearchInput() CapabilityInput {
	return CapabilityInput{Model: "volc-search", Query: "Go 1.26 发布了什么", TimeRange: TimeRangeWeek}
}

func TestSearchCapabilityActivity(t *testing.T) {
	client := &stubClient{results: []Result{{Title: "t", URL: "https://a.test", Summary: "s"}}}
	a := NewActivity(searchCatalog{"volc-search": client})

	out, err := a.Run(context.Background(), validSearchInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Results) != 1 || out.Results[0].URL != "https://a.test" {
		t.Fatalf("results = %+v", out.Results)
	}
	// 检索词与档位原样回带：只看结果列表看不出"我搜的到底是什么"
	if out.Model != "volc-search" || out.Query != "Go 1.26 发布了什么" || out.TimeRange != TimeRangeWeek {
		t.Fatalf("out = %+v", out)
	}
	if client.last.Query != "Go 1.26 发布了什么" || client.last.TimeRange != TimeRangeWeek {
		t.Fatalf("client request = %+v", client.last)
	}
}

// TestSearchCapabilityActivityEmptyResults 没搜到不是错误，但要序列化成 []
// 而不是 null——模型对 null 的反应不稳定。
func TestSearchCapabilityActivityEmptyResults(t *testing.T) {
	a := NewActivity(searchCatalog{"m": &stubClient{}})
	in := validSearchInput()
	in.Model = "m"
	out, err := a.Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"results":[]`) {
		t.Fatalf("empty results = %s", raw)
	}
}

func TestSearchCapabilityActivityValidation(t *testing.T) {
	client := &stubClient{}
	a := NewActivity(searchCatalog{"volc-search": client})

	for name, mutate := range map[string]func(*CapabilityInput){
		"missing model":       func(in *CapabilityInput) { in.Model = "" },
		"missing query":       func(in *CapabilityInput) { in.Query = "  " },
		"query too long":      func(in *CapabilityInput) { in.Query = strings.Repeat("字", maxQueryChars+1) },
		"bad time range":      func(in *CapabilityInput) { in.TimeRange = "OneWeek" },
		"model not available": func(in *CapabilityInput) { in.Model = "nope" },
	} {
		in := validSearchInput()
		mutate(&in)
		_, err := a.Run(context.Background(), in)
		var appErr *temporal.ApplicationError
		if !errors.As(err, &appErr) || !appErr.NonRetryable() {
			t.Fatalf("%s: err = %v, want non-retryable", name, err)
		}
	}
	// 入参错误可由模型修正，不应调用供应商并进行无效重试。
	if client.last.Query != "" {
		t.Fatal("invalid input must not reach the upstream client")
	}
}

func TestSearchCapabilityActivityUpstreamErrorRetryable(t *testing.T) {
	a := NewActivity(searchCatalog{"m": &stubClient{err: errors.New("boom")}})
	in := validSearchInput()
	in.Model = "m"
	_, err := a.Run(context.Background(), in)
	if err == nil {
		t.Fatal("search error swallowed")
	}
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) && appErr.NonRetryable() {
		t.Fatalf("transient search error must stay retryable: %v", err)
	}
}

func TestSearchCapabilityActivityRequiresCatalog(t *testing.T) {
	a := NewActivity(nil)
	_, err := a.Run(context.Background(), validSearchInput())
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) || !appErr.NonRetryable() {
		t.Fatalf("err = %v, want non-retryable", err)
	}
}
