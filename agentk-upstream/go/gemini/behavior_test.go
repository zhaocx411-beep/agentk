package gemini_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/chat"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gemini"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(r *http.Request, body io.ReadCloser) *http.Response {
	return &http.Response{
		StatusCode: 200,
		Header: http.Header{
			"Content-Type": []string{
				"application/json",
			},
		},
		Body:    body,
		Request: r,
	}
}
func TestRequestModel(t *testing.T) {
	var models []string
	client, err := gemini.NewChat(route.New(
		gateway.MustURL("https://example.com/v1"),
		nil,
		nil,
	), upstream.WithHTTPClient(&http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		models = append(models, strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/models/"), ":generateContent"))
		return response(
			r,
			io.NopCloser(strings.NewReader("{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}]}")),
		), nil
	})}))
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"model-one", "model-two"} {
		resp, err := client.Stream(t.Context(), chat.Request{Model: model})
		if err != nil {
			t.Fatal(err)
		}
		for _, err := range resp.Stream {
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(models) != 2 || models[0] != "model-one" || models[1] != "model-two" {
		t.Fatalf("models = %v", models)
	}
	if _, err := client.Stream(t.Context(), chat.Request{}); err == nil {
		t.Fatal("missing model accepted")
	}
	if len(models) != 2 {
		t.Fatal("missing model sent request")
	}
}

func TestTextReasoningUsageAndIteration(t *testing.T) {
	client, err := gemini.NewChat(route.New(
		gateway.MustURL("https://example.com"),
		nil,
		nil,
	), upstream.WithHTTPClient(&http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		return response(
			r,
			io.NopCloser(strings.NewReader(`{"candidates":[{"content":{"parts":[{"text":"think","thought":true,`+
				`"thoughtSignature":"sig"},{"text":"answer"}]},"finishReason":"STOP"}],`+
				`"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,`+
				`"cachedContentTokenCount":4}}`)),
		), nil
	})}))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Stream(t.Context(), chat.Request{Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resp.Result(); err == nil {
		t.Fatal("result available before consumption")
	}
	var deltas []chat.Delta
	for d, err := range resp.Stream {
		if err != nil {
			t.Fatal(err)
		}
		deltas = append(deltas, d)
	}
	result, err := resp.Result()
	if err != nil {
		t.Fatal(err)
	}
	if len(deltas) != 2 || len(result.Blocks) != 2 {
		t.Fatalf("deltas=%v result=%v", deltas, result)
	}
	for i, d := range deltas {
		if d.Block != i || d.Text != result.Blocks[i].Text || d.Kind != result.Blocks[i].Kind {
			t.Fatalf("delta=%v block=%v", d, result.Blocks[i])
		}
	}
	if result.Blocks[0].Kind != chat.BlockReasoning ||
		result.Blocks[0].Text != "think" ||
		result.Blocks[0].ProtocolState != "sig" ||
		result.Blocks[1].Kind != chat.BlockText ||
		result.Blocks[1].Text != "answer" {
		t.Fatalf("blocks=%v", result.Blocks)
	}
	if result.FinishReason != "STOP" || result.Usage != (chat.Usage{
		InputTokens:     10,
		OutputTokens:    2,
		CacheReadTokens: 4,
	}) {
		t.Fatalf("result=%v", result)
	}
}
