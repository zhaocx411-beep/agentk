package openai_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/chat"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/openai"
)

func TestFirstDeltaBeforeResponseCompletes(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	received := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"first\"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-received:
		case <-r.Context().Done():
			return
		case <-ctx.Done():
			return
		}
		_, _ = io.WriteString(
			w,
			"data: {\"choices\":[{\"delta\":{\"content\":\"last\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n",
		)
	}))
	defer srv.Close()
	defer cancel()
	resp, err := newTestClient(t, srv, nil).Stream(ctx, chat.Request{Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for delta, err := range resp.Stream {
		if err != nil {
			t.Fatal(err)
		}
		if text == "" {
			if delta.Text != "first" {
				t.Fatalf("first = %q", delta.Text)
			}
			close(received)
		}
		text += delta.Text
	}
	if text != "firstlast" {
		t.Fatalf("text = %q", text)
	}
}

type trackingBody struct {
	io.Reader
	closed bool
}

func (b *trackingBody) Close() error { b.closed = true; return nil }
func TestEarlyExitClosesBody(t *testing.T) {
	body := &trackingBody{Reader: strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"first\"}}]}\n\n")}
	client, err := openai.NewChat(
		route.New(
			gateway.MustURL("https://example.com"),
			nil,
			nil,
		),
		upstream.WithHTTPClient(&http.Client{
			Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				return response(
					r,
					body,
				), nil
			}),
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Stream(t.Context(), chat.Request{Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range resp.Stream {
		if err != nil {
			t.Fatal(err)
		}
		break
	}
	if !body.closed {
		t.Fatal("body not closed")
	}
	if _, err := resp.Result(); err == nil {
		t.Fatal("incomplete stream returned result")
	}
}
