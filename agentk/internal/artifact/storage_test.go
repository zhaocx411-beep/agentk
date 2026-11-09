package artifact

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	keffectsblob "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/keffects/blobstore"
)

type artifactBlobBackend struct {
	objects     map[string][]byte
	contentType string
	err         error
}

func (b *artifactBlobBackend) Put(
	_ context.Context,
	bucket, key string,
	body io.Reader,
	contentType string,
) error {
	if b.err != nil {
		return b.err
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	b.objects[bucket+"/"+key] = data
	b.contentType = contentType
	return nil
}

func (b *artifactBlobBackend) Get(
	_ context.Context,
	bucket, key string,
) (io.ReadCloser, string, error) {
	if b.err != nil {
		return nil, "", b.err
	}
	data, ok := b.objects[bucket+"/"+key]
	if !ok {
		return nil, "", fmt.Errorf("missing object")
	}
	return io.NopCloser(bytes.NewReader(data)), b.contentType, nil
}

func newArtifactBlobStorage(
	t *testing.T,
	cfg BlobConfig,
	backend keffectsblob.Backend,
) *BlobStorage {
	t.Helper()
	store, err := keffectsblob.NewWithBackend(keffectsblob.Config{
		Endpoint:       cfg.Endpoint,
		Region:         cfg.Region,
		BusinessBucket: cfg.Bucket,
		WorkBucket:     cfg.Bucket,
		CDNBase:        cfg.CDNBase,
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	return newBlobStorageWithStore(cfg, store)
}

func TestBlobStoragePutResolveAndOpen(t *testing.T) {
	backend := &artifactBlobBackend{objects: map[string][]byte{}}
	private := newArtifactBlobStorage(t, BlobConfig{
		Endpoint: "http://blob.test", Bucket: "bucket",
	}, backend)
	object, err := private.Put(context.Background(), "org/a.png", []byte("png"), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if object.URL != "http://blob.test/bucket/org/a.png" ||
		object.Visibility != Private || backend.contentType != "image/png" {
		t.Fatalf("object = %+v content-type = %q", object, backend.contentType)
	}
	resolved, ok := private.Resolve(object.URL)
	if !ok || resolved.Key != object.Key || resolved.Visibility != Private {
		t.Fatalf("resolved = %+v ok=%v", resolved, ok)
	}
	r, err := private.Open(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(r)
	_ = r.Close()
	if string(data) != "png" {
		t.Fatalf("open = %q", data)
	}

	public := newArtifactBlobStorage(t, BlobConfig{
		Endpoint: "http://blob.test", Bucket: "bucket",
		CDNBase: "https://cdn.example/assets",
	}, backend)
	object, err = public.Put(context.Background(), "org/b.png", []byte("png2"), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if object.URL != "https://cdn.example/assets/org/b.png" || object.Visibility != Public {
		t.Fatalf("public object = %+v", object)
	}
	if resolved, ok = public.Resolve(object.URL); !ok || resolved.Visibility != Public {
		t.Fatalf("public resolve = %+v ok=%v", resolved, ok)
	}
	if _, ok := public.Resolve("https://external.example/a.png"); ok {
		t.Fatal("external URL must not be claimed")
	}
}

func TestBlobStorageErrorsAndConfig(t *testing.T) {
	if _, err := NewBlobStorage(BlobConfig{Bucket: "b"}); err == nil {
		t.Fatal("missing endpoint must fail")
	}
	if _, err := NewBlobStorage(BlobConfig{Endpoint: "http://x"}); err == nil {
		t.Fatal("missing bucket must fail")
	}
	if (BlobConfig{}).Configured() {
		t.Fatal("empty config must not be configured")
	}
	if (BlobConfig{Endpoint: "e"}).Configured() {
		t.Fatal("partial config must be incomplete")
	}

	storage := newArtifactBlobStorage(t, BlobConfig{
		Endpoint: "http://blob.test", Bucket: "b",
	}, &artifactBlobBackend{
		objects: map[string][]byte{},
		err:     fmt.Errorf("AccessDenied"),
	})
	if _, err := storage.Put(context.Background(), "k", []byte("x"), ""); err == nil {
		t.Fatal("upload error must be returned")
	}
	if _, err := storage.Open(context.Background(), Object{Key: "k"}); err == nil {
		t.Fatal("read error must be returned")
	}
}

func TestLocalStorageRoundTripAndSafety(t *testing.T) {
	dir := t.TempDir()
	storage := NewLocalStorage(dir, "http://127.0.0.1:8080/")
	object, err := storage.Put(context.Background(), "org/20260723/a.mp4", []byte("data"), "video/mp4")
	if err != nil {
		t.Fatal(err)
	}
	if object.URL != "http://127.0.0.1:8080/files/org/20260723/a.mp4" || object.Visibility != Private {
		t.Fatalf("object = %+v", object)
	}
	onDisk, err := os.ReadFile(filepath.Join(dir, "org", "20260723", "a.mp4"))
	if err != nil || string(onDisk) != "data" {
		t.Fatalf("disk = %q err=%v", onDisk, err)
	}
	resolved, ok := storage.Resolve(object.URL)
	if !ok {
		t.Fatal("local URL not resolved")
	}
	r, err := storage.Open(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(r)
	_ = r.Close()
	if string(data) != "data" {
		t.Fatalf("open = %q", data)
	}
	if _, err := storage.Put(context.Background(), "../escape", []byte("x"), ""); err == nil {
		t.Fatal("path traversal must fail")
	}
	if _, ok := storage.Resolve("http://127.0.0.1:8080/files/../escape"); ok {
		t.Fatal("unsafe URL must not resolve")
	}
}
