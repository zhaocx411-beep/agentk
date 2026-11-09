// Package httpcall 提供内部 workflow 使用的通用 HTTP Activities。
package httpcall

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
)

// maxResponseBytes 限制返回给模型的响应体大小。
const maxResponseBytes = 1 << 20

// statusError 暴露上游状态码，供 temporalx.ClassifyActivityError 分类重试策略：
// 语义 4xx 不重试，408、429 和 5xx 可重试。
type statusError struct {
	status int
	body   string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("status %d: %s", e.status, e.body)
}

func (e *statusError) StatusCode() int { return e.status }

// doJSONRequest 发送 JSON HTTP 请求。GET 和 DELETE 不带 body；幂等键非空时发送
// Idempotency-Key header；非 2xx 返回 statusError。
func doJSONRequest(
	ctx context.Context,
	client *http.Client,
	method, url string,
	body []byte,
	idempotencyKey string,
) ([]byte, error) {
	var reader io.Reader
	if method != http.MethodGet && method != http.MethodDelete && len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if reader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &statusError{status: resp.StatusCode, body: string(bytes.TrimSpace(raw))}
	}
	return raw, nil
}
