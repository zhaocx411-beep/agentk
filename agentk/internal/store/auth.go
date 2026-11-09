package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"agentk/internal/authz"
	"agentk/internal/types"
)

// Org 是运维管理面使用的租户投影。
type Org struct {
	ID        int64     `json:"-"`
	PublicID  string    `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

// ServicePrincipal 是绑定单个租户的机器身份。
type ServicePrincipal struct {
	ID        int64     `json:"-"`
	PublicID  string    `json:"id"`
	TenantID  string    `json:"tenantId"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

// APIKey 是可列举的凭证元数据，不包含 secret。
type APIKey struct {
	ID          int64      `json:"-"`
	PublicID    string     `json:"id"`
	PrincipalID string     `json:"servicePrincipal"`
	Prefix      string     `json:"prefix"`
	Status      string     `json:"status"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// APIKeyCredential 仅由创建操作返回；Secret 不会再次读取或落库。
type APIKeyCredential struct {
	APIKey
	Secret string `json:"secret"`
}

func (s *Store) CreateOrg(ctx context.Context, name, kind string) (Org, error) {
	if name == "" {
		return Org{}, errors.New("create org: name is required")
	}
	if kind != "team" && kind != "enterprise" {
		return Org{}, fmt.Errorf("create org: kind must be team or enterprise, got %q", kind)
	}
	publicID := newPublicID("org_")
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO ak_orgs (public_id, name, kind, status) VALUES (?, ?, ?, 'active')`,
		publicID, name, kind)
	if err != nil {
		return Org{}, fmt.Errorf("create org: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Org{}, fmt.Errorf("create org: last insert id: %w", err)
	}
	return s.getOrg(ctx, id)
}

func (s *Store) getOrg(ctx context.Context, id int64) (Org, error) {
	var org Org
	err := s.db.QueryRowContext(ctx,
		`SELECT id, public_id, name, kind, status, created_at FROM ak_orgs WHERE id = ?`, id).
		Scan(&org.ID, &org.PublicID, &org.Name, &org.Kind, &org.Status, &org.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Org{}, types.ErrNotFound
	}
	if err != nil {
		return Org{}, err
	}
	return org, nil
}

func (s *Store) CreateServicePrincipal(ctx context.Context, orgPublicID, name string) (ServicePrincipal, error) {
	if orgPublicID == "" || name == "" {
		return ServicePrincipal{}, errors.New("create service principal: org and name are required")
	}
	publicID := newPublicID("sp_")
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO ak_service_principals (public_id, org_id, tenant_id, name)
		 SELECT ?, id, public_id, ? FROM ak_orgs
		 WHERE public_id = ? AND status = 'active'`,
		publicID, name, orgPublicID)
	if err != nil {
		return ServicePrincipal{}, fmt.Errorf("create service principal: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return ServicePrincipal{}, err
	}
	if affected != 1 {
		return ServicePrincipal{}, fmt.Errorf("create service principal: org %s: %w", orgPublicID, types.ErrNotFound)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ServicePrincipal{}, err
	}
	return s.getServicePrincipal(ctx, id)
}

func (s *Store) getServicePrincipal(ctx context.Context, id int64) (ServicePrincipal, error) {
	var sp ServicePrincipal
	err := s.db.QueryRowContext(ctx,
		`SELECT id, public_id, tenant_id, name, status, created_at
		 FROM ak_service_principals WHERE id = ?`, id).
		Scan(&sp.ID, &sp.PublicID, &sp.TenantID, &sp.Name, &sp.Status, &sp.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ServicePrincipal{}, types.ErrNotFound
	}
	if err != nil {
		return ServicePrincipal{}, err
	}
	return sp, nil
}

func (s *Store) DisableServicePrincipal(ctx context.Context, publicID string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE ak_service_principals SET status = 'disabled' WHERE public_id = ? AND status = 'active'`,
		publicID)
	if err != nil {
		return fmt.Errorf("disable service principal: %w", err)
	}
	return requireOneRow(res, "disable service principal")
}

func (s *Store) CreateAPIKey(ctx context.Context, principalPublicID string, expiresAt *time.Time) (APIKeyCredential, error) {
	prefix, err := randomHex(8)
	if err != nil {
		return APIKeyCredential{}, fmt.Errorf("create api key prefix: %w", err)
	}
	secretPart, err := randomHex(32)
	if err != nil {
		return APIKeyCredential{}, fmt.Errorf("create api key secret: %w", err)
	}
	rawKey := "ak_" + prefix + "_" + secretPart
	digest := apiKeyDigest(rawKey)
	publicID := newPublicID("key_")

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO ak_api_keys (public_id, service_principal_id, key_prefix, secret_hash, expires_at)
		 SELECT ?, sp.id, ?, ?, ?
		 FROM ak_service_principals sp
		 JOIN ak_orgs o ON o.id = sp.org_id
		 WHERE sp.public_id = ? AND sp.status = 'active' AND o.status = 'active'`,
		publicID, prefix, digest, expiresAt, principalPublicID)
	if err != nil {
		return APIKeyCredential{}, fmt.Errorf("create api key: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return APIKeyCredential{}, err
	}
	if affected != 1 {
		return APIKeyCredential{}, fmt.Errorf("create api key: service principal %s: %w", principalPublicID, types.ErrNotFound)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return APIKeyCredential{}, err
	}
	key, err := s.getAPIKey(ctx, id)
	if err != nil {
		return APIKeyCredential{}, err
	}
	return APIKeyCredential{APIKey: key, Secret: rawKey}, nil
}

func (s *Store) getAPIKey(ctx context.Context, id int64) (APIKey, error) {
	var key APIKey
	var expiresAt, lastUsedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT k.id, k.public_id, sp.public_id, k.key_prefix, k.status,
		        k.expires_at, k.last_used_at, k.created_at
		 FROM ak_api_keys k
		 JOIN ak_service_principals sp ON sp.id = k.service_principal_id
		 WHERE k.id = ?`, id).
		Scan(&key.ID, &key.PublicID, &key.PrincipalID, &key.Prefix, &key.Status,
			&expiresAt, &lastUsedAt, &key.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return APIKey{}, types.ErrNotFound
	}
	if err != nil {
		return APIKey{}, err
	}
	key.ExpiresAt = nullableTime(expiresAt)
	key.LastUsedAt = nullableTime(lastUsedAt)
	return key, nil
}

func (s *Store) RevokeAPIKey(ctx context.Context, publicID string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE ak_api_keys
		 SET status = 'revoked', revoked_at = NOW(3)
		 WHERE public_id = ? AND status = 'active'`, publicID)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	return requireOneRow(res, "revoke api key")
}

func (s *Store) ListAPIKeys(ctx context.Context, principalPublicID string) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT k.id, k.public_id, sp.public_id, k.key_prefix, k.status,
		        k.expires_at, k.last_used_at, k.created_at
		 FROM ak_api_keys k
		 JOIN ak_service_principals sp ON sp.id = k.service_principal_id
		 WHERE sp.public_id = ?
		 ORDER BY k.id DESC`, principalPublicID)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var keys []APIKey
	for rows.Next() {
		var key APIKey
		var expiresAt, lastUsedAt sql.NullTime
		if err := rows.Scan(&key.ID, &key.PublicID, &key.PrincipalID, &key.Prefix, &key.Status,
			&expiresAt, &lastUsedAt, &key.CreatedAt); err != nil {
			return nil, err
		}
		key.ExpiresAt = nullableTime(expiresAt)
		key.LastUsedAt = nullableTime(lastUsedAt)
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// AuthenticateAPIKey 每请求执行，确保吊销与禁用立即生效。
func (s *Store) AuthenticateAPIKey(ctx context.Context, prefix, rawKey string) (authz.Principal, error) {
	var p authz.Principal
	var storedHash []byte
	var keyID int64
	err := s.db.QueryRowContext(ctx,
		`SELECT k.id, k.secret_hash, sp.public_id, sp.tenant_id, sp.name
		 FROM ak_api_keys k
		 JOIN ak_service_principals sp ON sp.id = k.service_principal_id
		 JOIN ak_orgs o ON o.id = sp.org_id
		 WHERE k.key_prefix = ?
		   AND k.status = 'active'
		   AND (k.expires_at IS NULL OR k.expires_at > NOW(3))
		   AND sp.status = 'active'
		   AND o.status = 'active'
		 LIMIT 1`, prefix).
		Scan(&keyID, &storedHash, &p.UserPub, &p.TenantID, &p.Display)
	if errors.Is(err, sql.ErrNoRows) {
		return authz.Principal{}, types.ErrNotFound
	}
	if err != nil {
		return authz.Principal{}, fmt.Errorf("authenticate api key: %w", err)
	}
	digest := apiKeyDigest(rawKey)
	if len(storedHash) != len(digest) || subtle.ConstantTimeCompare(storedHash, digest) != 1 {
		return authz.Principal{}, types.ErrNotFound
	}
	// 最近使用时间只在至少一分钟未更新时落库，避免每请求都制造行写入。
	_, _ = s.db.ExecContext(ctx,
		`UPDATE ak_api_keys SET last_used_at = NOW(3)
		 WHERE id = ? AND (last_used_at IS NULL OR last_used_at < NOW(3) - INTERVAL 1 MINUTE)`, keyID)
	return p, nil
}

func apiKeyDigest(rawKey string) []byte {
	digest := sha256.Sum256([]byte(rawKey))
	return digest[:]
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func nullableTime(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
}

func requireOneRow(res sql.Result, action string) error {
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("%s: %w", action, types.ErrNotFound)
	}
	return nil
}
