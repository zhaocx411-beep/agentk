package capcatalog

import (
	"context"
	"fmt"
	"sync"
	"time"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
)

type memoryManifest struct {
	record    ManifestRecord
	expiresAt time.Time
}

type memoryOwner struct {
	taskQueue string
	expiresAt time.Time
}

type Memory struct {
	mu        sync.Mutex
	now       func() time.Time
	ttl       time.Duration
	manifests map[string]memoryManifest
	owners    map[string]memoryOwner
}

func NewMemory() *Memory {
	return newMemory(time.Now, DefaultTTL)
}

func newMemory(now func() time.Time, ttl time.Duration) *Memory {
	return &Memory{
		now:       now,
		ttl:       ttl,
		manifests: make(map[string]memoryManifest),
		owners:    make(map[string]memoryOwner),
	}
}

var _ Catalog = (*Memory)(nil)

func (m *Memory) Register(
	_ context.Context,
	raw sdkcapability.ManifestV1,
) (ManifestRecord, error) {
	manifest, digest, err := NormalizeManifest(raw)
	if err != nil {
		return ManifestRecord{}, fmt.Errorf("capability catalog: normalize manifest: %w", err)
	}
	now := m.now().UTC()
	expiresAt := now.Add(m.ttl)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.purgeExpired(now)

	for _, def := range manifest.Capabilities {
		if current, ok := m.owners[def.ID]; ok && current.taskQueue != manifest.TaskQueue {
			return ManifestRecord{}, fmt.Errorf(
				"%w: capability %q is owned by task queue %q",
				ErrRefConflict, def.ID, current.taskQueue,
			)
		}
	}

	record := ManifestRecord{
		Manifest: manifest, Digest: digest, ReceivedAt: now,
	}
	m.manifests[manifest.TaskQueue] = memoryManifest{
		record: record, expiresAt: expiresAt,
	}
	for _, def := range manifest.Capabilities {
		m.owners[def.ID] = memoryOwner{
			taskQueue: manifest.TaskQueue, expiresAt: expiresAt,
		}
	}
	return record, nil
}

func (m *Memory) ResolveCapability(
	_ context.Context,
	capabilityID string,
) (ResolvedCapability, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, err := m.resolveRecord(capabilityID, m.now().UTC())
	if err != nil {
		return ResolvedCapability{}, err
	}
	return resolveCapability(record, capabilityID)
}

func (m *Memory) ResolveTool(
	ctx context.Context,
	capabilityID string,
) (ResolvedTool, error) {
	return resolveTool(ctx, capabilityID, m.ResolveCapability)
}

func (m *Memory) ListCapabilities(
	_ context.Context,
) ([]ResolvedCapability, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.purgeExpired(m.now().UTC())

	var out []ResolvedCapability
	for _, stored := range m.manifests {
		items, err := expandCapabilities(stored.record)
		if err != nil {
			return nil, fmt.Errorf("capability catalog: %w", err)
		}
		out = append(out, items...)
	}
	sortCapabilities(out)
	return out, nil
}

func (m *Memory) resolveRecord(
	capabilityID string,
	now time.Time,
) (ManifestRecord, error) {
	m.purgeExpired(now)
	owner, ok := m.owners[capabilityID]
	if !ok {
		return ManifestRecord{}, fmt.Errorf("%w: capability %q", ErrNotFound, capabilityID)
	}
	stored, ok := m.manifests[owner.taskQueue]
	if !ok {
		return ManifestRecord{}, fmt.Errorf("%w: manifest for capability %q", ErrNotFound, capabilityID)
	}
	return stored.record, nil
}

func (m *Memory) purgeExpired(now time.Time) {
	for key, stored := range m.manifests {
		if !stored.expiresAt.After(now) {
			delete(m.manifests, key)
		}
	}
	for key, owner := range m.owners {
		if !owner.expiresAt.After(now) {
			delete(m.owners, key)
		}
	}
}
