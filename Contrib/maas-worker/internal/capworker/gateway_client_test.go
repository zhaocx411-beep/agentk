package capworker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

var testGatewayURL = &url.URL{Scheme: "https", Host: "gateway.example.test"}

func TestGatewayClientInfer(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/v1/infer/transnet" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("Idempotency-Key") != "request-1" {
			t.Fatalf("idempotency key = %q", request.Header.Get("Idempotency-Key"))
		}
		var input gatewayRequest
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if input.RequestID != "request-1" || input.Features["video"] != "blob:key" {
			t.Fatalf("request = %+v", input)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"requestId":"request-1","route":"transnet","features":{"shots":2},"latencyMs":3}`)),
		}, nil
	})}

	client := newGatewayClient(testGatewayURL, "secret", httpClient)
	output, err := client.Infer(t.Context(), "transnet", "request-1", map[string]any{"video": "blob:key"})
	if err != nil {
		t.Fatal(err)
	}
	if output["shots"].(json.Number).String() != "2" {
		t.Fatalf("output = %#v", output)
	}
}

func TestGatewayClientClassifiesHTTPStatus(t *testing.T) {
	for _, test := range []struct {
		status    int
		code      string
		permanent bool
	}{
		{status: http.StatusBadRequest, code: "INVALID_REQUEST", permanent: true},
		{status: http.StatusRequestTimeout, code: "REQUEST_CANCELED", permanent: false},
		{status: http.StatusTooManyRequests, code: "UPSTREAM_RATE_LIMITED", permanent: false},
		{status: http.StatusBadGateway, code: "UPSTREAM_ERROR", permanent: true},
		{status: http.StatusServiceUnavailable, code: "UPSTREAM_UNAVAILABLE", permanent: false},
	} {
		t.Run(strconv.Itoa(test.status), func(t *testing.T) {
			httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: test.status,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(
						`{"error":{"code":"` + test.code + `","message":"failed"}}`)),
				}, nil
			})}
			client := newGatewayClient(testGatewayURL, "", httpClient)
			_, err := client.Infer(t.Context(), "transnet", "request-1", map[string]any{"video": "x"})
			var callError *CallError
			if !errors.As(err, &callError) || callError.Permanent != test.permanent {
				t.Fatalf("error = %#v", err)
			}
		})
	}
}

func TestGatewayClientNetworkFailureIsRetryable(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network unavailable")
	})}
	client := newGatewayClient(testGatewayURL, "", httpClient)
	_, err := client.Infer(context.Background(), "transnet", "request-1", map[string]any{})
	var callError *CallError
	if !errors.As(err, &callError) || callError.Permanent {
		t.Fatalf("error = %#v", err)
	}
}
