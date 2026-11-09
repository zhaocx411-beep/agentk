package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"agentk/internal/artifact"
)

func TestBuildArtifactStorage(t *testing.T) {
	t.Run("local demo", func(t *testing.T) {
		t.Setenv("AGENTK_ARTIFACT_STORE", "local")
		t.Setenv("AGENTK_FILES_DIR", t.TempDir())
		storage, err := buildArtifactStorage(":9090")
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := storage.(*artifact.LocalStorage); !ok {
			t.Fatalf("storage = %T, want LocalStorage", storage)
		}
	})

	t.Run("blob is default", func(t *testing.T) {
		t.Setenv("AGENTK_ARTIFACT_STORE", "")
		t.Setenv("AGENTK_BLOB_ENDPOINT", "")
		t.Setenv("AGENTK_BLOB_BUCKET", "")
		if _, err := buildArtifactStorage(":8080"); err == nil {
			t.Fatal("expected missing BlobStore error")
		}
	})

	t.Run("partial blob config fails", func(t *testing.T) {
		t.Setenv("AGENTK_ARTIFACT_STORE", "blob")
		t.Setenv("AGENTK_BLOB_ENDPOINT", "http://blob.test")
		t.Setenv("AGENTK_BLOB_BUCKET", "")
		if _, err := buildArtifactStorage(":8080"); err == nil {
			t.Fatal("expected partial BlobStore config error")
		}
	})

	t.Run("explicit blob", func(t *testing.T) {
		t.Setenv("AGENTK_ARTIFACT_STORE", "blob")
		t.Setenv("AGENTK_BLOB_ENDPOINT", "http://blob.test")
		t.Setenv("AGENTK_BLOB_BUCKET", "bucket")
		storage, err := buildArtifactStorage(":8080")
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := storage.(*artifact.BlobStorage); !ok {
			t.Fatalf("storage = %T, want BlobStorage", storage)
		}
	})

	t.Run("unknown storage", func(t *testing.T) {
		t.Setenv("AGENTK_ARTIFACT_STORE", "memory")
		if _, err := buildArtifactStorage(":8080"); err == nil {
			t.Fatal("expected unknown artifact storage error")
		}
	})
}

func TestMountArtifactFiles(t *testing.T) {
	storage := artifact.NewLocalStorage(t.TempDir(), "http://local.test")
	if _, err := storage.Put(context.Background(), "org/image.txt", []byte("artifact-data"), "text/plain"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mountArtifactFiles(mux, storage)
	req := httptest.NewRequest(http.MethodGet, "/files/org/image.txt", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Result().Body)
	if rec.Code != http.StatusOK || string(body) != "artifact-data" {
		t.Fatalf("status=%d body=%q", rec.Code, body)
	}

	blob, err := artifact.NewBlobStorage(artifact.BlobConfig{Endpoint: "http://blob.test", Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	mux = http.NewServeMux()
	mountArtifactFiles(mux, blob)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("blob /files status=%d, want 404", rec.Code)
	}
}
