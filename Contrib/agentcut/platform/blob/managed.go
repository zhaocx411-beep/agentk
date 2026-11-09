package blob

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"

	"agentcut/timeline"
)

// ResolveManagedKey resolves a trusted source into a Storage key without
// downloading an arbitrary URL. Explicit blobKey wins; otherwise uri must be
// under the configured Blob endpoint, CDN base, or an explicit managed base.
func ResolveManagedKey(blobKey, uri string) (string, error) {
	if key := strings.TrimSpace(blobKey); key != "" {
		return validateManagedKey(key)
	}
	rawURI := strings.TrimSpace(uri)
	parsed, err := url.Parse(rawURI)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("media requires blobKey or an allowlisted HTTP/HTTPS blob URI")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("managed blob URI must not contain query or fragment")
	}
	for _, base := range managedBases() {
		key, ok := keyUnderBase(parsed, base)
		if !ok {
			continue
		}
		return validateManagedKey(key)
	}
	return "", fmt.Errorf("media URI is outside the configured Blob endpoint/CDN allowlist")
}

// ResolveAgentMediaKey intentionally ignores every caller-provided blobKey:
// an Agent may only prove ownership by supplying an allowlisted URI. The
// resolved object must be either an Agent-K artifact or this exact media's
// project-owned source object.
func ResolveAgentMediaKey(projectID, mediaID, uri string) (string, error) {
	key, err := ResolveManagedKey("", uri)
	if err != nil {
		return "", err
	}
	if err := ValidateMediaSourceKey(projectID, mediaID, key); err != nil {
		return "", err
	}
	return key, nil
}

// ResolveMediaSourceKey is the preparation-time defense in depth. HTTP sources
// are re-derived from their URI and must agree with stored metadata. Local
// upload URIs cannot encode a Blob key, so their stored key is validated
// directly against the exact project/media source prefix.
func ResolveMediaSourceKey(projectID, mediaID, blobKey, uri string) (string, error) {
	rawURI := strings.TrimSpace(uri)
	parsed, _ := url.Parse(rawURI)
	if parsed != nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		derived, err := ResolveManagedKey("", rawURI)
		if err != nil {
			return "", err
		}
		if stored := strings.TrimSpace(blobKey); stored != "" {
			validStored, err := validateManagedKey(stored)
			if err != nil || validStored != derived {
				return "", fmt.Errorf("media blobKey does not match its allowlisted URI")
			}
		}
		if err := ValidateMediaSourceKey(projectID, mediaID, derived); err != nil {
			return "", err
		}
		return derived, nil
	}
	key, err := validateManagedKey(blobKey)
	if err != nil {
		return "", err
	}
	if err := ValidateMediaSourceKey(projectID, mediaID, key); err != nil {
		return "", err
	}
	return key, nil
}

// ResolveRenderSourceKey resolves only the authoritative Timeline BlobKey.
// Render activities must never derive a download target from Timeline URI:
// URI is presentation/legacy metadata and may be stale or attacker-controlled.
func ResolveRenderSourceKey(projectID, mediaID, blobKey string) (string, error) {
	key, err := validateManagedKey(blobKey)
	if err != nil {
		return "", fmt.Errorf("render media requires a valid Timeline blobKey: %w", err)
	}
	if !IsProjectMediaSourceKey(projectID, mediaID, key) {
		return "", fmt.Errorf("render media must be archived in the project/media source namespace")
	}
	return key, nil
}

func IsProjectMediaSourceKey(projectID, mediaID, key string) bool {
	if timeline.ValidateMediaID(mediaID) != nil {
		return false
	}
	validKey, err := validateManagedKey(key)
	if err != nil {
		return false
	}
	return strings.HasPrefix(validKey, fmt.Sprintf("agentcut/%s/source/%s/", projectID, mediaID))
}

func ValidateMediaSourceKey(projectID, mediaID, key string) error {
	if err := timeline.ValidateMediaID(mediaID); err != nil {
		return err
	}
	validKey, err := validateManagedKey(key)
	if err != nil {
		return err
	}
	projectPrefix := fmt.Sprintf("agentcut/%s/source/%s/", projectID, mediaID)
	if strings.HasPrefix(validKey, projectPrefix) {
		return nil
	}
	artifactPrefix := strings.Trim(strings.TrimSpace(os.Getenv("AGENTCUT_AGENTK_ARTIFACT_PREFIX")), "/")
	if artifactPrefix == "" {
		artifactPrefix = "agentk/artifacts"
	}
	if validPrefix, err := validateManagedKey(artifactPrefix); err == nil &&
		(validKey == validPrefix || strings.HasPrefix(validKey, validPrefix+"/")) {
		return nil
	}
	return fmt.Errorf("media source is outside the configured Agent-K artifact namespace and project/media source prefix")
}

func validateManagedKey(key string) (string, error) {
	key = strings.TrimSpace(strings.ReplaceAll(key, "\\", "/"))
	clean := path.Clean(key)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") ||
		strings.HasPrefix(clean, "/") || clean != key || strings.Contains(key, "://") {
		return "", fmt.Errorf("invalid managed blob key")
	}
	return clean, nil
}

func managedBases() []*url.URL {
	var rawBases []string
	endpoint := strings.TrimSpace(os.Getenv("AGENTCUT_BLOB_ENDPOINT"))
	bucket := strings.Trim(strings.TrimSpace(os.Getenv("AGENTCUT_BLOB_BUCKET")), "/")
	if endpoint != "" && bucket != "" {
		rawBases = append(rawBases, strings.TrimSuffix(endpoint, "/")+"/"+bucket)
	}
	if cdn := strings.TrimSpace(os.Getenv("AGENTCUT_BLOB_CDN_BASE")); cdn != "" {
		rawBases = append(rawBases, cdn)
	}
	for _, base := range strings.Split(os.Getenv("AGENTCUT_BLOB_URL_ALLOWLIST"), ",") {
		if base = strings.TrimSpace(base); base != "" {
			rawBases = append(rawBases, base)
		}
	}
	bases := make([]*url.URL, 0, len(rawBases))
	for _, raw := range rawBases {
		base, err := url.Parse(strings.TrimSuffix(raw, "/"))
		if err == nil && (base.Scheme == "http" || base.Scheme == "https") && base.Host != "" {
			bases = append(bases, base)
		}
	}
	return bases
}

func keyUnderBase(uri, base *url.URL) (string, bool) {
	if !strings.EqualFold(uri.Scheme, base.Scheme) || !strings.EqualFold(uri.Host, base.Host) {
		return "", false
	}
	basePath := strings.TrimSuffix(base.Path, "/")
	if uri.Path == basePath || !strings.HasPrefix(uri.Path, basePath+"/") {
		return "", false
	}
	return strings.TrimPrefix(uri.Path, basePath+"/"), true
}

func IsManagedHTTPURI(uri string) bool {
	parsed, err := url.Parse(strings.TrimSpace(uri))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	_, err = ResolveManagedKey("", uri)
	return err == nil
}

// ManagedHTTPURI returns an allowlisted HTTP URL for a key. It preserves an
// already trusted URI, otherwise derives one from CDNBase or Endpoint/Bucket.
// Local storage intentionally returns an empty string because Agent-K cannot
// fetch local:// objects from another worker.
func ManagedHTTPURI(key, existingURI string) string {
	if IsManagedHTTPURI(existingURI) {
		return strings.TrimSpace(existingURI)
	}
	validKey, err := validateManagedKey(key)
	if err != nil {
		return ""
	}
	if cdn := strings.TrimSpace(os.Getenv("AGENTCUT_BLOB_CDN_BASE")); cdn != "" {
		return strings.TrimSuffix(cdn, "/") + "/" + validKey
	}
	endpoint := strings.TrimSpace(os.Getenv("AGENTCUT_BLOB_ENDPOINT"))
	bucket := strings.Trim(strings.TrimSpace(os.Getenv("AGENTCUT_BLOB_BUCKET")), "/")
	if endpoint == "" || bucket == "" {
		return ""
	}
	return strings.TrimSuffix(endpoint, "/") + "/" + bucket + "/" + validKey
}
