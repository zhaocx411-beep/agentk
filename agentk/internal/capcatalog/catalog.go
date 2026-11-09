// Package capcatalog stores and resolves static core and announced contrib capabilities.
package capcatalog

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
)

const DefaultTTL = time.Hour

var (
	ErrNotFound    = errors.New("capability catalog entry not found")
	ErrRefConflict = errors.New("capability ref owned by another task queue")
	ErrBusy        = errors.New("capability catalog is busy")
)

type ManifestRecord struct {
	Manifest   sdkcapability.ManifestV1 `json:"manifest"`
	Digest     string                   `json:"digest"`
	ReceivedAt time.Time                `json:"receivedAt"`
}

type ResolvedCapability struct {
	TaskQueue      string
	Digest         string
	LastReportedAt time.Time
	Capability     sdkcapability.CapabilityDef
}

type ResolvedTool struct {
	ResolvedCapability
	Tool sdkcapability.ToolDecl
}

type Catalog interface {
	Register(
		ctx context.Context,
		manifest sdkcapability.ManifestV1,
	) (ManifestRecord, error)
	ResolveCapability(
		ctx context.Context,
		capabilityID string,
	) (ResolvedCapability, error)
	ResolveTool(ctx context.Context, capabilityID string) (ResolvedTool, error)
	// ListCapabilities 返回当前可见的全部能力，按 capability ID 升序。
	ListCapabilities(ctx context.Context) ([]ResolvedCapability, error)
}

type capabilityResolver func(
	context.Context,
	string,
) (ResolvedCapability, error)

func resolveTool(
	ctx context.Context,
	capabilityID string,
	resolve capabilityResolver,
) (ResolvedTool, error) {
	if err := sdkcapability.ValidateID(capabilityID); err != nil {
		return ResolvedTool{}, fmt.Errorf("invalid capability ID %q", capabilityID)
	}
	resolved, err := resolve(ctx, capabilityID)
	if err != nil {
		return ResolvedTool{}, err
	}
	if resolved.Capability.Tool == nil {
		return ResolvedTool{}, fmt.Errorf(
			"%w: capability %q is not a tool", ErrNotFound, capabilityID)
	}
	return ResolvedTool{
		ResolvedCapability: resolved,
		Tool:               *resolved.Capability.Tool,
	}, nil
}

func findCapability(
	manifest sdkcapability.ManifestV1,
	id string,
) (sdkcapability.CapabilityDef, bool) {
	for _, def := range manifest.Capabilities {
		if def.ID == id {
			return def, true
		}
	}
	return sdkcapability.CapabilityDef{}, false
}

func resolveCapability(
	record ManifestRecord,
	id string,
) (ResolvedCapability, error) {
	manifest, err := record.Manifest.Normalize()
	if err != nil {
		return ResolvedCapability{}, fmt.Errorf("normalize stored manifest: %w", err)
	}
	def, ok := findCapability(manifest, id)
	if !ok {
		return ResolvedCapability{}, fmt.Errorf("%w: capability %q", ErrNotFound, id)
	}
	return ResolvedCapability{
		TaskQueue:      manifest.TaskQueue,
		Digest:         record.Digest,
		LastReportedAt: record.ReceivedAt,
		Capability:     def,
	}, nil
}

func expandCapabilities(record ManifestRecord) ([]ResolvedCapability, error) {
	manifest, err := record.Manifest.Normalize()
	if err != nil {
		return nil, fmt.Errorf("normalize stored manifest: %w", err)
	}
	out := make([]ResolvedCapability, 0, len(manifest.Capabilities))
	for _, def := range manifest.Capabilities {
		out = append(out, ResolvedCapability{
			TaskQueue:      manifest.TaskQueue,
			Digest:         record.Digest,
			LastReportedAt: record.ReceivedAt,
			Capability:     def,
		})
	}
	return out, nil
}

func sortCapabilities(items []ResolvedCapability) {
	slices.SortFunc(items, func(a, b ResolvedCapability) int {
		return strings.Compare(a.Capability.ID, b.Capability.ID)
	})
}
