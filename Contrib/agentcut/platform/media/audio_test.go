package media

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFFAudioExtractorCreatesASRMP3(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	dir := t.TempDir()
	script := fakeFFmpegScript(t, dir, []byte("fake-mp3"))
	input := filepath.Join(dir, "video.mp4")
	require.NoError(t, os.WriteFile(input, []byte("source"), 0o600))
	output := filepath.Join(dir, "asr.mp3")
	err := (FFAudioExtractor{Binary: script, Timeout: 5 * time.Second}).
		ExtractForASR(context.Background(), input, output)
	require.NoError(t, err)
	raw, err := os.ReadFile(output)
	require.NoError(t, err)
	assert.Equal(t, "fake-mp3", string(raw))
}

func TestFFAudioExtractorMissingBinary(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "video.mp4")
	require.NoError(t, os.WriteFile(input, []byte("source"), 0o600))
	err := (FFAudioExtractor{
		Binary: filepath.Join(t.TempDir(), "missing-ffmpeg"), Timeout: time.Second,
	}).ExtractForASR(context.Background(), input, filepath.Join(t.TempDir(), "asr.mp3"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ffmpeg")
}

func TestFFAudioPreviewerCreatesBrowserMP3(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	dir := t.TempDir()
	script := fakeFFmpegScript(t, dir, []byte("browser-mp3"))
	input := filepath.Join(dir, "input.wav")
	require.NoError(t, os.WriteFile(input, []byte("source"), 0o600))
	output := filepath.Join(dir, "preview.mp3")

	err := (FFAudioPreviewer{Binary: script, Timeout: 5 * time.Second}).
		TranscodeForPreview(context.Background(), input, output)

	require.NoError(t, err)
	raw, err := os.ReadFile(output)
	require.NoError(t, err)
	assert.Equal(t, "browser-mp3", string(raw))
}
