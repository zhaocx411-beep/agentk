package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agentk/internal/types"
)

func TestServiceAPIKeyLifecycle(t *testing.T) {
	db := testDB(t)
	st := New(db)
	ctx := context.Background()

	org, err := st.CreateOrg(ctx, "evaluation-prod", "team")
	if err != nil {
		t.Fatal(err)
	}
	sp, err := st.CreateServicePrincipal(ctx, org.PublicID, "evaluation-platform")
	if err != nil {
		t.Fatal(err)
	}
	cred, err := st.CreateAPIKey(ctx, sp.PublicID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cred.Secret, "ak_"+cred.Prefix+"_") {
		t.Fatalf("unexpected key format: %q", cred.Secret)
	}

	var stored []byte
	if err := db.QueryRow(`SELECT secret_hash FROM ak_api_keys WHERE public_id = ?`, cred.PublicID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if string(stored) == cred.Secret || len(stored) != 32 {
		t.Fatalf("secret persisted incorrectly: len(hash)=%d", len(stored))
	}

	p, err := st.AuthenticateAPIKey(ctx, cred.Prefix, cred.Secret)
	if err != nil {
		t.Fatal(err)
	}
	if p.UserPub != sp.PublicID || p.TenantID != org.PublicID {
		t.Fatalf("principal = %+v", p)
	}
	if _, err := st.AuthenticateAPIKey(ctx, cred.Prefix, cred.Secret+"0"); !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("bad secret error = %v, want not found", err)
	}

	second, err := st.CreateAPIKey(ctx, sp.PublicID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RevokeAPIKey(ctx, cred.PublicID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AuthenticateAPIKey(ctx, cred.Prefix, cred.Secret); !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("revoked key error = %v, want not found", err)
	}
	if _, err := st.AuthenticateAPIKey(ctx, second.Prefix, second.Secret); err != nil {
		t.Fatalf("replacement key rejected: %v", err)
	}

	keys, err := st.ListAPIKeys(ctx, sp.PublicID)
	if err != nil || len(keys) != 2 {
		t.Fatalf("keys = %+v, err = %v", keys, err)
	}
	if keys[0].PublicID != second.PublicID || keys[1].Status != "revoked" {
		t.Fatalf("unexpected key list: %+v", keys)
	}

	if err := st.DisableServicePrincipal(ctx, sp.PublicID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AuthenticateAPIKey(ctx, second.Prefix, second.Secret); !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("disabled principal error = %v, want not found", err)
	}
}

func TestExpiredServiceAPIKeyRejected(t *testing.T) {
	st := New(testDB(t))
	ctx := context.Background()
	org, err := st.CreateOrg(ctx, "evaluation-staging", "team")
	if err != nil {
		t.Fatal(err)
	}
	sp, err := st.CreateServicePrincipal(ctx, org.PublicID, "evaluation-platform")
	if err != nil {
		t.Fatal(err)
	}
	// 过期点取足够久远：过期判定用的是 DB 的 NOW(3)，本地开发库常与主机存在分钟级时钟漂移
	past := time.Now().Add(-24 * time.Hour)
	cred, err := st.CreateAPIKey(ctx, sp.PublicID, &past)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AuthenticateAPIKey(ctx, cred.Prefix, cred.Secret); !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("expired key error = %v, want not found", err)
	}
}
