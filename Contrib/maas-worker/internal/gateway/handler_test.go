package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	inferpb "git.corp.kuaishou.com/klingai-agentk/contrib/maas-worker.git/gen/infer"
	"git.corp.kuaishou.com/klingai-agentk/contrib/maas-worker.git/internal/config"
)

type fakeInferer struct {
	request *inferpb.InferRequest
	service string
	lane    string
	result  *inferpb.InferResponse
	err     error
}

func (f *fakeInferer) Infer(_ context.Context, service, lane string, request *inferpb.InferRequest) (*inferpb.InferResponse, error) {
	f.service, f.lane, f.request = service, lane, request
	return f.result, f.err
}

func TestInferSuccess(t *testing.T) {
	upstream := &fakeInferer{result: &inferpb.InferResponse{Feature: map[string]*inferpb.ExFeature{
		"error_code": intFeature("error_code", 0),
		"shots":      stringFeature("shots", `[[0,1.2]]`),
	}}}
	handler := testHandler(upstream, "")
	request := httptest.NewRequest(http.MethodPost, "/v1/infer/transnet", bytes.NewBufferString(`{
  "requestId":"req-1",
  "features":{"video":"upload:demo:video.mp4","min_shot_duration":0.3}
}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	if upstream.service != "maas-transnet" || upstream.lane != "online" {
		t.Fatalf("target = %q lane = %q", upstream.service, upstream.lane)
	}
	if upstream.request.GetRequestId() != "req-1" || upstream.request.GetFeature()["start_time"].GetIntVal() == 0 {
		t.Fatalf("request = %+v", upstream.request)
	}
	var body inferResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.RequestID != "req-1" || body.Features["shots"] != `[[0,1.2]]` {
		t.Fatalf("body = %+v", body)
	}
}

func TestRouteDefaultFeaturesAreInjectedAndCanBeOverridden(t *testing.T) {
	upstream := &fakeInferer{result: &inferpb.InferResponse{Feature: map[string]*inferpb.ExFeature{}}}
	handler := New(upstream, Options{
		Routes: map[string]config.Route{"ffmpeg": {
			ServiceName: "maas-ffmpeg", TimeoutSeconds: 600,
			AllowedFeatures: []string{"op", "db", "table"},
			DefaultFeatures: map[string]json.RawMessage{
				"db": json.RawMessage(`"agentk"`), "table": json.RawMessage(`"artifacts"`),
			},
		}},
		MaxRequestSize: 1 << 20,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/infer/ffmpeg",
		bytes.NewBufferString(`{"features":{"op":"probe","table":"session-output"}}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	features := upstream.request.GetFeature()
	if features["db"].GetStrVal() != "agentk" ||
		features["table"].GetStrVal() != "session-output" {
		t.Fatalf("upstream features = %+v", features)
	}
}

func TestIdempotencyKeyBecomesRPCRequestID(t *testing.T) {
	upstream := &fakeInferer{result: &inferpb.InferResponse{Feature: map[string]*inferpb.ExFeature{}}}
	request := httptest.NewRequest(http.MethodPost, "/v1/infer/transnet", bytes.NewBufferString(`{"features":{"video":"x"}}`))
	request.Header.Set("Idempotency-Key", "activity-1")
	response := httptest.NewRecorder()
	testHandler(upstream, "").ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	if upstream.request.GetRequestId() != "activity-1" || response.Header().Get("Idempotency-Key") != "activity-1" {
		t.Fatalf("RPC request id = %q response key = %q", upstream.request.GetRequestId(), response.Header().Get("Idempotency-Key"))
	}
}

func TestRejectsMismatchedIdempotencyKey(t *testing.T) {
	upstream := &fakeInferer{}
	request := httptest.NewRequest(http.MethodPost, "/v1/infer/transnet", bytes.NewBufferString(`{"requestId":"body-1","features":{"video":"x"}}`))
	request.Header.Set("Idempotency-Key", "header-1")
	response := httptest.NewRecorder()
	testHandler(upstream, "").ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || upstream.request != nil {
		t.Fatalf("status = %d body = %s upstream = %+v", response.Code, response.Body.String(), upstream.request)
	}
}

func TestInferMapsBusinessError(t *testing.T) {
	upstream := &fakeInferer{result: &inferpb.InferResponse{Feature: map[string]*inferpb.ExFeature{
		"error_code":    intFeature("error_code", 17),
		"error_message": stringFeature("error_message", "bad media"),
	}}}
	request := httptest.NewRequest(http.MethodPost, "/v1/infer/transnet", bytes.NewBufferString(`{"features":{"video":"bad"}}`))
	response := httptest.NewRecorder()
	testHandler(upstream, "").ServeHTTP(response, request)

	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var body errorEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "UPSTREAM_ERROR" || body.Error.UpstreamCode != 17 {
		t.Fatalf("body = %+v", body)
	}
}

func TestBearerAuthentication(t *testing.T) {
	handler := testHandler(&fakeInferer{}, "secret")
	request := httptest.NewRequest(http.MethodGet, "/v1/routes", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/routes", nil)
	request.Header.Set("Authorization", "Bearer secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
}

func TestRequestSizeLimit(t *testing.T) {
	upstream := &fakeInferer{}
	handler := New(upstream, Options{
		Routes:         map[string]config.Route{"transnet": {ServiceName: "maas-transnet", TimeoutSeconds: 1}},
		MaxRequestSize: 10, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/infer/transnet", bytes.NewBufferString(`{"features":{"video":"far too large"}}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
}

func testHandler(inferer Inferer, token string) http.Handler {
	return New(inferer, Options{
		Routes: map[string]config.Route{"transnet": {
			ServiceName: "maas-transnet", TimeoutSeconds: 600, Lane: "online",
		}},
		MaxRequestSize: 1 << 20, BearerToken: token,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}
