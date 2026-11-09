package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	AuthModeDev           = "dev"
	AuthModeTrustedHeader = "trusted-header"
)

type AuthConfig struct {
	Mode       string
	UserHeader string
	DevUser    string
}

func AuthConfigFromEnv() (AuthConfig, error) {
	cfg := AuthConfig{
		Mode:       strings.ToLower(strings.TrimSpace(os.Getenv("AGENTCUT_AUTH_MODE"))),
		UserHeader: strings.TrimSpace(os.Getenv("AGENTCUT_TRUSTED_USER_HEADER")),
		DevUser:    "dev",
	}
	if cfg.Mode == "" {
		cfg.Mode = AuthModeDev
	}
	if cfg.UserHeader == "" {
		cfg.UserHeader = "X-AgentCut-User"
	}
	if cfg.Mode != AuthModeDev && cfg.Mode != AuthModeTrustedHeader {
		return AuthConfig{}, fmt.Errorf("AGENTCUT_AUTH_MODE must be dev or trusted-header")
	}
	if cfg.Mode == AuthModeDev {
		if rawDevUser, ok := os.LookupEnv("AGENTCUT_DEV_USER"); ok {
			cfg.DevUser = strings.TrimSpace(rawDevUser)
			if err := validateDevUser(cfg.DevUser); err != nil {
				return AuthConfig{}, err
			}
		}
	}
	return cfg, nil
}

func validateDevUser(value string) error {
	if value == "" {
		return fmt.Errorf("AGENTCUT_DEV_USER must not be empty")
	}
	if len(value) > 128 || !utf8.ValidString(value) {
		return fmt.Errorf("AGENTCUT_DEV_USER must be valid UTF-8 and at most 128 bytes")
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return fmt.Errorf("AGENTCUT_DEV_USER must not contain control characters")
		}
	}
	return nil
}

type userContextKey struct{}

func userFromContext(ctx context.Context) string {
	user, _ := ctx.Value(userContextKey{}).(string)
	return user
}

func (c AuthConfig) withDefaults() AuthConfig {
	if c.Mode == "" {
		c.Mode = AuthModeDev
	}
	if c.UserHeader == "" {
		c.UserHeader = "X-AgentCut-User"
	}
	if c.DevUser == "" {
		c.DevUser = "dev"
	}
	return c
}

func (c AuthConfig) middleware(next http.Handler) http.Handler {
	c = c.withDefaults()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || strings.HasPrefix(r.URL.Path, "/v1/shared/") {
			next.ServeHTTP(w, r)
			return
		}
		raw := strings.TrimSpace(r.Header.Get(c.UserHeader))
		user := raw
		switch c.Mode {
		case AuthModeDev:
			if user == "" {
				user = c.DevUser
			}
		case AuthModeTrustedHeader:
			if user == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]any{
					"error": "authenticated user header is required",
					"code":  "UNAUTHENTICATED",
				})
				return
			}
		default:
			writeError(w, http.StatusInternalServerError, "invalid authentication configuration")
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey{}, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
