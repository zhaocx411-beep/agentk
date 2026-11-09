package keffects

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pb "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gen/keffects/v1"
)

type fakeMediaStore struct {
	resolvedBucket string
	resolvedKey    string
	resolveErr     error
	putCalls       int
	copyCalls      int
	copySrcBucket  string
	copySrcKey     string
	copyDstBucket  string
	copyDstKey     string
	copyErr        error
	putBucket      string
	putKey         string
	putData        []byte
	putContentType string
	uri            string
}

func (f *fakeMediaStore) Resolve(string) (string, string, bool, error) {
	return f.resolvedBucket, f.resolvedKey, f.resolveErr != nil || f.resolvedBucket != "", f.resolveErr
}

func (f *fakeMediaStore) Put(
	_ context.Context,
	bucket, key string,
	data []byte,
	contentType string,
) error {
	f.putCalls++
	f.putBucket = bucket
	f.putKey = key
	f.putData = append([]byte(nil), data...)
	f.putContentType = contentType
	return nil
}

func (f *fakeMediaStore) Copy(
	_ context.Context,
	srcBucket, srcKey, dstBucket, dstKey string,
) error {
	f.copyCalls++
	f.copySrcBucket = srcBucket
	f.copySrcKey = srcKey
	f.copyDstBucket = dstBucket
	f.copyDstKey = dstKey
	return f.copyErr
}

func (f *fakeMediaStore) URI(string, string) (string, bool) {
	return f.uri, f.uri != ""
}

func TestStageMediaCopiesManagedObjectToWorkBucket(t *testing.T) {
	store := &fakeMediaStore{resolvedBucket: "business", resolvedKey: "input.png"}
	client := NewClient(Config{
		OutputBucket: "business", MediaStore: store,
	})
	media, err := client.StageMedia(
		context.Background(), "https://cdn.example/input.png",
		StageMediaOptions{MaxDownloadBytes: 10},
	)
	if err != nil {
		t.Fatal(err)
	}
	if store.copyCalls != 1 || media.GetBucket() != WorkBucket ||
		media.GetKey() != stagedInputKey("business", "input.png") || store.copyDstKey != media.GetKey() {
		t.Fatalf("copy calls=%d media=%+v", store.copyCalls, media)
	}
}

func TestStageMediaReusesManagedObjectAlreadyInWorkBucket(t *testing.T) {
	store := &fakeMediaStore{resolvedBucket: WorkBucket, resolvedKey: "input.png"}
	client := NewClient(Config{
		OutputBucket: "business", MediaStore: store,
	})
	media, err := client.StageMedia(
		context.Background(), "http://blob/work/input.png",
		StageMediaOptions{MaxDownloadBytes: 10},
	)
	if err != nil {
		t.Fatal(err)
	}
	if store.copyCalls != 0 || media.GetBucket() != WorkBucket {
		t.Fatalf("copy calls=%d media=%+v", store.copyCalls, media)
	}
}

func TestStageMediaImportsExternalHTTPResource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png"))
	}))
	t.Cleanup(server.Close)
	store := &fakeMediaStore{}
	client := NewClient(Config{
		OutputBucket: "business", MediaStore: store,
	})
	media, err := client.StageMedia(
		context.Background(), server.URL+"/asset",
		StageMediaOptions{MaxDownloadBytes: 10, ImportKeyPrefix: "custom"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if store.putBucket != WorkBucket || string(store.putData) != "png" ||
		store.putContentType != "image/png" ||
		!strings.HasPrefix(store.putKey, "custom/") ||
		!strings.HasSuffix(store.putKey, ".png") ||
		media.GetBucket() != WorkBucket || media.GetKey() != store.putKey {
		t.Fatalf("store=%+v media=%+v", store, media)
	}
}

func TestStageMediaEnforcesExternalByteLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("too large"))
	}))
	t.Cleanup(server.Close)
	client := NewClient(Config{MediaStore: &fakeMediaStore{}})
	_, err := client.StageMedia(
		context.Background(), server.URL,
		StageMediaOptions{MaxDownloadBytes: 3},
	)
	if err == nil || !strings.Contains(err.Error(), "exceeds 3 bytes") {
		t.Fatalf("error = %v", err)
	}
}

func TestStageMediaReturnsCopyError(t *testing.T) {
	store := &fakeMediaStore{
		resolvedBucket: "business",
		resolvedKey:    "input.png",
		copyErr:        errors.New("copy failed"),
	}
	client := NewClient(Config{
		OutputBucket: "business", MediaStore: store,
	})
	_, err := client.StageMedia(
		context.Background(), "https://cdn.example/input.png",
		StageMediaOptions{MaxDownloadBytes: 10},
	)
	if err == nil || !strings.Contains(err.Error(), "copy failed") {
		t.Fatalf("error = %v", err)
	}
}

func TestOutputURIRequiresOutputBucket(t *testing.T) {
	store := &fakeMediaStore{uri: "https://cdn.example/output.mp4"}
	client := NewClient(Config{OutputBucket: "business", MediaStore: store})
	uri, err := client.OutputURI(&pb.Media{Bucket: "business", Key: "output.mp4"})
	if err != nil {
		t.Fatal(err)
	}
	if uri != store.uri {
		t.Fatalf("URI = %q", uri)
	}
	if _, err := client.OutputURI(
		&pb.Media{Bucket: WorkBucket, Key: "output.mp4"},
	); err == nil {
		t.Fatal("work-bucket output must be rejected")
	}
}

func TestStageMediaStopsOnResolveError(t *testing.T) {
	sentinel := errors.New("invalid storage URL")
	store := &fakeMediaStore{resolveErr: sentinel}
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("resolve failure reached HTTP")
	}))
	defer server.Close()
	client := NewClient(Config{MediaStore: store})
	_, err := client.StageMedia(t.Context(), server.URL, StageMediaOptions{MaxDownloadBytes: 10})
	if !errors.Is(err, sentinel) || store.copyCalls != 0 || store.putCalls != 0 {
		t.Fatalf("err=%v copies=%d puts=%d", err, store.copyCalls, store.putCalls)
	}
}

func TestStagedInputKeySeparatesSourceBuckets(t *testing.T) {
	first := stagedInputKey("business", "dir/a b.png")
	if first == stagedInputKey("extra", "dir/a b.png") {
		t.Fatal("different source buckets collide")
	}
	if first != stagedInputKey("business", "dir/a b.png") || !strings.HasPrefix(first, "keffects/inputs/managed/") || !strings.HasSuffix(first, "/a_b.png") {
		t.Fatalf("unstable or invalid key: %s", first)
	}
}
