package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkerHealthHandler(t *testing.T) {
	for _, path := range []string{"/healthz", "/readyz"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		workerHealthHandler().ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"role":"worker"`) {
			t.Fatalf("path = %s status = %d body = %s", path, response.Code, response.Body.String())
		}
	}
}

func TestWorkerHealthHandlerDoesNotExposeGatewayRoutes(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/infer/transnet", strings.NewReader(`{}`))
	response := httptest.NewRecorder()
	workerHealthHandler().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
}
