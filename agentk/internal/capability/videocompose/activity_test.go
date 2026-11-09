package videocompose

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.temporal.io/sdk/temporal"

	"agentk/internal/artifact"
)

func clipName(i int) string {
	return []string{"red.mp4", "blue.mp4"}[i]
}

func TestComposeActivityValidation(t *testing.T) {
	ctx := context.Background()
	a := NewActivity(artifact.NewLocalStorage(t.TempDir(), "http://x"))

	_, err := a.Run(ctx, ActivityInput{})
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) || !appErr.NonRetryable() {
		t.Fatalf("err = %v, want non-retryable", err)
	}
	_, err = a.Run(ctx, ActivityInput{VideoURLs: []string{"http://x/video.mp4"}, Size: "4k"})
	if !errors.As(err, &appErr) || !appErr.NonRetryable() {
		t.Fatalf("invalid size err = %v, want non-retryable", err)
	}
}

// TestComposeActivity 端到端（宿主机没有 ffmpeg 时跳过）：拼接 → 落盘 → 输出映射。
func TestComposeActivity(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not in PATH")
	}
	srcDir := t.TempDir()
	for i, spec := range []struct {
		video string
		audio string
	}{
		{video: "color=c=red:s=320x240:d=1", audio: "sine=frequency=440:duration=1"},
		{video: "color=c=blue:s=640x360:d=1"},
	} {
		out := filepath.Join(srcDir, clipName(i))
		args := []string{"-y", "-hide_banner", "-loglevel", "error",
			"-f", "lavfi", "-i", spec.video}
		if spec.audio != "" {
			args = append(args, "-f", "lavfi", "-i", spec.audio, "-c:a", "aac", "-shortest")
		}
		args = append(args, "-pix_fmt", "yuv420p", out)
		cmd := exec.Command(ffmpeg, args...)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("generate clip %d: %v: %s", i, err, b)
		}
	}
	srv := httptest.NewServer(http.FileServer(http.Dir(srcDir)))
	t.Cleanup(srv.Close)

	outDir := t.TempDir()
	a := NewActivity(artifact.NewLocalStorage(outDir, srv.URL))
	out, err := a.Run(context.Background(), ActivityInput{
		VideoURLs: []string{srv.URL + "/" + clipName(0), srv.URL + "/" + clipName(1)},
		Size:      outputSize1080p,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ClipCount != 2 || len(out.Artifacts) != 1 ||
		!strings.Contains(out.Artifacts[0].URI, "/capabilities/videos/") ||
		!strings.HasSuffix(out.Artifacts[0].URI, ".mp4") {
		t.Fatalf("out = %+v", out)
	}
	files, err := filepath.Glob(filepath.Join(outDir, "capabilities", "videos", "*.mp4"))
	if err != nil || len(files) != 1 {
		t.Fatalf("composed files = %v, err = %v", files, err)
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Fatalf("ffprobe must be installed with ffmpeg: %v", err)
	}
	probe := exec.Command(ffprobe, "-v", "error", "-select_streams", "a:0",
		"-show_entries", "stream=codec_type", "-of", "csv=p=0", files[0])
	raw, err := probe.Output()
	if err != nil || strings.TrimSpace(string(raw)) != "audio" {
		t.Fatalf("composed output has no audio stream: output=%q err=%v", raw, err)
	}
	probe = exec.Command(ffprobe, "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width,height", "-of", "csv=s=x:p=0", files[0])
	raw, err = probe.Output()
	if err != nil || strings.TrimSpace(string(raw)) != "1440x1080" {
		t.Fatalf("composed output resolution = %q, want 1440x1080: err=%v", raw, err)
	}
}

func TestOutputDimensions(t *testing.T) {
	for _, tc := range []struct {
		name          string
		input         clipInfo
		size          string
		width, height int
	}{
		{name: "auto", input: clipInfo{width: 720, height: 1280}, size: outputSizeAuto, width: 720, height: 1280},
		{name: "portrait", input: clipInfo{width: 720, height: 1280}, size: outputSize1080p, width: 1080, height: 1920},
		{name: "landscape", input: clipInfo{width: 1280, height: 720}, size: outputSize1080p, width: 1920, height: 1080},
		{name: "square", input: clipInfo{width: 1024, height: 1024}, size: outputSize1080p, width: 1080, height: 1080},
		{name: "four-three", input: clipInfo{width: 640, height: 480}, size: outputSize1080p, width: 1440, height: 1080},
		{name: "ultrawide", input: clipInfo{width: 2560, height: 1080}, size: outputSize1080p, width: 2560, height: 1080},
		{name: "rotated", input: clipInfo{width: 1920, height: 1080, rotation: 90}, size: outputSizeAuto, width: 1080, height: 1920},
	} {
		t.Run(tc.name, func(t *testing.T) {
			width, height := outputDimensions(tc.input, tc.size)
			if width != tc.width || height != tc.height {
				t.Fatalf("outputDimensions(%dx%d) = %dx%d, want %dx%d",
					tc.input.width, tc.input.height, width, height, tc.width, tc.height)
			}
		})
	}
}
