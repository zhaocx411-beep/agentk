package artifact

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/keffects"
	keffectsblob "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/keffects/blobstore"
)

// BlobConfig configures the durable AgentK artifact bucket.
type BlobConfig struct {
	Endpoint string
	Bucket   string
	CDNBase  string
	Region   string
}

// BlobConfigFromEnv reads the startup-stable artifact storage identity.
func BlobConfigFromEnv() BlobConfig {
	return BlobConfig{
		Endpoint: os.Getenv("AGENTK_BLOB_ENDPOINT"),
		Bucket:   os.Getenv("AGENTK_BLOB_BUCKET"),
		CDNBase:  os.Getenv("AGENTK_BLOB_CDN_BASE"),
		Region:   envOr("AGENTK_BLOB_REGION", "hb1"),
	}
}

func (c BlobConfig) Configured() bool {
	return strings.TrimSpace(c.Endpoint) != "" && strings.TrimSpace(c.Bucket) != ""
}

// BlobStorage is the Artifact-domain view over the upstream Kess-aware store.
// Bucket transitions and keffects media lifecycle stay in the upstream SDK.
type BlobStorage struct {
	cfg   BlobConfig
	store *keffectsblob.Store
}

func NewBlobStorage(cfg BlobConfig) (*BlobStorage, error) {
	cfg.Endpoint = strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	cfg.Bucket = strings.Trim(strings.TrimSpace(cfg.Bucket), "/")
	cfg.CDNBase = strings.TrimRight(strings.TrimSpace(cfg.CDNBase), "/")
	cfg.Region = strings.TrimSpace(cfg.Region)
	if !cfg.Configured() {
		return nil, fmt.Errorf(
			"artifact: blob endpoint and bucket are required " +
				"(AGENTK_BLOB_ENDPOINT / AGENTK_BLOB_BUCKET)")
	}
	store, err := keffectsblob.New(keffectsblob.Config{
		Endpoint:       cfg.Endpoint,
		Region:         cfg.Region,
		BusinessBucket: cfg.Bucket,
		WorkBucket:     cfg.Bucket,
		CDNBase:        cfg.CDNBase,
	})
	if err != nil {
		return nil, fmt.Errorf("artifact: create BlobStore client: %w", err)
	}
	return newBlobStorageWithStore(cfg, store), nil
}

func newBlobStorageWithStore(cfg BlobConfig, store *keffectsblob.Store) *BlobStorage {
	return &BlobStorage{cfg: cfg, store: store}
}

func (s *BlobStorage) Put(
	ctx context.Context,
	key string,
	data []byte,
	contentType string,
) (Object, error) {
	if err := s.store.Put(ctx, s.cfg.Bucket, key, data, contentType); err != nil {
		return Object{}, fmt.Errorf("artifact: put %q: %w", key, err)
	}
	return s.object(key), nil
}

func (s *BlobStorage) Resolve(uri string) (Object, bool) {
	bucket, key, ok := s.store.Resolve(uri)
	if !ok || bucket != s.cfg.Bucket {
		return Object{}, false
	}
	object := Object{Key: key, URL: uri, Visibility: Private}
	if s.cfg.CDNBase != "" && strings.HasPrefix(uri, s.cfg.CDNBase+"/") {
		object.Visibility = Public
	}
	return object, true
}

func (s *BlobStorage) Open(ctx context.Context, object Object) (io.ReadCloser, error) {
	body, err := s.store.Get(ctx, s.cfg.Bucket, object.Key)
	if err != nil {
		return nil, fmt.Errorf("artifact: get %q: %w", object.Key, err)
	}
	return body, nil
}

func (s *BlobStorage) object(key string) Object {
	uri, _ := s.store.URI(s.cfg.Bucket, key)
	visibility := Private
	if s.cfg.CDNBase != "" {
		visibility = Public
	}
	return Object{Key: key, URL: uri, Visibility: visibility}
}

// KeffectsMediaStore returns the upstream media store view used by one
// workflow. AgentK only supplies deployment configuration; staging and bucket
// transitions are implemented by the SDK.
func (s *BlobStorage) KeffectsMediaStore(
	workBucket string,
) (keffects.MediaStore, string, string) {
	store := s.store.WithWorkBucket(workBucket)
	return store, store.WorkBucket(), store.BusinessBucket()
}
