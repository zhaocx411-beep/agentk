package httpcall

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
)

type Response struct {
	StatusCode int
	Header     http.Header
}

func JSON(ctx context.Context, c *http.Client, method, target string, body any) ([]byte, *Response, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return Do(c, req)
}
func Do(c *http.Client, req *http.Request) ([]byte, *Response, error) {
	resp, err := c.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	return raw, &Response{StatusCode: resp.StatusCode, Header: resp.Header}, err
}
func Error(resp *Response, code, message string) *upstream.APIError {
	return &upstream.APIError{StatusCode: resp.StatusCode, Code: code, Message: message, RequestID: RequestID(resp)}
}
func RequestID(resp *Response) string {
	for _, key := range []string{"x-request-id", "request-id", "apim-request-id"} {
		if id := resp.Header.Get(key); id != "" {
			return id
		}
	}
	return ""
}
func Snippet(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}
