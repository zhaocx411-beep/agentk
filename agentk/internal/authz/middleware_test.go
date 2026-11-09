package authz

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

/* ---------- fakes ---------- */

type fakeVerifier struct {
	p   Principal
	err error
}

func (f fakeVerifier) Verify(*http.Request) (Principal, error) {
	return f.p, f.err
}

/* ---------- helpers ---------- */

// echoPrincipal 记录进到下游 handler 的 principal。
func echoPrincipal(got *Principal, called *int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*called++
		if p, ok := FromContext(r.Context()); ok {
			*got = p
		}
		w.WriteHeader(http.StatusOK)
	})
}

func do(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

/* ---------- tests ---------- */

func TestMiddlewareUnauthenticated(t *testing.T) {
	var called int
	var p Principal
	h := Middleware(echoPrincipal(&p, &called), fakeVerifier{err: ErrUnauthenticated}, nil)

	rec := do(h, "/v1/sessions")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if called != 0 {
		t.Fatal("next handler must not be called")
	}
}

// 凭证校验的基建故障（非 ErrUnauthenticated 的错误）同样 401，不放行。
func TestMiddlewareVerifierFailureClosed(t *testing.T) {
	var called int
	var p Principal
	h := Middleware(echoPrincipal(&p, &called), fakeVerifier{err: fmt.Errorf("credential store down")}, nil)

	if rec := do(h, "/v1/sessions"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if called != 0 {
		t.Fatal("next handler must not be called")
	}
}

func TestMiddlewareAllowlist(t *testing.T) {
	var called int
	var p Principal
	h := Middleware(echoPrincipal(&p, &called), fakeVerifier{err: ErrUnauthenticated}, []string{"/healthz"})

	if rec := do(h, "/healthz"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if called != 1 {
		t.Fatal("allowlisted path must reach next handler")
	}
}

func TestMiddlewareInjectsPrincipal(t *testing.T) {
	want := Principal{UserPub: "sp_eval", TenantID: "org_eval", Display: "evaluation"}
	var called int
	var got Principal
	h := Middleware(echoPrincipal(&got, &called), fakeVerifier{p: want}, nil)

	if rec := do(h, "/v1/sessions"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got != want {
		t.Fatalf("principal = %+v, want %+v", got, want)
	}
}
