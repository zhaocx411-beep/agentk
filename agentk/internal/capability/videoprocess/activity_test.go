package videoprocess

import (
	"strings"
	"testing"

	"agentk/internal/mediaffmpeg"
)

func TestBuildArgs(t *testing.T) {
	args, err := buildArgs(ActivityInput{
		Operations: []Operation{
			{Type: "trim", StartSeconds: 1, DurationSeconds: 2},
			{Type: "reframe", Width: 1080, Height: 1920, Fit: "cover"},
			{Type: "normalize"},
		},
	}, mediaffmpeg.Info{Duration: 10, HasVideo: true, HasAudio: true}, "in.mp4", "out.mp4")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"trim=start=1:duration=2", "scale=1080:1920", "crop=1080:1920", "fps=30", "-c:v libx264", "-c:a aac"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args %q do not contain %q", joined, want)
		}
	}
}

func TestBuildArgsRejectsInvalidOperations(t *testing.T) {
	for _, operation := range []Operation{
		{Type: "trim", DurationSeconds: 0},
		{Type: "speed", Factor: 3},
		{Type: "reframe", Width: 1, Height: 1080},
		{Type: "raw_ffmpeg"},
	} {
		if _, err := buildArgs(ActivityInput{Operations: []Operation{operation}},
			mediaffmpeg.Info{Duration: 10, HasVideo: true}, "in", "out"); err == nil {
			t.Fatalf("operation %+v unexpectedly accepted", operation)
		}
	}
}

func TestBuildArgsMuteRemovesAudio(t *testing.T) {
	args, err := buildArgs(ActivityInput{Operations: []Operation{{Type: "mute"}}},
		mediaffmpeg.Info{HasVideo: true, HasAudio: true}, "in", "out")
	if err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(args, " "); !strings.Contains(joined, "-an") {
		t.Fatalf("args = %q, want -an", joined)
	}
}
