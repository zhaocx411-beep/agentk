// agentk-admin 管理组织、Service Principal 和 API Key；API Key 明文只在创建成功时输出一次。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"os"
	"time"

	"agentk/internal/audit"
	"agentk/internal/store"
	"agentk/internal/storeboot"
)

type adminStore interface {
	CreateOrg(context.Context, string, string) (store.Org, error)
	CreateServicePrincipal(context.Context, string, string) (store.ServicePrincipal, error)
	DisableServicePrincipal(context.Context, string) error
	CreateAPIKey(context.Context, string, *time.Time) (store.APIKeyCredential, error)
	RevokeAPIKey(context.Context, string) error
	ListAPIKeys(context.Context, string) ([]store.APIKey, error)
}

func main() {
	// runMain 返回前完成资源清理，再由 main 转换为非零退出状态。
	if err := runMain(); err != nil {
		log.Fatal(err)
	}
}

func runMain() error {
	ctx := context.Background()
	// 管理命令只检查 schema 版本；数据库迁移由 agentk-migrate 执行。
	st, closeDB, err := storeboot.Open(ctx, envOr("AGENTK_STORE", "mysql"), false)
	if err != nil {
		return err
	}
	defer closeDB()
	return run(ctx, os.Args[1:], st, os.Stdout)
}

func run(ctx context.Context, args []string, st adminStore, out io.Writer) error {
	if len(args) < 2 {
		return usageError()
	}
	switch args[0] + " " + args[1] {
	case "org create":
		fs := newFlagSet("org create")
		name := fs.String("name", "", "organization name")
		kind := fs.String("kind", "team", "team or enterprise")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		org, err := st.CreateOrg(ctx, *name, *kind)
		if err != nil {
			return err
		}
		audit.RecordOperator("org.create", org.PublicID)
		return writeJSON(out, org)

	case "service-principal create":
		fs := newFlagSet("service-principal create")
		org := fs.String("org", "", "org public id")
		name := fs.String("name", "", "service principal name")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		sp, err := st.CreateServicePrincipal(ctx, *org, *name)
		if err != nil {
			return err
		}
		audit.RecordOperator("service-principal.create", sp.PublicID)
		return writeJSON(out, sp)

	case "service-principal disable":
		fs := newFlagSet("service-principal disable")
		id := fs.String("id", "", "service principal public id")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if *id == "" {
			return errors.New("--id is required")
		}
		if err := st.DisableServicePrincipal(ctx, *id); err != nil {
			return err
		}
		audit.RecordOperator("service-principal.disable", *id)
		return writeJSON(out, map[string]string{"id": *id, "status": "disabled"})

	case "api-key create":
		fs := newFlagSet("api-key create")
		sp := fs.String("service-principal", "", "service principal public id")
		expiresIn := fs.Duration("expires-in", 0, "optional validity duration")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if *sp == "" {
			return errors.New("--service-principal is required")
		}
		var expiresAt *time.Time
		if *expiresIn < 0 {
			return errors.New("--expires-in must not be negative")
		}
		if *expiresIn > 0 {
			t := time.Now().Add(*expiresIn)
			expiresAt = &t
		}
		key, err := st.CreateAPIKey(ctx, *sp, expiresAt)
		if err != nil {
			return err
		}
		audit.RecordOperator("api-key.create", key.PublicID)
		return writeJSON(out, struct {
			ID               string     `json:"id"`
			ServicePrincipal string     `json:"servicePrincipal"`
			Secret           string     `json:"secret"`
			ExpiresAt        *time.Time `json:"expiresAt,omitempty"`
		}{key.PublicID, key.PrincipalID, key.Secret, key.ExpiresAt})

	case "api-key revoke":
		fs := newFlagSet("api-key revoke")
		id := fs.String("id", "", "api key public id")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if *id == "" {
			return errors.New("--id is required")
		}
		if err := st.RevokeAPIKey(ctx, *id); err != nil {
			return err
		}
		audit.RecordOperator("api-key.revoke", *id)
		return writeJSON(out, map[string]string{"id": *id, "status": "revoked"})

	case "api-key list":
		fs := newFlagSet("api-key list")
		sp := fs.String("service-principal", "", "service principal public id")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if *sp == "" {
			return errors.New("--service-principal is required")
		}
		keys, err := st.ListAPIKeys(ctx, *sp)
		if err != nil {
			return err
		}
		return writeJSON(out, keys)
	default:
		return usageError()
	}
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func usageError() error {
	return errors.New("usage: agentk-admin {org create|service-principal create|service-principal disable|api-key create|api-key revoke|api-key list} [flags]")
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
