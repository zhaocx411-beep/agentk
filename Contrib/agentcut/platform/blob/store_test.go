package blob

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"agentcut/entity"
)

func TestNewStoreFromEnvConfiguration(t *testing.T) {
	for _, key := range []string{
		"AGENTCUT_BLOB_BACKEND", "AGENTCUT_BLOB_ENDPOINT", "AGENTCUT_BLOB_BUCKET",
		"AGENTCUT_BLOB_CDN_BASE", "AGENTCUT_LOCAL_BLOB_ROOT",
	} {
		t.Setenv(key, "")
	}
	store, err := NewStoreFromEnv()
	require.NoError(t, err)
	assert.Nil(t, store)
	assert.Equal(t, []string{"AGENTCUT_BLOB_ENDPOINT", "AGENTCUT_BLOB_BUCKET"}, MissingConfiguration())

	t.Setenv("AGENTCUT_BLOB_ENDPOINT", "https://blob")
	_, err = NewStoreFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AGENTCUT_BLOB_BUCKET")

	t.Setenv("AGENTCUT_BLOB_BACKEND", "local")
	t.Setenv("AGENTCUT_LOCAL_BLOB_ROOT", t.TempDir())
	store, err = NewStoreFromEnv()
	require.NoError(t, err)
	assert.Equal(t, "local", store.Backend())
}

func TestLocalStoreRoundTripAndTraversal(t *testing.T) {
	store, err := NewLocalStore(t.TempDir())
	require.NoError(t, err)
	ctx := context.Background()
	key := "agentcut/p1/source/m1/test.txt"

	uri, err := store.Put(ctx, key, bytes.NewBufferString("hello"), 5, "text/plain")
	require.NoError(t, err)
	assert.Equal(t, "local://"+key, uri)

	obj, err := store.Get(ctx, key)
	require.NoError(t, err)
	defer obj.Body.Close()
	raw, err := io.ReadAll(obj.Body)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(raw))
	assert.Equal(t, int64(5), obj.Size)

	require.NoError(t, store.Delete(ctx, key))
	require.NoError(t, store.Delete(ctx, key), "delete must be idempotent")
	_, err = store.Get(ctx, key)
	assert.ErrorIs(t, err, os.ErrNotExist)

	for _, bad := range []string{"../secret", "/tmp/secret", "agentcut/../../secret"} {
		_, err := store.Put(ctx, bad, bytes.NewReader(nil), 0, "")
		assert.Error(t, err, bad)
	}
}

func TestHTTPStoreForwardsByteRange(t *testing.T) {
	payload := []byte("0123456789")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/bucket/video.mp4", r.URL.Path)
		assert.Equal(t, "bytes=2-5", r.Header.Get("Range"))
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Range", "bytes 2-5/10")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[2:6])
	}))
	defer server.Close()

	store := NewStore(Config{Endpoint: server.URL, Bucket: "bucket"})
	object, err := store.GetRange(context.Background(), "video.mp4", "bytes=2-5")
	require.NoError(t, err)
	defer object.Body.Close()
	raw, err := io.ReadAll(object.Body)
	require.NoError(t, err)
	assert.Equal(t, payload[2:6], raw)
	assert.Equal(t, "bytes 2-5/10", object.ContentRange)
	assert.Equal(t, int64(4), object.Size)

	_, err = store.GetRange(context.Background(), "video.mp4", "bytes=0-1\r\nX-Evil: yes")
	assert.ErrorIs(t, err, ErrRangeNotSatisfiable)
}

func TestHTTPStoreCanaryRoundTrip(t *testing.T) {
	payload := []byte("agentcut blob canary\n")
	var stored []byte
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method)
		assert.Equal(t, "/bucket/agentcut-e2e/alice/run/canary.txt", r.URL.Path)
		switch r.Method {
		case http.MethodPut:
			assert.Equal(t, "text/plain", r.Header.Get("Content-Type"))
			var err error
			stored, err = io.ReadAll(r.Body)
			require.NoError(t, err)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			if r.Header.Get("Range") == "" {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(stored)
				return
			}
			assert.Equal(t, "bytes=1-3", r.Header.Get("Range"))
			w.Header().Set("Content-Range", "bytes 1-3/21")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(stored[1:4])
		case http.MethodDelete:
			stored = nil
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	store := NewStore(Config{Endpoint: server.URL + "/", Bucket: "/bucket/"})
	assert.Equal(t, server.URL+"/bucket/object", store.objectURL("/object"))
	result, err := RunCanary(
		context.Background(),
		store,
		"agentcut-e2e/alice/run/canary.txt",
		payload,
	)
	require.NoError(t, err)
	assert.Equal(t, "bytes 1-3/21", result.ContentRange)
	assert.Equal(t, []string{"PUT", "GET", "GET", "DELETE"}, calls)
	assert.Nil(t, stored)
}

func TestHTTPStoreCanaryReportsFailureAndAttemptsCleanup(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method)
		if r.Method == http.MethodPut {
			http.Error(w, "signature required", http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	_, err := RunCanary(
		context.Background(),
		NewStore(Config{Endpoint: server.URL, Bucket: "bucket"}),
		"agentcut-e2e/alice/run/canary.txt",
		[]byte("canary"),
	)
	require.ErrorContains(t, err, "HTTP 403")
	assert.Equal(t, []string{"PUT", "DELETE"}, calls)
}

func TestDeleteMediaBlobValidatesScopeAndIsIdempotent(t *testing.T) {
	store, err := NewLocalStore(t.TempDir())
	require.NoError(t, err)
	ctx := context.Background()
	key := "agentcut/p1/source/m1/a.mp4"
	for _, ownedKey := range []string{key, ThumbnailKey("p1", "m1"), ASRAudioKey("p1", "m1")} {
		_, err = store.Put(ctx, ownedKey, bytes.NewBufferString("x"), 1, "application/octet-stream")
		require.NoError(t, err)
	}
	activities := &CleanupActivities{Store: store}
	req := entity.DeleteMediaBlobRequest{ProjectID: "p1", MediaID: "m1", BlobKey: key}
	require.NoError(t, activities.DeleteMediaBlob(ctx, req))
	require.NoError(t, activities.DeleteMediaBlob(ctx, req))
	for _, ownedKey := range []string{key, ThumbnailKey("p1", "m1"), ASRAudioKey("p1", "m1")} {
		_, err = store.Get(ctx, ownedKey)
		assert.ErrorIs(t, err, os.ErrNotExist)
	}

	externalKey := "agentk/artifacts/shared.mp4"
	_, err = store.Put(ctx, externalKey, bytes.NewBufferString("x"), 1, "video/mp4")
	require.NoError(t, err)
	req.BlobKey = externalKey
	require.NoError(t, activities.DeleteMediaBlob(ctx, req))
	external, err := store.Get(ctx, externalKey)
	require.NoError(t, err, "Agent-K owned source must not be deleted")
	require.NoError(t, external.Body.Close())
}

func TestPathDerivedMediaKeysRejectUnsafeIDs(t *testing.T) {
	for _, mediaID := range []string{"../escape", "a/b", strings.Repeat("x", 129)} {
		assert.Empty(t, SourceKey("p1", mediaID, "a.mp4"))
		assert.Empty(t, ThumbnailKeyForGeneration("p1", mediaID, 1))
		assert.Empty(t, ASRAudioKeyForGeneration("p1", mediaID, 1))
		assert.Empty(t, AudioPreviewKeyForGeneration("p1", mediaID, 1))
	}
}
