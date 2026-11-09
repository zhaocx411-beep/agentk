package authz

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"agentk/internal/types"
)

// ErrUnauthenticated 凭证缺失或无效；中间件译为 401。
var ErrUnauthenticated = errors.New("authz: unauthenticated")

// Verifier 从请求中校验凭证并解析出身份。失败须返回 ErrUnauthenticated
// （可包装细节，中间件用 errors.Is 判定），不区分"没带"和"带了但伪造"。
type Verifier interface {
	Verify(r *http.Request) (Principal, error)
}

// APIKeyStore 是服务凭证 verifier 对持久化层的最小依赖。
type APIKeyStore interface {
	AuthenticateAPIKey(ctx context.Context, prefix, rawKey string) (Principal, error)
}

// NewAPIKeyVerifier 校验 `Authorization: Bearer <API Key>`。缺头、格式不对、
// 凭证无效一律 401：agent-k 只认应用接入方，没有第二条身份通道可以回退。
func NewAPIKeyVerifier(store APIKeyStore) Verifier {
	return &apiKeyVerifier{store: store}
}

type apiKeyVerifier struct {
	store APIKeyStore
}

func (v *apiKeyVerifier) Verify(r *http.Request) (Principal, error) {
	parts := strings.Fields(strings.TrimSpace(r.Header.Get("Authorization")))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return Principal{}, fmt.Errorf("%w: malformed authorization header", ErrUnauthenticated)
	}
	prefix, err := parseAPIKey(parts[1])
	if err != nil {
		return Principal{}, err
	}
	p, err := v.store.AuthenticateAPIKey(r.Context(), prefix, parts[1])
	if err != nil {
		if errors.Is(err, types.ErrNotFound) {
			return Principal{}, fmt.Errorf("%w: invalid api key", ErrUnauthenticated)
		}
		return Principal{}, err
	}
	return p, nil
}

func parseAPIKey(raw string) (string, error) {
	parts := strings.Split(raw, "_")
	if len(parts) != 3 || parts[0] != "ak" || len(parts[1]) != 16 || len(parts[2]) != 64 {
		return "", fmt.Errorf("%w: malformed api key", ErrUnauthenticated)
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return "", fmt.Errorf("%w: malformed api key prefix", ErrUnauthenticated)
	}
	if _, err := hex.DecodeString(parts[2]); err != nil {
		return "", fmt.Errorf("%w: malformed api key secret", ErrUnauthenticated)
	}
	return parts[1], nil
}
