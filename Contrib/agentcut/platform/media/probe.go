// Package media contains server-side media inspection helpers.
package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"agentcut/timeline"
)

type Prober interface {
	Probe(ctx context.Context, path string) (timeline.MediaProbe, error)
}

type FFProber struct {
	Binary  string
	Timeout time.Duration
}

type ffprobeOutput struct {
	Streams []struct {
		CodecType  string `json:"codec_type"`
		CodecName  string `json:"codec_name"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
		RFrameRate string `json:"r_frame_rate"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func (p FFProber) Probe(ctx context.Context, path string) (timeline.MediaProbe, error) {
	if err := validateSourceFile(path); err != nil {
		return timeline.MediaProbe{}, err
	}
	binary := strings.TrimSpace(p.Binary)
	if binary == "" {
		binary = "ffprobe"
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	release, err := acquireFFmpeg(ctx)
	if err != nil {
		return timeline.MediaProbe{}, fmt.Errorf("wait for ffprobe slot: %w", err)
	}
	defer release()
	cmd := exec.CommandContext(ctx, binary,
		"-v", "error", "-show_entries",
		"format=duration:stream=codec_type,codec_name,width,height,r_frame_rate",
		"-of", "json", path)
	var stdout, stderr cappedBuffer
	stdout.max = maxProbeOutputBytes
	stderr.max = maxCommandOutputBytes
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return timeline.MediaProbe{}, fmt.Errorf("ffprobe timed out after %s", timeout)
		}
		if errors.Is(err, exec.ErrNotFound) {
			return timeline.MediaProbe{}, fmt.Errorf("ffprobe not found in PATH")
		}
		return timeline.MediaProbe{}, fmt.Errorf("ffprobe failed: %w: %s", err, stderr.String())
	}
	if stdout.n > maxProbeOutputBytes {
		return timeline.MediaProbe{}, fmt.Errorf("ffprobe output exceeds 1 MiB")
	}
	raw := stdout.buf.Bytes()
	var out ffprobeOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return timeline.MediaProbe{}, fmt.Errorf("decode ffprobe output: %w", err)
	}
	duration, err := strconv.ParseFloat(out.Format.Duration, 64)
	if err != nil || duration <= 0 {
		return timeline.MediaProbe{}, fmt.Errorf("ffprobe returned no positive duration")
	}
	if err := validateMediaDuration(duration); err != nil {
		return timeline.MediaProbe{}, err
	}
	probe := timeline.MediaProbe{DurationMs: int64(duration*1000 + 0.5)}
	for _, stream := range out.Streams {
		switch stream.CodecType {
		case "video":
			if probe.Width == 0 {
				probe.Width = stream.Width
				probe.Height = stream.Height
				probe.NativeFPS = parseRate(stream.RFrameRate)
				probe.Codec = stream.CodecName
			}
		case "audio":
			probe.HasAudio = true
			if probe.Codec == "" {
				probe.Codec = stream.CodecName
			}
		}
	}
	return probe, nil
}

func parseRate(raw string) float64 {
	parts := strings.Split(raw, "/")
	if len(parts) == 2 {
		n, nerr := strconv.ParseFloat(parts[0], 64)
		d, derr := strconv.ParseFloat(parts[1], 64)
		if nerr == nil && derr == nil && d != 0 {
			return n / d
		}
	}
	rate, _ := strconv.ParseFloat(raw, 64)
	return rate
}
