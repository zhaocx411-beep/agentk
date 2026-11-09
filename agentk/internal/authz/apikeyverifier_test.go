package authz

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"agentk/internal/types"
)

const testAPIKey = "ak_0011223344556677_00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

type fakeAPIKeyStore struct {
	key    string
	active bool
	calls  int
}

func (f *fakeAPIKeyStore) AuthenticateAPIKey(_ context.Context, prefix, rawKey string) (Principal, error) {
	f.calls++
	if !f.active || prefix != "0011223344556677" || rawKey != f.key {
		return Principal{}, types.ErrNotFound
	}
	return Principal{UserPub: "sp_eval", TenantID: "org_eval", Display: "evaluation"}, nil
}

func TestAPIKeyVerifierRejectsMissingAuthorization(t *testing.T) {
	keys := &fakeAPIKeyStore{key: testAPIKey, active: true}
	v := NewAPIKeyVerifier(keys)
	if _, err := v.Verify(httptest.NewRequest(http.MethodGet, "/v1/me", nil)); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want unauthenticated", err)
	}
	if keys.calls != 0 {
		t.Fatalf("missing credential must not hit the store: calls = %d", keys.calls)
	}
}

func TestAPIKeyVerifierRejectsMalformedHeader(t *testing.T) {
	v := NewAPIKeyVerifier(&fakeAPIKeyStore{})
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer not-an-agentk-key")
	if _, err := v.Verify(req); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want unauthenticated", err)
	}
}

// 每请求都查库，吊销立即生效——不缓存主体。
func TestServicePrincipalRevokedImmediately(t *testing.T) {
	keys := &fakeAPIKeyStore{key: testAPIKey, active: true}
	var got Principal
	var called int
	h := Middleware(echoPrincipal(&got, &called), NewAPIKeyVerifier(keys), nil)

	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
		req.Header.Set("Authorization", "Bearer "+testAPIKey)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	if rec := request(); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got.UserPub != "sp_eval" || got.TenantID != "org_eval" {
		t.Fatalf("principal = %+v", got)
	}

	keys.active = false
	if rec := request(); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status after revoke = %d, want 401", rec.Code)
	}
	if keys.calls != 2 {
		t.Fatalf("api key validations = %d, want one per request", keys.calls)
	}
}
