package mediae2e

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"agentk/internal/artifact"
	"agentk/internal/capability/audioextract"
	"agentk/internal/capability/frameextract"
	"agentk/internal/capability/videocompose"
	"agentk/internal/capability/videoprocess"
	"agentk/internal/mediaffmpeg"
)

func TestMediaCapabilitiesEndToEnd(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not in PATH")
	}
	root := t.TempDir()
	inputPath := filepath.Join(root, "input.mp4")
	cmd := exec.Command(ffmpeg,
		"-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "color=c=blue:s=320x240:d=1",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-shortest", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", inputPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate fixture: %v: %s", err, output)
	}
	mux := http.NewServeMux()
	mux.Handle("/files/", http.StripPrefix("/files/", http.FileServer(http.Dir(root))))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	storage := artifact.NewLocalStorage(root, server.URL)
	inputURL := server.URL + "/files/input.mp4"
	var subtitleSegments []videocompose.SubtitleSegment
	if supported, err := mediaffmpeg.SupportsFilter(context.Background(), ffmpeg, "subtitles"); err != nil {
		t.Fatal(err)
	} else if supported {
		subtitleSegments = []videocompose.SubtitleSegment{{
			StartSeconds: 0, EndSeconds: 0.8, Text: "agent-k",
		}}
	} else {
		t.Log("ffmpeg has no subtitles filter; multi-track path is tested, subtitle argv is covered by unit tests")
	}

	processed, err := videoprocess.NewActivity(storage).Run(context.Background(), videoprocess.ActivityInput{
		VideoURL: inputURL,
		Operations: []videoprocess.Operation{
			{Type: "trim", StartSeconds: 0.1, DurationSeconds: 0.5},
			{Type: "reframe", Width: 160, Height: 240, Fit: "cover"},
			{Type: "normalize"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	audio, err := audioextract.NewActivity(storage).Run(context.Background(), audioextract.ActivityInput{
		VideoURL: inputURL, Format: "wav", DurationSeconds: 0.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	frame, err := frameextract.NewActivity(storage).Run(context.Background(), frameextract.ActivityInput{
		VideoURL: inputURL, TimestampSeconds: 0.2, Format: "jpg", Width: 120,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(processed.Artifacts) != 1 || len(audio.Artifacts) != 1 || len(frame.Artifacts) != 1 {
		t.Fatalf("unexpected artifacts: processed=%+v audio=%+v frame=%+v",
			processed.Artifacts, audio.Artifacts, frame.Artifacts)
	}
	processedArtifact := processed.Artifacts[0]
	audioArtifact := audio.Artifacts[0]
	frameArtifact := frame.Artifacts[0]
	loop := false
	voiceVolume := 0.6
	bgmVolume := 0.2
	preserveOriginal := false
	composed, err := videocompose.NewComposeActivity(storage).Run(context.Background(), videocompose.ComposeActivityInput{
		VideoURLs: []string{processedArtifact.URI, processedArtifact.URI},
		AudioTracks: []videocompose.AudioTrack{
			{URL: audioArtifact.URI, Volume: &voiceVolume, Loop: loop},
			{URL: audioArtifact.URI, StartSeconds: 0.2, Volume: &bgmVolume, Loop: loop},
		},
		PreserveOriginalAudio: &preserveOriginal,
		SubtitleSegments:      subtitleSegments,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(composed.Artifacts) != 1 {
		t.Fatalf("composed artifacts = %+v", composed.Artifacts)
	}
	composedArtifact := composed.Artifacts[0]
	for name, description := range map[string]string{
		"processed": processedArtifact.Description,
		"audio":     audioArtifact.Description,
		"frame":     frameArtifact.Description,
		"composed":  composedArtifact.Description,
	} {
		if description == "" {
			t.Fatalf("%s artifact has no description", name)
		}
	}

	for name, uri := range map[string]string{
		"processed": processedArtifact.URI,
		"audio":     audioArtifact.URI,
		"frame":     frameArtifact.URI,
		"composed":  composedArtifact.URI,
	} {
		object, ok := storage.Resolve(uri)
		if !ok {
			t.Fatalf("%s URL is not in artifact storage: %s", name, uri)
		}
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(object.Key)))
		if err != nil || info.Size() == 0 {
			t.Fatalf("%s artifact invalid: info=%v err=%v", name, info, err)
		}
	}
}
