// 各 Agent HTTP 服务共用的请求/响应工具与访问日志中间件。
package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
	"unicode/utf8"
)

const (
	// MaxRequestBody API 请求体上限。
	MaxRequestBody = 64 << 10
)

func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("api: encode response: %v", err)
	}
}

func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, map[string]string{"error": message})
}

// DecodeOneJSON 严格解码单个 JSON 值：未知字段与多余值都判为错误。
func DecodeOneJSON(body io.Reader, target any) error {
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func TruncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}

// AccessLog 每个请求记一行：方法、路径、状态码、耗时、来源。
// /healthz 探活太密，静默跳过。
func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("http: %s %s -> %d (%s) from %s",
			r.Method, r.URL.Path, rec.status,
			time.Since(start).Round(time.Millisecond), r.RemoteAddr)
	})
}

// statusRecorder 截获 WriteHeader 以便 access log 记录状态码。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Flush 透传给底层 ResponseWriter：SSE（/stream）依赖 http.Flusher，
// 中间件包装不能把这个能力吞掉。
func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap 供 http.ResponseController 找回底层 writer 的扩展能力。
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
