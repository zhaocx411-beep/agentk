package videoprobe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"go.temporal.io/sdk/temporal"
)

func TestActivityValidation(t *testing.T) {
	activity := NewActivity(nil)
	for _, input := range []ActivityInput{
		{},
		{VideoURL: "/tmp/video.mp4"},
		{VideoURL: "file:///tmp/video.mp4"},
		{VideoURL: "http://127.0.0.1/private.mp4"},
	} {
		_, err := activity.Run(context.Background(), input)
		var appErr *temporal.ApplicationError
		if !errors.As(err, &appErr) || !appErr.NonRetryable() {
			t.Fatalf("input = %+v, err = %v, want non-retryable", input, err)
		}
	}
}

func TestDownloadVideoRejectsOversizedContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.FormatInt(maxVideoBytes+1, 10))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	if _, _, err := downloadVideo(context.Background(), server.Client(), server.URL, io.Discard); err == nil {
		t.Fatal("oversized video should be rejected")
	}
}

func TestActivity(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not in PATH")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not in PATH")
	}

	dir := t.TempDir()
	videoPath := filepath.Join(dir, "sample.mp4")
	cmd := exec.Command(ffmpeg,
		"-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "color=c=blue:s=320x240:r=25:d=1",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=1",
		"-c:v", "libx264", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-shortest",
		videoPath,
	)
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate sample: %v: %s", err, raw)
	}

	server := httptest.NewServer(http.FileServer(http.Dir(dir)))
	t.Cleanup(server.Close)
	videoURL := server.URL + "/sample.mp4"
	activity := NewActivity(nil)
	activity.client = server.Client()
	activity.allowPrivate = true
	out, err := activity.Run(context.Background(), ActivityInput{VideoURL: videoURL})
	if err != nil {
		t.Fatal(err)
	}
	if out.FileSizeBytes <= 0 || out.DurationSecs <= 0 ||
		len(out.VideoStreams) != 1 || len(out.AudioStreams) != 1 {
		t.Fatalf("output = %+v", out)
	}
	video := out.VideoStreams[0]
	if video.Width != 320 || video.Height != 240 || video.FrameRate != 25 {
		t.Fatalf("video = %+v", video)
	}
	if out.AudioStreams[0].SampleRate != 48000 {
		t.Fatalf("audio = %+v", out.AudioStreams[0])
	}
	var raw struct {
		Format struct {
			Filename string `json:"filename"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out.Raw, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.Format.Filename != videoURL {
		t.Fatalf("raw filename = %q, want %q", raw.Format.Filename, videoURL)
	}
}
