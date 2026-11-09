package authz

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
)

// Middleware 身份中间件：校验凭证 → principal 注入 context。
// allow 里的路径（精确匹配，如 /healthz）免鉴权直放。
func Middleware(next http.Handler, v Verifier, allow []string) http.Handler {
	allowSet := make(map[string]struct{}, len(allow))
	for _, p := range allow {
		allowSet[p] = struct{}{}
	}
	return &middleware{next: next, verifier: v, allow: allowSet}
}

type middleware struct {
	next     http.Handler
	verifier Verifier
	allow    map[string]struct{}
}

func (m *middleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, ok := m.allow[r.URL.Path]; ok {
		m.next.ServeHTTP(w, r)
		return
	}
	p, err := m.verifier.Verify(r)
	if err != nil {
		if !errors.Is(err, ErrUnauthenticated) {
			// 非鉴权类错误（凭证库故障等）也一律 401，不泄露内部状态；仅日志留痕
			log.Printf("authz: verify: %v", err)
		}
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	m.next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
