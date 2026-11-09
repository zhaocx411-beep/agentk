package keffects

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"

	pb "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gen/keffects/v1"
)

const defaultMediaKeyPrefix = "keffects/inputs/"

// MediaStore is the storage boundary used by the keffects client. Implementations
// own URI parsing and object I/O; Client owns the work/output bucket lifecycle.
type MediaStore interface {
	Resolve(uri string) (bucket, key string, matched bool, err error)
	Put(
		ctx context.Context,
		bucket, key string,
		data []byte,
		contentType string,
	) error
	Copy(
		ctx context.Context,
		srcBucket, srcKey, dstBucket, dstKey string,
	) error
	URI(bucket, key string) (string, bool)
}

type StageMediaOptions struct {
	MaxDownloadBytes int64
	ImportKeyPrefix  string
}

// MediaWorkflowClient is the complete workflow boundary for consumers that
// submit URI-backed media and return canonical result URIs.
type MediaWorkflowClient interface {
	WorkflowClient
	StageMedia(context.Context, string, StageMediaOptions) (*pb.Media, error)
	OutputURI(*pb.Media) (string, error)
}

// StageMedia resolves a managed URI or imports an external HTTP(S) resource,
// then ensures the object is available in WorkBucket.
func (c *Client) StageMedia(
	ctx context.Context,
	uri string,
	options StageMediaOptions,
) (*pb.Media, error) {
	if c.config.MediaStore == nil {
		return nil, fmt.Errorf("keffects: media store is required")
	}
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return nil, fmt.Errorf("keffects: media URI is required")
	}
	bucket, key, matched, err := c.config.MediaStore.Resolve(uri)
	if err != nil {
		return nil, fmt.Errorf("keffects: resolve media: %w", err)
	}
	if matched {
		if strings.TrimSpace(bucket) == "" || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("keffects: media URI %q resolved to an empty bucket or key", uri)
		}
		if bucket != WorkBucket {
			stagedKey := stagedInputKey(bucket, key)
			if err := c.config.MediaStore.Copy(
				ctx, bucket, key, WorkBucket, stagedKey,
			); err != nil {
				return nil, fmt.Errorf(
					"keffects: stage media %s/%s in work bucket %s: %w",
					bucket, key, WorkBucket, err)
			}
			key = stagedKey
		}
		return &pb.Media{Bucket: WorkBucket, Key: key}, nil
	}
	if options.MaxDownloadBytes <= 0 {
		return nil, fmt.Errorf("keffects: positive media byte limit is required")
	}
	parsed, err := url.Parse(uri)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf(
			"keffects: media %q is not managed storage or an HTTP(S) URL", uri)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, fmt.Errorf("keffects: create media request: %w", err)
	}
	response, err := c.config.HTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("keffects: download media %s: %w", uri, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"keffects: download media %s: HTTP %d", uri, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, options.MaxDownloadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("keffects: read media %s: %w", uri, err)
	}
	if int64(len(data)) > options.MaxDownloadBytes {
		return nil, fmt.Errorf(
			"keffects: media %s exceeds %d bytes", uri, options.MaxDownloadBytes)
	}
	contentType := strings.TrimSpace(
		strings.Split(response.Header.Get("Content-Type"), ";")[0])
	extension := path.Ext(parsed.Path)
	if extension == "" {
		extensions, _ := mime.ExtensionsByType(contentType)
		if len(extensions) > 0 {
			extension = extensions[0]
		}
	}
	prefix := strings.TrimLeft(strings.TrimSpace(options.ImportKeyPrefix), "/")
	if prefix == "" {
		prefix = defaultMediaKeyPrefix
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	sum := sha256.Sum256([]byte(uri))
	key = fmt.Sprintf("%s%x%s", prefix, sum[:16], extension)
	if err := c.config.MediaStore.Put(
		ctx, WorkBucket, key, data, contentType,
	); err != nil {
		return nil, fmt.Errorf("keffects: store imported media: %w", err)
	}
	return &pb.Media{Bucket: WorkBucket, Key: key}, nil
}

// OutputURI converts an output-bucket media result into its caller URI.
func (c *Client) OutputURI(media *pb.Media) (string, error) {
	if c.config.MediaStore == nil {
		return "", fmt.Errorf("keffects: media store is required")
	}
	if media == nil || media.GetBucket() == "" || media.GetKey() == "" {
		return "", fmt.Errorf("keffects: output media is empty")
	}
	if c.config.OutputBucket != "" && media.GetBucket() != c.config.OutputBucket {
		return "", fmt.Errorf(
			"keffects: output media is in bucket %q, want output bucket %q",
			media.GetBucket(), c.config.OutputBucket)
	}
	uri, ok := c.config.MediaStore.URI(media.GetBucket(), media.GetKey())
	if !ok || strings.TrimSpace(uri) == "" {
		return "", fmt.Errorf(
			"keffects: cannot build URI for %s/%s", media.GetBucket(), media.GetKey())
	}
	return uri, nil
}

func stagedInputKey(bucket, key string) string {
	sum := sha256.Sum256([]byte(bucket + "\x00" + key))
	basename := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, path.Base(key))
	return fmt.Sprintf("keffects/inputs/managed/%x/%s", sum, basename)
}
