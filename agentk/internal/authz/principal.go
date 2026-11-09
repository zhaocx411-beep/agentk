// Package authz 是接入层的身份中间件：校验应用接入方的 API Key，
// 把 principal 注入 request context。agent-k 只认接入方这一层身份，
// 下游应用的终端用户由下游应用自己管理。
package authz

import (
	"context"
	"net/http"
)

// Principal 是解析完成的请求身份：应用接入方（服务主体）+ 它绑定的租户。
type Principal struct {
	// UserPub 服务主体 public id（sp_ 前缀）
	UserPub string
	// TenantID 主体绑定的 org public_id（org_ 前缀），即业务表 tenant_id 的取值
	TenantID string
	// Display 展示名，不作任何 id
	Display string
}

type ctxKey struct{}

// WithPrincipal 把 principal 注入 context（仅中间件调用）。
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// FromContext 取出请求身份。经过 Middleware 的请求必然存在；
// ok=false 说明调用路径没挂中间件（编程错误，调用方应拒绝请求）。
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}

// WithDevPrincipal 给所有请求注入固定的 dev principal。
// 仅供无鉴权模式（AGENTK_AUTH=off）与测试使用：不校验、不落库，
// 但让下游"从 context 取租户"的代码路径与生产一致。
func WithDevPrincipal(next http.Handler) http.Handler {
	dev := Principal{UserPub: "sp_dev", TenantID: "org_dev", Display: "dev"}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), dev)))
	})
}
