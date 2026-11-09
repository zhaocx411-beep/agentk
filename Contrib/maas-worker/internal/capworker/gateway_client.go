package capworker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const maxGatewayResponseBytes = int64(128 << 20)

// GatewayClient 通过 KNGX 暴露的 HTTPS URL 调用 PRT MaaS Gateway。
type GatewayClient struct {
	baseURL     *url.URL
	httpClient  *http.Client
	bearerToken string
}

type gatewayRequest struct {
	RequestID string         `json:"requestId"`
	Features  map[string]any `json:"features"`
}

type gatewayResponse struct {
	Features map[string]any `json:"features"`
}

type gatewayErrorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// CallError 保留 HTTP 状态和是否可重试，供 Temporal Activity 分类重试语义。
type CallError struct {
	StatusCode int
	Code       string
	Message    string
	Permanent  bool
	Cause      error
}

func (e *CallError) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("MaaS gateway returned HTTP %d (%s): %s", e.StatusCode, e.Code, e.Message)
	}
	return "call MaaS gateway: " + e.Message
}

func (e *CallError) Unwrap() error { return e.Cause }

func NewGatewayClient(baseURL *url.URL, bearerToken string, httpClient *http.Client) (*GatewayClient, error) {
	if baseURL == nil || baseURL.Host == "" {
		return nil, fmt.Errorf("MaaS gateway base URL is required")
	}
	return newGatewayClient(baseURL, bearerToken, httpClient), nil
}

func newGatewayClient(baseURL *url.URL, bearerToken string, httpClient *http.Client) *GatewayClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	copyURL := *baseURL
	return &GatewayClient{baseURL: &copyURL, httpClient: httpClient, bearerToken: bearerToken}
}

// Infer 使用 route 的 AgentK 输入作为 HTTP features，并只把响应 features 返回给 AgentK。
func (c *GatewayClient) Infer(
	ctx context.Context,
	route string,
	requestID string,
	features map[string]any,
) (map[string]any, error) {
	if features == nil {
		return nil, &CallError{Code: "INVALID_ARGUMENT", Message: "features must be a JSON object", Permanent: true}
	}
	payload, err := json.Marshal(gatewayRequest{RequestID: requestID, Features: features})
	if err != nil {
		return nil, &CallError{Code: "INVALID_ARGUMENT", Message: "encode features: " + err.Error(), Permanent: true, Cause: err}
	}
	endpoint := *c.baseURL
	endpoint.Path += "/v1/infer/" + url.PathEscape(route)
	endpoint.RawPath = ""
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, &CallError{Code: "INVALID_REQUEST", Message: err.Error(), Permanent: true, Cause: err}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", requestID)
	if c.bearerToken != "" {
		request.Header.Set("Authorization", "Bearer "+c.bearerToken)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, &CallError{Code: "HTTP_FAILED", Message: err.Error(), Cause: err}
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxGatewayResponseBytes+1))
	if err != nil {
		return nil, &CallError{Code: "READ_FAILED", Message: err.Error(), Cause: err}
	}
	if int64(len(raw)) > maxGatewayResponseBytes {
		return nil, &CallError{Code: "RESPONSE_TOO_LARGE", Message: "gateway response exceeds 128 MiB", Permanent: true}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var envelope gatewayErrorEnvelope
		_ = json.Unmarshal(raw, &envelope)
		code := envelope.Error.Code
		if code == "" {
			code = http.StatusText(response.StatusCode)
		}
		message := envelope.Error.Message
		if message == "" {
			message = string(bytes.TrimSpace(raw))
		}
		message = truncateMessage(message, 4096)
		return nil, &CallError{
			StatusCode: response.StatusCode,
			Code:       code,
			Message:    message,
			Permanent:  permanentGatewayError(response.StatusCode, code),
		}
	}

	var output gatewayResponse
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&output); err != nil {
		return nil, &CallError{Code: "INVALID_RESPONSE", Message: err.Error(), Permanent: true, Cause: err}
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return nil, &CallError{Code: "INVALID_RESPONSE", Message: err.Error(), Permanent: true, Cause: err}
	}
	if output.Features == nil {
		return nil, &CallError{Code: "INVALID_RESPONSE", Message: "gateway response is missing features", Permanent: true}
	}
	return output.Features, nil
}

func truncateMessage(message string, limit int) string {
	if len(message) <= limit {
		return message
	}
	return message[:limit] + "..."
}

func permanentGatewayError(statusCode int, code string) bool {
	switch code {
	case "UPSTREAM_ERROR", "UPSTREAM_REJECTED", "UPSTREAM_UNAUTHENTICATED",
		"UPSTREAM_PERMISSION_DENIED", "UPSTREAM_NOT_FOUND":
		return true
	}
	return statusCode >= 400 && statusCode < 500 &&
		statusCode != http.StatusRequestTimeout &&
		statusCode != http.StatusTooManyRequests
}

var _ error = (*CallError)(nil)
