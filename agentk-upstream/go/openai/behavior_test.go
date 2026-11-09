package openai_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/chat"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/openai"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(r *http.Request, body io.ReadCloser) *http.Response {
	return &http.Response{
		StatusCode: 200,
		Header: http.Header{
			"Content-Type": []string{
				"text/event-stream",
			},
		},
		Body:    body,
		Request: r,
	}
}
func TestRequestModel(t *testing.T) {
	var models []string
	client, err := openai.NewChat(route.New(
		gateway.MustURL("https://example.com/v1"),
		nil,
		nil,
	), upstream.WithHTTPClient(&http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		var body struct{ Model string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		models = append(models, body.Model)
		return response(
			r,
			io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")),
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

func TestNewRejectsInvalidRoute(t *testing.T) {
	if _, err := openai.NewChat(gateway.Route{}); err == nil {
		t.Fatal("invalid route accepted")
	}
}
