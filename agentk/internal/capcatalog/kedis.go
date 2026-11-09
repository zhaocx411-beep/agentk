package capcatalog

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"git.corp.kuaishou.com/infra/infra-framework-go.git/kedis"
	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
	"github.com/go-redis/redis/v7"
)

const (
	kedisKeyPrefix = "agentk:capcatalog:v2:"
	lockTTL        = 10 * time.Second
)

const releaseLockScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`

type Kedis struct {
	kd        *kedis.Kedis
	now       func() time.Time
	ttl       time.Duration
	keyPrefix string
}

func NewKedis(resourceName string) (*Kedis, error) {
	if strings.TrimSpace(resourceName) == "" {
		return nil, fmt.Errorf("capability catalog: kedis resource name is required")
	}
	kd, err := kedis.NewKedisWithOptions(resourceName, kedis.Options{
		DialTimeout:        3 * time.Second,
		ReadTimeout:        2 * time.Second,
		IdleTimeout:        time.Minute,
		IdleCheckFrequency: time.Minute,
		MinIdleConns:       2,
		PoolSize:           10,
	})
	if err != nil {
		return nil, fmt.Errorf(
			"capability catalog: init kedis resource %q: %w", resourceName, err)
	}
	return newKedis(kd, time.Now, DefaultTTL), nil
}

func newKedis(kd *kedis.Kedis, now func() time.Time, ttl time.Duration) *Kedis {
	return &Kedis{kd: kd, now: now, ttl: ttl, keyPrefix: kedisKeyPrefix}
}

var _ Catalog = (*Kedis)(nil)

func (c *Kedis) Register(
	ctx context.Context,
	raw sdkcapability.ManifestV1,
) (ManifestRecord, error) {
	manifest, digest, err := NormalizeManifest(raw)
	if err != nil {
		return ManifestRecord{}, fmt.Errorf("capability catalog: normalize manifest: %w", err)
	}
	unlock, err := c.lockCatalog(ctx)
	if err != nil {
		return ManifestRecord{}, err
	}
	defer unlock()

	for _, def := range manifest.Capabilities {
		current, found, err := c.get(c.ownerKey(def.ID))
		if err != nil {
			return ManifestRecord{}, fmt.Errorf("capability catalog: read owner %q: %w", def.ID, err)
		}
		if found && current != manifest.TaskQueue {
			return ManifestRecord{}, fmt.Errorf(
				"%w: capability %q is owned by task queue %q",
				ErrRefConflict, def.ID, current,
			)
		}
	}

	record := ManifestRecord{
		Manifest:   manifest,
		Digest:     digest,
		ReceivedAt: c.now().UTC(),
	}
	rawRecord, err := json.Marshal(record)
	if err != nil {
		return ManifestRecord{}, fmt.Errorf("capability catalog: marshal record: %w", err)
	}
	if err := c.kd.Set(c.manifestKey(manifest.TaskQueue), rawRecord, c.ttl).Err(); err != nil {
		return ManifestRecord{}, fmt.Errorf("capability catalog: write manifest: %w", err)
	}
	_, err = c.kd.Pipelined(func(pipe redis.Pipeliner) error {
		for _, def := range manifest.Capabilities {
			pipe.Set(c.ownerKey(def.ID), manifest.TaskQueue, c.ttl)
		}
		pipe.SAdd(c.queuesKey(), manifest.TaskQueue)
		pipe.Expire(c.queuesKey(), c.ttl)
		return nil
	})
	if err != nil {
		return ManifestRecord{}, fmt.Errorf("capability catalog: refresh owners: %w", err)
	}
	return record, nil
}

func (c *Kedis) ListCapabilities(
	_ context.Context,
) ([]ResolvedCapability, error) {
	queues, err := c.kd.SMembers(c.queuesKey()).Result()
	if err != nil {
		return nil, fmt.Errorf("capability catalog: read task queues: %w", err)
	}
	var out []ResolvedCapability
	for _, queue := range queues {
		raw, found, err := c.get(c.manifestKey(queue))
		if err != nil {
			return nil, fmt.Errorf("capability catalog: read manifest: %w", err)
		}
		// Manifest 先于集合成员过期是正常的；成员会随集合整体 TTL 消失。
		if !found {
			continue
		}
		var record ManifestRecord
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			return nil, fmt.Errorf("capability catalog: decode manifest: %w", err)
		}
		items, err := expandCapabilities(record)
		if err != nil {
			return nil, fmt.Errorf("capability catalog: %w", err)
		}
		out = append(out, items...)
	}
	sortCapabilities(out)
	return out, nil
}

func (c *Kedis) ResolveCapability(
	_ context.Context,
	capabilityID string,
) (ResolvedCapability, error) {
	queue, found, err := c.get(c.ownerKey(capabilityID))
	if err != nil {
		return ResolvedCapability{}, fmt.Errorf("capability catalog: read owner: %w", err)
	}
	if !found {
		return ResolvedCapability{}, fmt.Errorf("%w: capability %q", ErrNotFound, capabilityID)
	}
	raw, found, err := c.get(c.manifestKey(queue))
	if err != nil {
		return ResolvedCapability{}, fmt.Errorf("capability catalog: read manifest: %w", err)
	}
	if !found {
		return ResolvedCapability{}, fmt.Errorf(
			"%w: manifest for capability %q", ErrNotFound, capabilityID)
	}
	var record ManifestRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return ResolvedCapability{}, fmt.Errorf("capability catalog: decode manifest: %w", err)
	}
	return resolveCapability(record, capabilityID)
}

func (c *Kedis) ResolveTool(
	ctx context.Context,
	capabilityID string,
) (ResolvedTool, error) {
	return resolveTool(ctx, capabilityID, c.ResolveCapability)
}

func (c *Kedis) lockCatalog(ctx context.Context) (func(), error) {
	token, err := lockToken()
	if err != nil {
		return nil, fmt.Errorf("capability catalog: create lock token: %w", err)
	}
	key := c.catalogLockKey()
	locked, err := c.kd.SetNX(key, token, lockTTL).Result()
	if err != nil {
		return nil, fmt.Errorf("capability catalog: lock catalog: %w", err)
	}
	if !locked {
		return nil, fmt.Errorf("%w: catalog", ErrBusy)
	}
	return func() {
		// 即使调用 context 已取消也尝试释放；Lua 校验 token，不会删除后来者。
		_ = c.kd.Eval(releaseLockScript, []string{key}, token).Err()
	}, nil
}

func (c *Kedis) get(key string) (string, bool, error) {
	value, err := c.kd.Get(key).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func (c *Kedis) manifestKey(taskQueue string) string {
	return c.keyPrefix + "manifest:" + escapeKeyPart(taskQueue)
}

func (c *Kedis) ownerKey(capabilityID string) string {
	return c.keyPrefix + "owner:" + escapeKeyPart(capabilityID)
}

func (c *Kedis) queuesKey() string {
	return c.keyPrefix + "queues"
}

func (c *Kedis) catalogLockKey() string {
	return c.keyPrefix + "lock"
}

func escapeKeyPart(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func lockToken() (string, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(token[:]), nil
}
