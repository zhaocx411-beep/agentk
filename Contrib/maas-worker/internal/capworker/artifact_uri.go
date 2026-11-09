package capworker

import (
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"
)

type artifactURIResolver struct{}

func newArtifactURIResolver() artifactURIResolver { return artifactURIResolver{} }

// URI 把 MaaS 返回的 db:table:key 物理引用转换成与网络拓扑无关的 Blob URI。
// 它只保留 bucket/key 身份，不上传、复制、下载或解析 Blob endpoint。
func (r artifactURIResolver) URI(reference string) (string, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return "", fmt.Errorf("artifact reference is empty")
	}
	if strings.HasPrefix(reference, "blob://") {
		bucket, key, ok := strings.Cut(strings.TrimPrefix(reference, "blob://"), "/")
		if !ok || !validArtifactBucket(bucket) {
			return "", fmt.Errorf("artifact URI %q has no valid bucket", reference)
		}
		if err := validateArtifactKey(key); err != nil {
			return "", err
		}
		return "blob://" + bucket + "/" + key, nil
	}
	parsed, err := url.Parse(reference)
	if err == nil && parsed.User == nil && parsed.Host != "" &&
		(parsed.Scheme == "http" || parsed.Scheme == "https") {
		return reference, nil
	}
	parts := strings.SplitN(reference, ":", 3)
	if len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != "" {
		bucket := parts[0] + "-" + parts[1]
		if !validArtifactBucket(bucket) {
			return "", fmt.Errorf("artifact bucket %q is invalid", bucket)
		}
		if err := validateArtifactKey(parts[2]); err != nil {
			return "", err
		}
		return "blob://" + bucket + "/" + parts[2], nil
	}
	return "", fmt.Errorf(
		"artifact reference %q is not blob://bucket/key, db:table:key, or HTTP(S)", reference)
}

func validArtifactBucket(bucket string) bool {
	return bucket != "" && !strings.ContainsAny(bucket, ":/?#")
}

func validateArtifactKey(key string) error {
	if key == "" || strings.HasPrefix(key, "/") || path.Clean(key) != key ||
		strings.HasPrefix(key, "../") || strings.ContainsAny(key, "?#") {
		return fmt.Errorf("artifact key %q is invalid", key)
	}
	return nil
}

func (a *inferActivity) attachManagedArtifacts(output map[string]any) (map[string]any, error) {
	if len(a.artifactOutputs) == 0 {
		return output, nil
	}
	features := make([]string, 0, len(a.artifactOutputs))
	for feature := range a.artifactOutputs {
		features = append(features, feature)
	}
	sort.Strings(features)
	artifacts := make([]map[string]any, 0, len(features))
	for _, feature := range features {
		value, exists := output[feature]
		if !exists || value == nil {
			continue
		}
		reference, ok := nonEmptyString(value)
		if !ok {
			return nil, invalidGatewayOutput(
				"artifact output %q must be a non-empty string", feature)
		}
		uri, err := a.artifactURIs.URI(reference)
		if err != nil {
			return nil, invalidGatewayOutput(
				"normalize artifact output %q: %v", feature, err)
		}
		artifacts = append(artifacts, map[string]any{
			"type": a.artifactOutputs[feature], "uri": uri,
			"description": fmt.Sprintf("MaaS 输出字段 %s", feature),
		})
	}
	if len(artifacts) == 0 {
		return output, nil
	}
	result := make(map[string]any, len(output)+1)
	for name, value := range output {
		result[name] = value
	}
	result["artifacts"] = artifacts
	return result, nil
}
