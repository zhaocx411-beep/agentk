package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"agentk/internal/store"
)

type fakeAdminStore struct {
	createdSP string
	revoked   string
}

func (f *fakeAdminStore) CreateOrg(context.Context, string, string) (store.Org, error) {
	return store.Org{PublicID: "org_test"}, nil
}
func (f *fakeAdminStore) CreateServicePrincipal(_ context.Context, org, _ string) (store.ServicePrincipal, error) {
	return store.ServicePrincipal{PublicID: "sp_test", TenantID: org}, nil
}
func (f *fakeAdminStore) DisableServicePrincipal(context.Context, string) error { return nil }
func (f *fakeAdminStore) CreateAPIKey(_ context.Context, sp string, _ *time.Time) (store.APIKeyCredential, error) {
	f.createdSP = sp
	return store.APIKeyCredential{
		APIKey: store.APIKey{PublicID: "key_test", PrincipalID: sp, Prefix: "0011223344556677"},
		Secret: "ak_0011223344556677_secret",
	}, nil
}
func (f *fakeAdminStore) RevokeAPIKey(_ context.Context, id string) error {
	f.revoked = id
	return nil
}
func (f *fakeAdminStore) ListAPIKeys(context.Context, string) ([]store.APIKey, error) {
	return []store.APIKey{{PublicID: "key_test", Prefix: "0011223344556677"}}, nil
}

func TestAPIKeyCreateOutputsSecretOnce(t *testing.T) {
	st := &fakeAdminStore{}
	var out bytes.Buffer
	err := run(context.Background(),
		[]string{"api-key", "create", "--service-principal", "sp_eval"},
		st, &out)
	if err != nil {
		t.Fatal(err)
	}
	if st.createdSP != "sp_eval" || strings.Count(out.String(), "ak_0011223344556677_secret") != 1 {
		t.Fatalf("output = %s", out.String())
	}

	out.Reset()
	if err := run(context.Background(),
		[]string{"api-key", "list", "--service-principal", "sp_eval"},
		st, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "secret") {
		t.Fatalf("list leaked secret: %s", out.String())
	}
}