package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResourceLimitsAreEnforceable(t *testing.T) {
	require.Error(t, validateMediaDuration(0))
	require.Error(t, validateMediaDuration(maxMediaDurationSec+1))
	require.NoError(t, validateMediaDuration(maxMediaDurationSec))

	path := filepath.Join(t.TempDir(), "source")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
	require.NoError(t, validateSourceFile(path))
	raw, err := readFileLimited(path, 1)
	require.NoError(t, err)
	assert.Equal(t, "x", string(raw))
	require.Error(t, func() error {
		_, err := readFileLimited(path, 0)
		return err
	}())

	var output cappedBuffer
	output.max = 4
	_, err = output.Write([]byte("123456"))
	require.NoError(t, err)
	assert.Contains(t, output.String(), "truncated")
	assert.LessOrEqual(t, output.buf.Len(), 4)
}

func TestSharedFFmpegSemaphoreLimitsConcurrency(t *testing.T) {
	ConfigureFFmpegConcurrency(1)
	defer ConfigureFFmpegConcurrency(2)
	release, err := acquireFFmpeg(context.Background())
	require.NoError(t, err)
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = acquireFFmpeg(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestFFmpegConcurrencyConfiguration(t *testing.T) {
	t.Setenv("AGENTCUT_FFMPEG_MAX_CONCURRENCY", "7")
	assert.Equal(t, 7, FFmpegConcurrencyFromEnv())
	t.Setenv("AGENTCUT_FFMPEG_MAX_CONCURRENCY", "0")
	assert.Equal(t, 2, FFmpegConcurrencyFromEnv())
}
