package capcatalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
)

func DecodeManifestObject(raw map[string]any) (sdkcapability.ManifestV1, error) {
	encoded, err := json.Marshal(withoutLegacySurfaceFields(raw))
	if err != nil {
		return sdkcapability.ManifestV1{}, fmt.Errorf("encode manifest object: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var manifest sdkcapability.ManifestV1
	if err := decoder.Decode(&manifest); err != nil {
		return sdkcapability.ManifestV1{}, fmt.Errorf("decode manifest object: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return sdkcapability.ManifestV1{}, fmt.Errorf("decode manifest object: trailing data")
	}
	return manifest, nil
}

// withoutLegacySurfaceFields 兼容旧 Worker announce：旧 Pipeline 声明已无语义，
// 旧 Tool name 由 Profile name 取代。除此之外仍由严格 decoder 拒绝未知字段。
func withoutLegacySurfaceFields(raw map[string]any) map[string]any {
	compatible := maps.Clone(raw)
	capabilities, ok := raw["capabilities"].([]any)
	if !ok {
		return compatible
	}
	cloned := make([]any, len(capabilities))
	for i, item := range capabilities {
		def, ok := item.(map[string]any)
		if !ok {
			cloned[i] = item
			continue
		}
		capability := maps.Clone(def)
		delete(capability, "pipeline")
		if tool, ok := def["tool"].(map[string]any); ok {
			tool = maps.Clone(tool)
			delete(tool, "name")
			capability["tool"] = tool
		}
		cloned[i] = capability
	}
	compatible["capabilities"] = cloned
	return compatible
}

func NormalizeManifest(
	raw sdkcapability.ManifestV1,
) (sdkcapability.ManifestV1, string, error) {
	normalized, err := raw.Normalize()
	if err != nil {
		return sdkcapability.ManifestV1{}, "", err
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return sdkcapability.ManifestV1{}, "", fmt.Errorf("marshal normalized manifest: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return normalized, hex.EncodeToString(sum[:]), nil
}
