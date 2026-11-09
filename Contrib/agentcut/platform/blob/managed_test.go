package blob

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveManagedKeyRejectsUntrustedURLs(t *testing.T) {
	t.Setenv("AGENTCUT_BLOB_ENDPOINT", "https://blob.example")
	t.Setenv("AGENTCUT_BLOB_BUCKET", "bucket")
	t.Setenv("AGENTCUT_BLOB_CDN_BASE", "https://cdn.example/assets")
	t.Setenv("AGENTCUT_BLOB_URL_ALLOWLIST", "https://media.example/managed")

	for uri, want := range map[string]string{
		"https://blob.example/bucket/agentcut/p1/source/m1/a.mp4": "agentcut/p1/source/m1/a.mp4",
		"https://cdn.example/assets/agentcut/p1/source/m1/a.mp4":  "agentcut/p1/source/m1/a.mp4",
		"https://media.example/managed/agentk/artifacts/a.mp4":    "agentk/artifacts/a.mp4",
	} {
		got, err := ResolveManagedKey("", uri)
		require.NoError(t, err, uri)
		assert.Equal(t, want, got)
		assert.True(t, IsManagedHTTPURI(uri))
	}

	for _, uri := range []string{
		"http://127.0.0.1/internal",
		"https://evil.example/bucket/a.mp4",
		"https://blob.example/other/a.mp4",
		"https://blob.example/bucket/a.mp4?token=secret",
		"file:///etc/passwd",
	} {
		_, err := ResolveManagedKey("", uri)
		assert.Error(t, err, uri)
		assert.False(t, IsManagedHTTPURI(uri))
	}

	assert.Equal(t, "agentk/artifacts/a.mp4", mustManagedKey(t, "agentk/artifacts/a.mp4"))
	assert.Equal(t, "https://cdn.example/assets/agentk/artifacts/a.mp4",
		ManagedHTTPURI("agentk/artifacts/a.mp4", ""))
	for _, key := range []string{"../secret", "/absolute", "a/../../secret", "https://evil.example/x"} {
		_, err := ResolveManagedKey(key, "https://evil.example/x")
		assert.Error(t, err, key)
	}
}

func TestResolveAgentMediaKeyEnforcesSourceNamespace(t *testing.T) {
	t.Setenv("AGENTCUT_BLOB_ENDPOINT", "https://blob.example")
	t.Setenv("AGENTCUT_BLOB_BUCKET", "bucket")
	t.Setenv("AGENTCUT_AGENTK_ARTIFACT_PREFIX", "agentk/artifacts")

	got, err := ResolveAgentMediaKey("p1", "m1",
		"https://blob.example/bucket/agentk/artifacts/run/output.mp4")
	require.NoError(t, err)
	assert.Equal(t, "agentk/artifacts/run/output.mp4", got)

	got, err = ResolveAgentMediaKey("p1", "m1",
		"https://blob.example/bucket/agentcut/p1/source/m1/video.mp4")
	require.NoError(t, err)
	assert.Equal(t, "agentcut/p1/source/m1/video.mp4", got)

	_, err = ResolveAgentMediaKey("p1", "m1",
		"https://blob.example/bucket/agentcut/p2/source/m1/video.mp4")
	require.Error(t, err)
	_, err = ResolveAgentMediaKey("p1", "../escape",
		"https://blob.example/bucket/agentk/artifacts/output.mp4")
	require.Error(t, err)
}

func TestResolveRenderSourceKeyUsesOnlyRegisteredBlobKey(t *testing.T) {
	key, err := ResolveRenderSourceKey("p1", "m1", "agentcut/p1/source/m1/video.mp4")
	require.NoError(t, err)
	assert.Equal(t, "agentcut/p1/source/m1/video.mp4", key)

	_, err = ResolveRenderSourceKey("p1", "m1", "")
	require.Error(t, err)
	_, err = ResolveRenderSourceKey("p1", "m1", "agentcut/p2/source/m1/video.mp4")
	require.Error(t, err)
	_, err = ResolveRenderSourceKey("p1", "m1", "agentk/artifacts/other-project.mp4")
	require.Error(t, err)
}

func mustManagedKey(t *testing.T, key string) string {
	t.Helper()
	got, err := ResolveManagedKey(key, "")
	require.NoError(t, err)
	return got
}
