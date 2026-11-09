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

func TestParseRate(t *testing.T) {
	assert.InDelta(t, 29.970, parseRate("30000/1001"), 0.001)
	assert.Equal(t, 25.0, parseRate("25"))
	assert.Zero(t, parseRate("0/0"))
}

func TestFFProberParsesOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "ffprobe")
	err := os.WriteFile(script, []byte(`#!/bin/sh
printf '%s' '{"streams":[{"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"r_frame_rate":"30000/1001"},{"codec_type":"audio","codec_name":"aac"}],"format":{"duration":"1.234"}}'
`), 0o750)
	require.NoError(t, err)
	input := filepath.Join(dir, "input.mp4")
	require.NoError(t, os.WriteFile(input, []byte("source"), 0o600))

	probe, err := (FFProber{Binary: script, Timeout: 5 * time.Second}).Probe(context.Background(), input)
	require.NoError(t, err)
	assert.Equal(t, int64(1234), probe.DurationMs)
	assert.Equal(t, 1920, probe.Width)
	assert.True(t, probe.HasAudio)
	assert.Equal(t, "h264", probe.Codec)
	assert.InDelta(t, 29.970, probe.NativeFPS, 0.001)
}
