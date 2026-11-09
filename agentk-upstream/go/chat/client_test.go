package chat_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/chat"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gemini"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/openai"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type failingBody struct {
	err    error
	closed bool
}

func (b *failingBody) Read([]byte) (int, error) { return 0, b.err }
func (b *failingBody) Close() error             { b.closed = true; return nil }
func response(r *http.Request, body io.ReadCloser) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body, Request: r}
}
func TestTransportErrorChains(t *testing.T) {
	constructors := map[string]func(gateway.Route, ...upstream.Option) (chat.Client, error){
		"openai": func(r gateway.Route, opts ...upstream.Option) (chat.Client, error) { return openai.NewChat(r, opts...) },
		"gemini": func(r gateway.Route, opts ...upstream.Option) (chat.Client, error) { return gemini.NewChat(r, opts...) },
	}
	for name, newClient := range constructors {
		t.Run(name, func(t *testing.T) {

			for _, kind := range []string{"cancel", "read"} {
				t.Run(kind, func(t *testing.T) {
					sentinel := errors.New("read interrupted")
					body := &failingBody{err: sentinel}
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					client, err := newClient(route.New(
						gateway.MustURL("https://example.com"),
						nil,
						nil,
					), upstream.WithHTTPClient(&http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
						if kind == "cancel" {
							cancel()
							return nil, r.Context().Err()
						}
						return response(r, body), nil
					})}))
					if err != nil {
						t.Fatal(err)
					}
					resp, err := client.Stream(ctx, chat.Request{Model: "model"})
					if err == nil {
						for _, streamErr := range resp.Stream {
							if streamErr != nil {
								err = streamErr
							}
						}
					}
					if kind == "cancel" {
						sentinel = context.Canceled
					} else if !body.closed {
						t.Error("response body not closed")
					}
					if !errors.Is(err, sentinel) {
						t.Fatalf("error = %v, want %v", err, sentinel)
					}
				})
			}

		})
	}
}
