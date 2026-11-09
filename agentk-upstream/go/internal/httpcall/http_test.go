package httpcall_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/httpcall"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

type failedReader struct{ err error }

func (r failedReader) Read([]byte) (int, error) { return 0, r.err }
func TestDoErrors(t *testing.T) {
	sentinel := errors.New("connection failed")
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "transport", true: "cancel"}[cancel], func(t *testing.T) {
			ctx, stop := context.WithCancel(t.Context())
			defer stop()
			want := sentinel
			client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				if cancel {
					stop()
					return nil, r.Context().Err()
				}
				return nil, sentinel
			})}
			req, err := http.NewRequestWithContext(ctx, "POST", "https://provider.test", nil)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = httpcall.Do(client, req)
			if cancel {
				want = context.Canceled
			}
			if !errors.Is(err, want) {
				t.Fatalf("error=%v want=%v", err, want)
			}
		})
	}
}
func TestDoClosesBody(t *testing.T) {
	sentinel := errors.New("read interrupted")
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "read failure"}[fail], func(t *testing.T) {
			var reader io.Reader = strings.NewReader("response")
			if fail {
				reader = io.MultiReader(strings.NewReader("partial"), failedReader{sentinel})
			}
			body := &trackedBody{Reader: reader}
			client := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"X-Request-Id": []string{"id"}}, Body: body}, nil
			})}
			req, err := http.NewRequestWithContext(t.Context(), "GET", "https://provider.test", nil)
			if err != nil {
				t.Fatal(err)
			}
			raw, resp, err := httpcall.Do(client, req)
			if !body.closed || resp == nil || resp.StatusCode != 200 || resp.Header.Get("x-request-id") != "id" {
				t.Fatalf("closed=%v response=%+v", body.closed, resp)
			}
			if fail {
				if !errors.Is(err, sentinel) || string(raw) != "partial" {
					t.Fatalf("body=%q error=%v", raw, err)
				}
			} else {
				if err != nil || string(raw) != "response" {
					t.Fatalf("body=%q error=%v", raw, err)
				}
			}
		})
	}
}
