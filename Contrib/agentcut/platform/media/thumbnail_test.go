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

// fakeFFmpegScript 写一个假的 ffmpeg 可执行脚本：把最后一个参数（输出路径）
// 拷贝自同目录下的 fixture 文件，不真正调用 ffmpeg，纯粹验证参数拼接/输出读取
// 逻辑，和 probe_test.go 的 fakeffprobe fixture 手法一致。
func fakeFFmpegScript(t *testing.T, dir string, fixture []byte) string {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fixture.bin"), fixture, 0o640))
	script := filepath.Join(dir, "ffmpeg")
	require.NoError(t, os.WriteFile(script, []byte(`#!/bin/sh
shift $(($# - 1))
cp "$(dirname "$0")/fixture.bin" "$1"
`), 0o750))
	return script
}

func TestFFThumbnailerGenerateVideoSprite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	dir := t.TempDir()
	script := fakeFFmpegScript(t, dir, []byte("fake-jpeg-sprite"))
	input := filepath.Join(dir, "input.mp4")
	require.NoError(t, os.WriteFile(input, []byte("source"), 0o600))

	data, err := (FFThumbnailer{Binary: script, Timeout: 5 * time.Second}).
		GenerateVideoSprite(context.Background(), input, 12.0)
	require.NoError(t, err)
	assert.Equal(t, "fake-jpeg-sprite", string(data))
}

func TestFFThumbnailerRejectsNonPositiveDuration(t *testing.T) {
	_, err := (FFThumbnailer{}).GenerateVideoSprite(context.Background(), "ignored", 0)
	require.Error(t, err)
}

func TestFFThumbnailerGenerateImageThumbnail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	dir := t.TempDir()
	script := fakeFFmpegScript(t, dir, []byte("fake-image-thumb"))
	input := filepath.Join(dir, "input.png")
	require.NoError(t, os.WriteFile(input, []byte("source"), 0o600))

	data, err := (FFThumbnailer{Binary: script, Timeout: 5 * time.Second}).
		GenerateImageThumbnail(context.Background(), input)
	require.NoError(t, err)
	assert.Equal(t, "fake-image-thumb", string(data))
}

func TestFFThumbnailerMissingBinary(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.png")
	require.NoError(t, os.WriteFile(input, []byte("source"), 0o600))
	_, err := (FFThumbnailer{Binary: filepath.Join(t.TempDir(), "does-not-exist"), Timeout: time.Second}).
		GenerateImageThumbnail(context.Background(), input)
	require.Error(t, err)
}

func TestFFThumbnailerEmptyOutputErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	dir := t.TempDir()
	script := fakeFFmpegScript(t, dir, []byte{})
	input := filepath.Join(dir, "input.png")
	require.NoError(t, os.WriteFile(input, []byte("source"), 0o600))

	_, err := (FFThumbnailer{Binary: script, Timeout: 5 * time.Second}).
		GenerateImageThumbnail(context.Background(), input)
	require.Error(t, err)
}
