package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxFFmpegSourceBytes  = int64(2 << 30)
	maxCommandOutputBytes = int64(64 << 10)
	maxProbeOutputBytes   = int64(1 << 20)
	maxThumbnailBytes     = int64(10 << 20)
	maxASROutputBytes     = int64(25 << 20)
	maxWaveformPCMBytes   = int64(400 << 20)
	maxMediaDurationSec   = 6 * 60 * 60
)

const (
	// MaxFFmpegSourceBytes and MaxMediaDurationSec are shared safety limits for
	// upload preparation and local rendering.
	MaxFFmpegSourceBytes = maxFFmpegSourceBytes
	MaxMediaDurationSec  = maxMediaDurationSec
)

var ffmpegGate = struct {
	sync.RWMutex
	ch chan struct{}
}{ch: make(chan struct{}, 2)}

// ConfigureFFmpegConcurrency changes the shared in-process ffmpeg/ffprobe
// limit. Deployments with multiple worker processes still need an external
// task-queue/container concurrency limit; this semaphore only protects one
// process.
func ConfigureFFmpegConcurrency(limit int) {
	if limit < 1 {
		limit = 1
	}
	ffmpegGate.Lock()
	ffmpegGate.ch = make(chan struct{}, limit)
	ffmpegGate.Unlock()
}

func FFmpegConcurrencyFromEnv() int {
	limit, err := strconv.Atoi(strings.TrimSpace(os.Getenv("AGENTCUT_FFMPEG_MAX_CONCURRENCY")))
	if err != nil || limit < 1 || limit > 64 {
		return 2
	}
	return limit
}

func acquireFFmpeg(ctx context.Context) (func(), error) {
	ffmpegGate.RLock()
	ch := ffmpegGate.ch
	ffmpegGate.RUnlock()
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func validateSourceFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat media source: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxFFmpegSourceBytes {
		return fmt.Errorf("media source size %d is outside (0, 2 GiB]", info.Size())
	}
	return nil
}

func validateMediaDuration(durationSec float64) error {
	if durationSec <= 0 || durationSec > maxMediaDurationSec {
		return fmt.Errorf("media duration must be within (0, %d] seconds", maxMediaDurationSec)
	}
	return nil
}

// ValidateFFmpegSource applies the shared regular-file and 2 GiB limit.
func ValidateFFmpegSource(path string) error { return validateSourceFile(path) }

// ValidateMediaDuration applies the shared six-hour duration limit.
func ValidateMediaDuration(durationSec float64) error { return validateMediaDuration(durationSec) }

// RunFFmpeg executes one independent argv under the shared process gate,
// timeout, and 64 KiB diagnostic-output cap.
func RunFFmpeg(ctx context.Context, binary string, timeout time.Duration, args ...string) error {
	if strings.TrimSpace(binary) == "" {
		binary = "ffmpeg"
	}
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	release, err := acquireFFmpeg(runCtx)
	if err != nil {
		return fmt.Errorf("wait for ffmpeg slot: %w", err)
	}
	defer release()
	cmd := exec.CommandContext(runCtx, binary, args...)
	var output cappedBuffer
	output.max = maxCommandOutputBytes
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("ffmpeg timed out after %s", timeout)
		}
		if errors.Is(err, exec.ErrNotFound) {
			return fmt.Errorf("ffmpeg not found in PATH")
		}
		return fmt.Errorf("ffmpeg failed: %w: %s", err, output.String())
	}
	return nil
}

type cappedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int64
	n   int64
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.n += int64(len(p))
	remaining := b.max - int64(b.buf.Len())
	if remaining > 0 {
		write := int64(len(p))
		if write > remaining {
			write = remaining
		}
		_, _ = b.buf.Write(p[:write])
	}
	return len(p), nil
}

func (b *cappedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := strings.TrimSpace(b.buf.String())
	if b.n > b.max {
		out += " [output truncated]"
	}
	return out
}

func readFileLimited(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("generated output exceeds %d bytes", limit)
	}
	return raw, nil
}
