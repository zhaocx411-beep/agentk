package netguard

import (
	"context"
	"net/http"
	"testing"
)

func TestValidatePublicHTTPURLRejectsUnsafeTargets(t *testing.T) {
	for _, raw := range []string{
		"file:///etc/passwd",
		"http://127.0.0.1/a",
		"http://[::1]/a",
		"http://169.254.169.254/latest/meta-data",
		"http://user:pass@example.com/a",
	} {
		if _, err := ValidatePublicHTTPURL(context.Background(), raw); err == nil {
			t.Fatalf("URL %q should be rejected", raw)
		}
	}
}

func TestPublicRedirectPolicyRejectsPrivateRedirect(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/private", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := PublicRedirectPolicy(nil)(req, nil); err == nil {
		t.Fatal("private redirect should be rejected")
	}
}

func TestPublicDialContextRejectsPrivateAddress(t *testing.T) {
	if _, err := PublicDialContext(context.Background(), "tcp", "127.0.0.1:80"); err == nil {
		t.Fatal("private dial target should be rejected")
	}
}
