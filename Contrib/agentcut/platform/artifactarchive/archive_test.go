package artifactarchive

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"agentcut/platform/blob"
	"agentcut/timeline"
)

type fakeVerifier struct {
	sessionID string
	uri       string
	kind      timeline.MediaKind
	err       error
}

func (v *fakeVerifier) VerifyArtifact(
	_ context.Context,
	sessionID, uri string,
	kind timeline.MediaKind,
) (Provenance, error) {
	v.sessionID, v.uri, v.kind = sessionID, uri, kind
	return Provenance{ExecutionID: "exe_1", ToolCallID: "call_1", Type: string(kind)}, v.err
}

func TestArchiveStreamsVerifiedArtifactIntoProjectNamespace(t *testing.T) {
	payload := bytes.Repeat([]byte("mp3"), 1024)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Content-Length", "3072")
		_, _ = w.Write(payload)
	}))
	defer source.Close()
	t.Setenv("AGENTCUT_AGENTK_ARTIFACT_URL_ALLOWLIST", source.URL)

	store, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	verifier := &fakeVerifier{}
	result, err := (&Archiver{Store: store, Verifier: verifier}).Archive(
		context.Background(), "p1", "ses_1",
		timeline.Media{MediaID: "m1", Kind: timeline.MediaAudio, URI: source.URL + "/generated.mp3"},
	)
	require.NoError(t, err)
	assert.Equal(t, "agentcut/p1/source/m1/audio.mp3", result.BlobKey)
	assert.Equal(t, "/v1/projects/p1/media/m1/content", result.URI)
	assert.Equal(t, "ses_1", verifier.sessionID)
	assert.Equal(t, source.URL+"/generated.mp3", verifier.uri)
	assert.Equal(t, timeline.MediaAudio, verifier.kind)

	object, err := store.Get(context.Background(), result.BlobKey)
	require.NoError(t, err)
	defer object.Body.Close()
	got, err := io.ReadAll(object.Body)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
}

func TestArchiveRejectsUnverifiedOrUnallowlistedArtifact(t *testing.T) {
	store, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	media := timeline.Media{
		MediaID: "m1", Kind: timeline.MediaAudio, URI: "https://untrusted.example/generated.mp3",
	}
	_, err = (&Archiver{Store: store, Verifier: &fakeVerifier{}}).
		Archive(context.Background(), "p1", "ses_1", media)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside")

	t.Setenv("AGENTCUT_AGENTK_ARTIFACT_URL_ALLOWLIST", "https://untrusted.example")
	_, err = (&Archiver{Store: store, Verifier: &fakeVerifier{err: errors.New("not in event log")}}).
		Archive(context.Background(), "p1", "ses_1", media)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provenance rejected")
}
