package artifactarchive

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"agentcut/platform/blob"
	"agentcut/timeline"
)

const MaxArtifactBytes = int64(2 << 30)

type Provenance struct {
	ExecutionID string
	ToolCallID  string
	Type        string
}

// Verifier proves that an exact artifact URI was emitted by a successful
// Agent-K tool call in the project-bound session. URI allowlisting alone is
// insufficient because shared artifact prefixes are not project-scoped.
type Verifier interface {
	VerifyArtifact(ctx context.Context, sessionID, uri string, kind timeline.MediaKind) (Provenance, error)
}

type Result struct {
	BlobKey string
	URI     string
}

type Archiver struct {
	Store    blob.Storage
	Verifier Verifier
	Client   *http.Client
}

func (a *Archiver) Archive(
	ctx context.Context,
	projectID, sessionID string,
	media timeline.Media,
) (Result, error) {
	if a.Store == nil {
		return Result{}, fmt.Errorf("AgentCut Blob storage is not configured")
	}
	if a.Verifier == nil {
		return Result{}, fmt.Errorf("Agent-K artifact verification is not configured")
	}
	if !safeProjectID.MatchString(projectID) {
		return Result{}, fmt.Errorf("invalid projectId")
	}
	if err := timeline.ValidateMediaID(media.MediaID); err != nil {
		return Result{}, err
	}
	if !strings.HasPrefix(sessionID, "ses_") {
		return Result{}, fmt.Errorf("valid project Agent-K session is required")
	}

	source, err := validateArtifactURL(media.URI)
	if err != nil {
		return Result{}, err
	}
	if _, err := a.Verifier.VerifyArtifact(ctx, sessionID, source.String(), media.Kind); err != nil {
		return Result{}, fmt.Errorf("artifact provenance rejected: %w", err)
	}

	client := a.Client
	if client == nil {
		client = &http.Client{
			Timeout: 15 * time.Minute,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) > 5 {
					return fmt.Errorf("too many artifact redirects")
				}
				_, err := validateArtifactURL(req.URL.String())
				return err
			},
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.String(), nil)
	if err != nil {
		return Result{}, fmt.Errorf("create artifact request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return Result{}, fmt.Errorf("download verified artifact: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("download verified artifact: HTTP %d", response.StatusCode)
	}
	if response.ContentLength <= 0 {
		return Result{}, fmt.Errorf("verified artifact requires a positive Content-Length for streaming archive")
	}
	if response.ContentLength > MaxArtifactBytes {
		return Result{}, fmt.Errorf("verified artifact exceeds 2 GiB")
	}

	filename, contentType, err := archiveFilename(media.Kind, source.Path, response.Header.Get("Content-Type"))
	if err != nil {
		return Result{}, err
	}
	key := blob.SourceKey(projectID, media.MediaID, filename)
	if key == "" {
		return Result{}, fmt.Errorf("failed to derive project artifact key")
	}
	archivedURI, err := a.Store.Put(
		ctx,
		key,
		io.LimitReader(response.Body, response.ContentLength),
		response.ContentLength,
		contentType,
	)
	if err != nil {
		return Result{}, fmt.Errorf("archive verified artifact: %w", err)
	}
	if a.Store.Backend() == "local" {
		archivedURI = fmt.Sprintf("/v1/projects/%s/media/%s/content", projectID, media.MediaID)
	}
	return Result{BlobKey: key, URI: archivedURI}, nil
}

var safeProjectID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,160}$`)

func ValidateURL(raw string) error {
	_, err := validateArtifactURL(raw)
	return err
}

func validateArtifactURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.User != nil || parsed.Fragment != "" {
		return nil, fmt.Errorf("artifact URI must be an allowlisted HTTP/HTTPS URL")
	}
	for _, base := range artifactBases() {
		if strings.EqualFold(parsed.Scheme, base.Scheme) &&
			strings.EqualFold(parsed.Host, base.Host) &&
			pathUnderBase(parsed.EscapedPath(), base.EscapedPath()) {
			return parsed, nil
		}
	}
	return nil, fmt.Errorf("artifact URI is outside AGENTCUT_AGENTK_ARTIFACT_URL_ALLOWLIST")
}

func artifactBases() []*url.URL {
	var values []string
	if endpoint := strings.TrimSpace(os.Getenv("AGENTCUT_BLOB_ENDPOINT")); endpoint != "" {
		if parsed, err := url.Parse(endpoint); err == nil && parsed.Scheme != "" && parsed.Host != "" {
			values = append(values, parsed.Scheme+"://"+parsed.Host)
		}
	}
	values = append(values, strings.Split(os.Getenv("AGENTCUT_AGENTK_ARTIFACT_URL_ALLOWLIST"), ",")...)
	bases := make([]*url.URL, 0, len(values))
	for _, value := range values {
		parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(value), "/"))
		if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") &&
			parsed.Host != "" && parsed.User == nil {
			bases = append(bases, parsed)
		}
	}
	return bases
}

func pathUnderBase(candidate, base string) bool {
	base = strings.TrimSuffix(base, "/")
	return base == "" || base == "/" || candidate == base || strings.HasPrefix(candidate, base+"/")
}

func archiveFilename(kind timeline.MediaKind, sourcePath, rawContentType string) (string, string, error) {
	contentType, _, _ := mime.ParseMediaType(rawContentType)
	ext := strings.ToLower(filepath.Ext(sourcePath))
	allowed := map[timeline.MediaKind]map[string]string{
		timeline.MediaAudio: {
			".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".aac": "audio/aac",
			".wav": "audio/wav", ".ogg": "audio/ogg", ".flac": "audio/flac",
		},
		timeline.MediaVideo: {
			".mp4": "video/mp4", ".mov": "video/quicktime",
			".webm": "video/webm", ".mkv": "video/x-matroska",
		},
		timeline.MediaImage: {
			".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".webp": "image/webp",
		},
	}
	types, ok := allowed[kind]
	if !ok {
		return "", "", fmt.Errorf("unsupported artifact media kind %q", kind)
	}
	expected, ok := types[ext]
	if !ok {
		return "", "", fmt.Errorf("artifact file extension is not supported for %s", kind)
	}
	if contentType != "" && contentType != "application/octet-stream" &&
		!strings.HasPrefix(contentType, string(kind)+"/") {
		return "", "", fmt.Errorf("artifact Content-Type %q does not match %s", contentType, kind)
	}
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = expected
	}
	name := string(kind) + ext
	if ext == ".jpeg" {
		name = string(kind) + ".jpg"
	}
	return name, contentType, nil
}
