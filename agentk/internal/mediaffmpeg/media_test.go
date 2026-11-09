package mediaffmpeg

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"agentk/internal/artifact"
)

func TestResolverReadsArtifactStorage(t *testing.T) {
	storage := artifact.NewLocalStorage(t.TempDir(), "http://artifacts")
	object, err := storage.Put(context.Background(), "inputs/demo.bin", []byte("demo"), "application/octet-stream")
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "materialized")
	size, err := (Resolver{Storage: storage}).Materialize(context.Background(), object.URL, dst)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if size != 4 || string(raw) != "demo" {
		t.Fatalf("size=%d data=%q", size, raw)
	}
}

func TestResolverEnforcesSizeLimit(t *testing.T) {
	storage := artifact.NewLocalStorage(t.TempDir(), "http://artifacts")
	object, err := storage.Put(context.Background(), "inputs/large.bin", []byte("12345"), "application/octet-stream")
	if err != nil {
		t.Fatal(err)
	}
	_, err = (Resolver{Storage: storage, MaxBytes: 4}).Materialize(
		context.Background(), object.URL, filepath.Join(t.TempDir(), "out"))
	if err == nil {
		t.Fatal("expected size limit error")
	}
}

func TestRunAndProbe(t *testing.T) {
	bins, err := Locate()
	if err != nil {
		t.Skip(err)
	}
	output := filepath.Join(t.TempDir(), "sample.mp4")
	err = Run(context.Background(), bins.FFmpeg, []string{
		"-f", "lavfi", "-i", "color=c=blue:s=320x240:d=0.2",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=0.2",
		"-shortest", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", output,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Fatal(err)
	}
	info, err := Probe(context.Background(), bins.FFprobe, output)
	if err != nil {
		t.Fatal(err)
	}
	if !info.HasVideo || !info.HasAudio || info.Width != 320 || info.Height != 240 || info.Duration <= 0 {
		t.Fatalf("info = %+v", info)
	}
}

func TestSupportsFilter(t *testing.T) {
	bins, err := Locate()
	if err != nil {
		t.Skip(err)
	}
	supported, err := SupportsFilter(context.Background(), bins.FFmpeg, "amix")
	if err != nil {
		t.Fatal(err)
	}
	if !supported {
		t.Fatal("ffmpeg build must provide amix")
	}
	supported, err = SupportsFilter(context.Background(), bins.FFmpeg, "agentk_filter_that_does_not_exist")
	if err != nil {
		t.Fatal(err)
	}
	if supported {
		t.Fatal("unknown filter reported as supported")
	}
}
