package capcatalog

import (
	"context"
	"fmt"
	"strings"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
)

const coreTaskQueue = "agentk"

// Composite 把随 agent-k 发布的核心契约与 Worker 上报的 contrib 目录合成一个能力面。
// ID 前缀决定唯一数据源；核心契约不参与 TTL，contrib 仍由动态目录维护。
type Composite struct {
	core    ManifestRecord
	dynamic Catalog
}

func NewComposite(
	core sdkcapability.ManifestV1,
	dynamic Catalog,
) (*Composite, error) {
	if dynamic == nil {
		return nil, fmt.Errorf("dynamic capability catalog is required")
	}
	normalized, digest, err := NormalizeManifest(core)
	if err != nil {
		return nil, fmt.Errorf("normalize core capability manifest: %w", err)
	}
	if normalized.TaskQueue != coreTaskQueue {
		return nil, fmt.Errorf("core capability task queue must be %q", coreTaskQueue)
	}
	for _, def := range normalized.Capabilities {
		if !strings.HasPrefix(def.ID, "agentk.") {
			return nil, fmt.Errorf("core capability id %q must have prefix %q", def.ID, "agentk.")
		}
		if err := validateBlockingTool(def); err != nil {
			return nil, err
		}
	}
	return &Composite{
		core:    ManifestRecord{Manifest: normalized, Digest: digest},
		dynamic: dynamic,
	}, nil
}

func (c *Composite) Register(
	ctx context.Context,
	raw sdkcapability.ManifestV1,
) (ManifestRecord, error) {
	manifest, _, err := NormalizeManifest(raw)
	if err != nil {
		return ManifestRecord{}, fmt.Errorf("normalize contrib capability manifest: %w", err)
	}
	if err := ValidateContribManifest(manifest); err != nil {
		return ManifestRecord{}, err
	}
	return c.dynamic.Register(ctx, manifest)
}

// ValidateContribManifest 校验公开 announce 边界：核心 ID 与核心 queue 都不可由
// 外部 Manifest 声明。调用方须先完成协议 Normalize。
func ValidateContribManifest(manifest sdkcapability.ManifestV1) error {
	if manifest.TaskQueue == coreTaskQueue {
		return fmt.Errorf("contrib task queue %q is reserved", coreTaskQueue)
	}
	for _, def := range manifest.Capabilities {
		if !strings.HasPrefix(def.ID, "contrib.") || strings.Count(def.ID, ".") != 2 {
			return fmt.Errorf(
				"contrib capability id %q must match contrib.<owner>.<name>", def.ID)
		}
		if err := validateBlockingTool(def); err != nil {
			return err
		}
	}
	return nil
}

func (c *Composite) ResolveCapability(
	ctx context.Context,
	id string,
) (ResolvedCapability, error) {
	switch {
	case strings.HasPrefix(id, "agentk."):
		return resolveCapability(c.core, id)
	case strings.HasPrefix(id, "contrib."):
		resolved, err := c.dynamic.ResolveCapability(ctx, id)
		if err != nil {
			return ResolvedCapability{}, err
		}
		if err := validateResolvedContrib(resolved); err != nil {
			return ResolvedCapability{}, err
		}
		return resolved, nil
	default:
		return ResolvedCapability{}, fmt.Errorf("%w: capability %q", ErrNotFound, id)
	}
}

func (c *Composite) ResolveTool(
	ctx context.Context,
	capabilityID string,
) (ResolvedTool, error) {
	return resolveTool(ctx, capabilityID, c.ResolveCapability)
}

func (c *Composite) ListCapabilities(
	ctx context.Context,
) ([]ResolvedCapability, error) {
	dynamic, err := c.dynamic.ListCapabilities(ctx)
	if err != nil {
		return nil, err
	}
	for _, resolved := range dynamic {
		if err := validateResolvedContrib(resolved); err != nil {
			return nil, err
		}
	}
	core, err := expandCapabilities(c.core)
	if err != nil {
		return nil, err
	}
	out := append(core, dynamic...)
	sortCapabilities(out)
	return out, nil
}

func validateResolvedContrib(resolved ResolvedCapability) error {
	if resolved.TaskQueue == coreTaskQueue {
		return fmt.Errorf(
			"stored contrib capability %q uses reserved task queue %q",
			resolved.Capability.ID, coreTaskQueue)
	}
	if !strings.HasPrefix(resolved.Capability.ID, "contrib.") ||
		strings.Count(resolved.Capability.ID, ".") != 2 {
		return fmt.Errorf(
			"stored capability id %q must match contrib.<owner>.<name>",
			resolved.Capability.ID)
	}
	if err := validateBlockingTool(resolved.Capability); err != nil {
		return err
	}
	return nil
}

func validateBlockingTool(def sdkcapability.CapabilityDef) error {
	if def.Tool != nil && def.Tool.Mode != sdkcapability.ToolModeBlocking {
		return fmt.Errorf(
			"capability %q uses unsupported tool mode %q", def.ID, def.Tool.Mode)
	}
	return nil
}

var _ Catalog = (*Composite)(nil)
