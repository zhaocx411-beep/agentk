package frameextract

import (
	"strings"
	"testing"
)

func TestBuildArgs(t *testing.T) {
	args, format, extension, mimeType, err := buildArgs(ActivityInput{
		TimestampSeconds: 2.5, Format: "jpg", Width: 720, Quality: 4,
	}, "in.mp4", "out.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if format != "jpg" || extension != "jpg" || mimeType != "image/jpeg" {
		t.Fatalf("metadata = %q %q %q", format, extension, mimeType)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"-ss 2.5", "-frames:v 1", "scale=720:-2", "-q:v 4"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args %q do not contain %q", joined, want)
		}
	}
}

func TestBuildArgsPNGDoesNotSetJPEGQuality(t *testing.T) {
	args, _, _, _, err := buildArgs(ActivityInput{Format: "png"}, "in", "out")
	if err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(args, " "); strings.Contains(joined, "-q:v") {
		t.Fatalf("PNG args unexpectedly contain JPEG quality: %q", joined)
	}
}

func TestBuildArgsRejectsInvalidValues(t *testing.T) {
	for _, input := range []ActivityInput{
		{TimestampSeconds: -1},
		{Format: "gif"},
		{Width: 1},
		{Format: "jpg", Quality: 32},
	} {
		if _, _, _, _, err := buildArgs(input, "in", "out"); err == nil {
			t.Fatalf("input %+v unexpectedly accepted", input)
		}
	}
}
