package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/google/jsonschema-go/jsonschema"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"

	"agentk/internal/capcatalog"
)

var (
	// ErrUnavailable 表示 Profile 已暴露 Pipeline，但当前 Catalog 无法提供它的契约。
	ErrUnavailable = errors.New("pipeline capability is unavailable")
	// ErrInvalidArguments 表示调用参数不符合 Pipeline 发布的 InputSchema。
	ErrInvalidArguments = errors.New("invalid pipeline arguments")
)

type resolvedEntry struct {
	item   ListItem
	target Target
}

func resolveEntry(
	ctx context.Context,
	catalog capcatalog.Catalog,
	entry ProfileEntry,
) (resolvedEntry, error) {
	if catalog == nil {
		return resolvedEntry{}, fmt.Errorf("%w: catalog is required", ErrUnavailable)
	}
	resolved, err := catalog.ResolveCapability(ctx, entry.Capability)
	if err != nil {
		return resolvedEntry{}, err
	}
	var kind Kind
	switch resolved.Capability.Kind {
	case sdkcapability.CapabilityKindWorkflow:
		kind = KindWorkflow
	case sdkcapability.CapabilityKindActivity:
		kind = KindActivity
	default:
		return resolvedEntry{}, fmt.Errorf(
			"%w: capability %q has unsupported kind %q",
			ErrUnavailable, entry.Capability, resolved.Capability.Kind)
	}
	if resolved.Capability.TimeoutSeconds > int64(maxTimeoutSeconds) {
		return resolvedEntry{}, fmt.Errorf(
			"%w: capability %q timeout exceeds %d seconds",
			ErrUnavailable, entry.Capability, maxTimeoutSeconds)
	}
	target := Target{
		Kind:           kind,
		Type:           resolved.Capability.TemporalType,
		TaskQueue:      resolved.TaskQueue,
		TimeoutSeconds: int(resolved.Capability.TimeoutSeconds),
	}
	if err := target.Validate(); err != nil {
		return resolvedEntry{}, fmt.Errorf(
			"%w: capability %q has invalid target: %v",
			ErrUnavailable, entry.Capability, err)
	}
	return resolvedEntry{
		target: target,
		item: ListItem{
			CapabilityID: entry.Capability,
			Name:         entry.Name,
			DisplayName:  entry.DisplayName,
			Description:  resolved.Capability.Description,
			InputSchema:  cloneJSON(resolved.Capability.InputSchema),
			OutputSchema: cloneJSON(resolved.Capability.OutputSchema),
		},
	}, nil
}

func validateArgs(inputSchema, args json.RawMessage) (json.RawMessage, error) {
	normalizedArgs, err := normalizeObjectArgs(args)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArguments, err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(inputSchema, &schema); err != nil {
		return nil, fmt.Errorf("%w: invalid input schema: %v", ErrUnavailable, err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve input schema: %v", ErrUnavailable, err)
	}
	// jsonschema-go v0.4.3 classifies json.Number by its underlying string kind
	// during type checks, so validation needs the standard JSON-decoded values.
	var schemaInstance map[string]any
	if err := json.Unmarshal(normalizedArgs, &schemaInstance); err != nil {
		return nil, fmt.Errorf("%w: decode normalized arguments: %v", ErrInvalidArguments, err)
	}
	if err := resolved.Validate(schemaInstance); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArguments, err)
	}
	return normalizedArgs, nil
}

func normalizeObjectArgs(args json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(args)) == 0 {
		args = json.RawMessage(`{}`)
	}
	var object map[string]any
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil {
		return nil, err
	}
	if object == nil {
		return nil, fmt.Errorf("args must be a JSON object")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values are not allowed")
		}
		return nil, err
	}
	normalizedArgs, err := json.Marshal(object)
	if err != nil {
		return nil, err
	}
	return normalizedArgs, nil
}

func cloneJSON(raw json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), raw...)
}
