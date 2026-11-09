// Package mediaffmpeg provides the shared, constrained FFmpeg execution layer
// used by media capabilities. User input is materialized to local scratch files
// before it is passed to FFmpeg; callers never pass raw CLI arguments through.
package mediaffmpeg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"agentk/internal/artifact"
	"agentk/internal/netguard"
)

const MaxInputBytes int64 = 512 << 20

type Binaries struct {
	FFmpeg  string
	FFprobe string
}

func Locate() (Binaries, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return Binaries{}, fmt.Errorf("ffmpeg not found in PATH: %w", err)
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return Binaries{}, fmt.Errorf("ffprobe not found in PATH: %w", err)
	}
	return Binaries{FFmpeg: ffmpeg, FFprobe: ffprobe}, nil
}

func SupportsFilter(ctx context.Context, ffmpeg, name string) (bool, error) {
	cmd := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-filters")
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("list ffmpeg filters: %w", err)
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == name {
			return true, nil
		}
	}
	return false, nil
}

type Workspace struct {
	Dir string
}

func NewWorkspace(prefix string) (*Workspace, error) {
	dir, err := os.MkdirTemp("", "agentk-"+prefix+"-*")
	if err != nil {
		return nil, err
	}
	return &Workspace{Dir: dir}, nil
}

func (w *Workspace) Path(name string) string {
	return filepath.Join(w.Dir, filepath.Base(name))
}

func (w *Workspace) Close() error {
	return os.RemoveAll(w.Dir)
}

type Resolver struct {
	Storage      artifact.Storage
	Client       *http.Client
	AllowPrivate bool
	MaxBytes     int64
}

func (r Resolver) Materialize(ctx context.Context, uri, dst string) (int64, error) {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return 0, fmt.Errorf("media URL is required")
	}
	var reader io.ReadCloser
	if r.Storage != nil {
		if object, ok := r.Storage.Resolve(uri); ok {
			opened, err := r.Storage.Open(ctx, object)
			if err != nil {
				return 0, err
			}
			reader = opened
		}
	}
	if reader == nil {
		if !r.AllowPrivate {
			if _, err := netguard.ValidatePublicHTTPURL(ctx, uri); err != nil {
				return 0, fmt.Errorf("unsafe media URL: %w", err)
			}
		}
		client := r.Client
		if client == nil {
			client = http.DefaultClient
		}
		if !r.AllowPrivate {
			clone := *client
			clone.CheckRedirect = netguard.PublicRedirectPolicy(client.CheckRedirect)
			client = &clone
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
		if err != nil {
			return 0, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, err
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			_ = resp.Body.Close()
			return 0, fmt.Errorf("GET %s: status %d", uri, resp.StatusCode)
		}
		reader = resp.Body
	}
	defer func() { _ = reader.Close() }()

	maxBytes := r.MaxBytes
	if maxBytes <= 0 {
		maxBytes = MaxInputBytes
	}
	file, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	size, copyErr := io.Copy(file, io.LimitReader(reader, maxBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return 0, copyErr
	}
	if closeErr != nil {
		return 0, closeErr
	}
	if size > maxBytes {
		return 0, fmt.Errorf("media exceeds %d bytes", maxBytes)
	}
	return size, nil
}

type Info struct {
	Duration float64
	Width    int
	Height   int
	Rotation int
	HasVideo bool
	HasAudio bool
}

func Probe(ctx context.Context, ffprobe, path string) (Info, error) {
	cmd := exec.CommandContext(ctx, ffprobe,
		"-v", "error",
		"-show_entries", "stream=codec_type,width,height:stream_tags=rotate:stream_side_data=rotation:format=duration",
		"-of", "json", path)
	raw, err := cmd.Output()
	if err != nil {
		return Info{}, fmt.Errorf("ffprobe: %w", err)
	}
	var decoded struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
			Tags      struct {
				Rotate string `json:"rotate"`
			} `json:"tags"`
			SideData []struct {
				Rotation *int `json:"rotation"`
			} `json:"side_data_list"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return Info{}, fmt.Errorf("decode ffprobe output: %w", err)
	}
	info := Info{}
	info.Duration, _ = strconv.ParseFloat(decoded.Format.Duration, 64)
	for _, stream := range decoded.Streams {
		switch stream.CodecType {
		case "video":
			info.HasVideo = true
			if info.Width == 0 {
				info.Width, info.Height = stream.Width, stream.Height
				for _, side := range stream.SideData {
					if side.Rotation != nil {
						info.Rotation = *side.Rotation
						break
					}
				}
				if info.Rotation == 0 {
					info.Rotation, _ = strconv.Atoi(stream.Tags.Rotate)
				}
			}
		case "audio":
			info.HasAudio = true
		}
	}
	if !info.HasVideo && !info.HasAudio {
		return Info{}, fmt.Errorf("media has no audio or video stream")
	}
	return info, nil
}

func Run(ctx context.Context, ffmpeg string, args []string) error {
	base := []string{"-y", "-hide_banner", "-nostdin", "-loglevel", "error"}
	cmd := exec.CommandContext(ctx, ffmpeg, append(base, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, Tail(stderr.String(), 1200))
	}
	return nil
}

func ReadOutput(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() == 0 {
		return nil, fmt.Errorf("ffmpeg produced an empty output")
	}
	return os.ReadFile(path)
}

func Tail(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return "…" + value[len(value)-limit:]
}
