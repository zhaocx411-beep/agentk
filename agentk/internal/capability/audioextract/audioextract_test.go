package audioextract

import (
	"strings"
	"testing"
)

func TestBuildArgs(t *testing.T) {
	args, format, extension, mimeType, sampleRate, channels, err := buildArgs(ActivityInput{
		StartSeconds: 2, DurationSeconds: 3, Format: "mp3",
		BitRate: "128k", SampleRate: 16000, Channels: 1, Normalize: true,
	}, "in.mp4", "out.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if format != "mp3" || extension != "mp3" || mimeType != "audio/mpeg" ||
		sampleRate != 16000 || channels != 1 {
		t.Fatalf("metadata = %q %q %q %d %d", format, extension, mimeType, sampleRate, channels)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"-ss 2", "-t 3", "-vn", "loudnorm", "libmp3lame", "-ar 16000", "-ac 1"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args %q do not contain %q", joined, want)
		}
	}
}

func TestBuildArgsRejectsUnsupportedValues(t *testing.T) {
	cases := []ActivityInput{
		{Format: "flac"},
		{Format: "mp3", BitRate: "1m"},
		{Format: "mp3", SampleRate: 12345},
		{Format: "mp3", Channels: 8},
		{Format: "mp3", StartSeconds: -1},
	}
	for _, input := range cases {
		if _, _, _, _, _, _, err := buildArgs(input, "in", "out"); err == nil {
			t.Fatalf("input %+v unexpectedly accepted", input)
		}
	}
}
