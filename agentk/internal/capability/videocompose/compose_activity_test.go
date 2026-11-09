package videocompose

import (
	"context"
	"strings"
	"testing"

	"agentk/internal/artifact"
)

func TestBuildComposeToolDeclaration(t *testing.T) {
	spec := BuildComposeTool()
	if spec.Name != ComposeToolName {
		t.Fatalf("spec = %+v", spec)
	}
	if schema := string(spec.InputSchema); !strings.Contains(schema, "audioTracks") ||
		!strings.Contains(schema, "subtitleUrl") || !strings.Contains(schema, "subtitleSegments") ||
		!strings.Contains(schema, "videoUrls") {
		t.Fatalf("schema = %s", schema)
	}
	for _, removed := range []string{"audioUrl", "audioMode", "audioVolume", "loopAudio"} {
		if strings.Contains(string(spec.InputSchema), removed) {
			t.Fatalf("schema still contains removed field %q: %s", removed, spec.InputSchema)
		}
	}
}

func TestBuildPostProcessArgsMultipleTracksAndSubtitles(t *testing.T) {
	args := buildPostProcessArgs("video.mp4", []preparedAudioTrack{
		{
			AudioTrack: AudioTrack{
				StartSeconds: 1.5, TrimStartSeconds: 2, DurationSeconds: 3,
				Volume: float64Pointer(0.4), Loop: true,
			},
			Path: "voice.mp3",
		},
		{AudioTrack: AudioTrack{Volume: float64Pointer(0.2)}, Path: "bgm.mp3"},
	}, true, "/tmp/subtitle.srt", "output.mp4")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-stream_loop -1", "atrim=start=2:duration=3", "volume=0.4", "adelay=1500:all=1",
		"volume=0.2", "amix=inputs=3", "subtitles=filename=", "-map [v]", "-map [a]", "-c:v libx264",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args %q do not contain %q", joined, want)
		}
	}
}

func TestBuildPostProcessArgsCanRemoveAudio(t *testing.T) {
	args := buildPostProcessArgs("video.mp4", nil, false, "", "output.mp4")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-an") || strings.Contains(joined, "-map 0:a:0") {
		t.Fatalf("args = %q, want video-only output", joined)
	}
}

func TestComposeActivityRejectsInvalidTracksAndSubtitles(t *testing.T) {
	activity := NewComposeActivity(artifact.NewLocalStorage(t.TempDir(), "http://artifacts"))
	for _, input := range []ComposeActivityInput{
		{
			VideoURLs:   []string{"http://example.com/video.mp4"},
			AudioTracks: []AudioTrack{{URL: "", StartSeconds: -1}},
		},
		{
			VideoURLs:   []string{"http://example.com/video.mp4"},
			SubtitleURL: "http://example.com/sub.vtt", SubtitleFormat: "vtt",
		},
		{
			VideoURLs:        []string{"http://example.com/video.mp4"},
			SubtitleSegments: []SubtitleSegment{{StartSeconds: 2, EndSeconds: 1, Text: "bad"}},
		},
		{
			VideoURLs:        []string{"http://example.com/video.mp4"},
			SubtitleURL:      "http://example.com/sub.srt",
			SubtitleSegments: []SubtitleSegment{{StartSeconds: 0, EndSeconds: 1, Text: "duplicate"}},
		},
	} {
		if _, err := activity.Run(context.Background(), input); err == nil {
			t.Fatalf("input %+v unexpectedly accepted", input)
		}
	}
}

func TestRenderSRT(t *testing.T) {
	got := renderSRT([]SubtitleSegment{
		{StartSeconds: 1.25, EndSeconds: 3.5, Text: "第一句"},
	})
	for _, want := range []string{"1\n", "00:00:01,250 --> 00:00:03,500", "第一句"} {
		if !strings.Contains(got, want) {
			t.Fatalf("SRT %q does not contain %q", got, want)
		}
	}
}

func float64Pointer(value float64) *float64 {
	return &value
}
