package audit

import (
	"context"
	"testing"

	"agentk/internal/authz"
)

func TestServicePrincipalAuditAttribution(t *testing.T) {
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{
		UserPub: "sp_eval", TenantID: "org_eval",
	})
	entry := entryFor(ctx, "session.create", "ses_1")
	if entry.ActorKind != "service" || entry.ServicePrincipal != "sp_eval" || entry.Tenant != "org_eval" {
		t.Fatalf("entry = %+v", entry)
	}
}
