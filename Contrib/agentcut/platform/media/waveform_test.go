package media

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pcmSamples(values ...int16) []byte {
	out := make([]byte, 0, len(values)*2)
	buf := make([]byte, 2)
	for _, v := range values {
		binary.LittleEndian.PutUint16(buf, uint16(v))
		out = append(out, buf...)
	}
	return out
}

func TestFFWaveformExtractorComputesPeaks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	dir := t.TempDir()
	// WaveformBucketCount 个采样点，全部固定为半幅度（16384/32768=0.5），
	// bucketSize=1，每桶应该都算出 ~0.5。
	values := make([]int16, WaveformBucketCount)
	for i := range values {
		values[i] = 16384
	}
	script := fakeFFmpegScript(t, dir, pcmSamples(values...))
	input := filepath.Join(dir, "input.wav")
	require.NoError(t, os.WriteFile(input, []byte("source"), 0o600))

	peaks, err := (FFWaveformExtractor{Binary: script, Timeout: 5 * time.Second}).
		Extract(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, peaks, WaveformBucketCount)
	for _, p := range peaks {
		assert.InDelta(t, 0.5, p, 0.001)
	}
}

func TestFFWaveformExtractorTakesAbsolutePeakPerBucket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	dir := t.TempDir()
	// 2 * WaveformBucketCount 采样点 -> bucketSize=2；每对里一正一负，且负数
	// 幅度更大，验证取的是绝对值峰值而不是简单最大值。
	values := make([]int16, 0, WaveformBucketCount*2)
	for i := 0; i < WaveformBucketCount; i++ {
		values = append(values, 1000, -20000)
	}
	script := fakeFFmpegScript(t, dir, pcmSamples(values...))
	input := filepath.Join(dir, "input.wav")
	require.NoError(t, os.WriteFile(input, []byte("source"), 0o600))

	peaks, err := (FFWaveformExtractor{Binary: script, Timeout: 5 * time.Second}).
		Extract(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, peaks, WaveformBucketCount)
	for _, p := range peaks {
		assert.InDelta(t, float32(20000.0/32768.0), p, 0.001)
	}
}

func TestFFWaveformExtractorEmptyOutputErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	dir := t.TempDir()
	script := fakeFFmpegScript(t, dir, []byte{})
	input := filepath.Join(dir, "input.wav")
	require.NoError(t, os.WriteFile(input, []byte("source"), 0o600))

	_, err := (FFWaveformExtractor{Binary: script, Timeout: 5 * time.Second}).
		Extract(context.Background(), input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty pcm")
}

func TestFFWaveformExtractorMissingBinary(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.wav")
	require.NoError(t, os.WriteFile(input, []byte("source"), 0o600))
	_, err := (FFWaveformExtractor{Binary: "/nonexistent/ffmpeg-binary", Timeout: time.Second}).
		Extract(context.Background(), input)
	require.Error(t, err)
}
