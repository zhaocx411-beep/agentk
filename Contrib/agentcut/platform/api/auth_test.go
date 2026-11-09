package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrustedHeaderAuthentication(t *testing.T) {
	s, _ := newTestServer()
	s.Auth = AuthConfig{
		Mode: AuthModeTrustedHeader, UserHeader: "X-Verified-User", DevUser: "ignored-dev",
	}
	h := s.Handler()

	rec := doJSON(t, h, http.MethodGet, "/v1/projects", nil)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "UNAUTHENTICATED")

	req := httptest.NewRequest(http.MethodGet, "/v1/projects", nil)
	req.Header.Set("X-AgentCut-User", "spoofed")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "untrusted legacy header must be ignored")

	req = httptest.NewRequest(http.MethodGet, "/v1/projects", nil)
	req.Header.Set("X-Verified-User", "verified-user")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestDevAuthenticationDefaultsUser(t *testing.T) {
	s, ft := newTestServer()
	h := s.Handler()
	projectID := createProject(t, h)
	require.Len(t, ft.projects["user-projects:dev"], 1)
	assert.Equal(t, projectID, ft.projects["user-projects:dev"][0].ProjectID)
}

func TestDevAuthenticationUsesConfiguredUserAndAllowsHeaderOverride(t *testing.T) {
	s, ft := newTestServer()
	s.Auth = AuthConfig{Mode: AuthModeDev, DevUser: "alice"}
	h := s.Handler()
	projectID := createProject(t, h)
	require.Len(t, ft.projects["user-projects:alice"], 1)
	assert.Equal(t, projectID, ft.projects["user-projects:alice"][0].ProjectID)

	req := httptest.NewRequest(
		http.MethodPost, "/v1/projects", strings.NewReader(`{"title":"Bob project"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AgentCut-User", "bob")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Len(t, ft.projects["user-projects:bob"], 1)
}

func TestAuthConfigFromEnv(t *testing.T) {
	t.Setenv("AGENTCUT_AUTH_MODE", "trusted-header")
	t.Setenv("AGENTCUT_TRUSTED_USER_HEADER", "X-Corp-User")
	t.Setenv("AGENTCUT_DEV_USER", "alice")
	cfg, err := AuthConfigFromEnv()
	require.NoError(t, err)
	assert.Equal(t, AuthModeTrustedHeader, cfg.Mode)
	assert.Equal(t, "X-Corp-User", cfg.UserHeader)
	assert.Equal(t, "dev", cfg.DevUser, "trusted-header must ignore AGENTCUT_DEV_USER")

	t.Setenv("AGENTCUT_DEV_USER", "invalid\nuser")
	cfg, err = AuthConfigFromEnv()
	require.NoError(t, err)
	assert.Equal(t, AuthModeTrustedHeader, cfg.Mode)

	t.Setenv("AGENTCUT_AUTH_MODE", "unknown")
	_, err = AuthConfigFromEnv()
	assert.Error(t, err)
}

func TestAuthConfigRejectsInvalidDevUser(t *testing.T) {
	for _, value := range []string{"   ", "alice\nadmin", strings.Repeat("x", 129)} {
		t.Setenv("AGENTCUT_DEV_USER", value)
		_, err := AuthConfigFromEnv()
		require.ErrorContains(t, err, "AGENTCUT_DEV_USER")
	}
}
