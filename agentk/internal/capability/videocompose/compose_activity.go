package videocompose

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"agentk/internal/artifact"
	"agentk/internal/mediaffmpeg"
	"agentk/internal/temporalx"
	"agentk/internal/tool"
)

type ComposeActivityInput struct {
	VideoURLs             []string          `json:"videoUrls"`
	Size                  string            `json:"size,omitempty"`
	AudioTracks           []AudioTrack      `json:"audioTracks,omitempty"`
	PreserveOriginalAudio *bool             `json:"preserveOriginalAudio,omitempty"`
	SubtitleURL           string            `json:"subtitleUrl,omitempty"`
	SubtitleFormat        string            `json:"subtitleFormat,omitempty"`
	SubtitleSegments      []SubtitleSegment `json:"subtitleSegments,omitempty"`
}

type AudioTrack struct {
	URL              string   `json:"url"`
	StartSeconds     float64  `json:"startSeconds,omitempty"`
	TrimStartSeconds float64  `json:"trimStartSeconds,omitempty"`
	DurationSeconds  float64  `json:"durationSeconds,omitempty"`
	Volume           *float64 `json:"volume,omitempty"`
	Loop             bool     `json:"loop,omitempty"`
}

type SubtitleSegment struct {
	StartSeconds float64 `json:"startSeconds"`
	EndSeconds   float64 `json:"endSeconds"`
	Text         string  `json:"text"`
}

type ComposeActivityOutput struct {
	Artifacts    []tool.ResultArtifact `json:"artifacts"`
	DurationSecs float64               `json:"durationSeconds"`
	ClipCount    int                   `json:"clipCount"`
	Width        int                   `json:"width"`
	Height       int                   `json:"height"`
}

type ComposeActivity struct {
	storage artifact.Storage
}

func NewComposeActivity(storage artifact.Storage) *ComposeActivity {
	return &ComposeActivity{storage: storage}
}

func (a *ComposeActivity) Run(ctx context.Context, in ComposeActivityInput) (ComposeActivityOutput, error) {
	const op = "capability compose_video"
	if a.storage == nil {
		return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("videocompose storage is required")))
	}
	if len(in.VideoURLs) == 0 || len(in.VideoURLs) > 20 {
		return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("videoUrls must contain between 1 and 20 items"))
	}
	if in.Size == "" {
		in.Size = outputSizeAuto
	}
	if in.Size != outputSizeAuto && in.Size != outputSize1080p {
		return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("size must be auto or 1080p"))
	}
	if len(in.AudioTracks) > 8 {
		return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("audioTracks must contain at most 8 items"))
	}
	for i, track := range in.AudioTracks {
		if strings.TrimSpace(track.URL) == "" || track.StartSeconds < 0 ||
			track.TrimStartSeconds < 0 || track.DurationSeconds < 0 ||
			(track.Volume != nil && (*track.Volume < 0 || *track.Volume > 2)) {
			return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op,
				temporalx.InvalidActivityInput("invalid audioTracks[%d]", i))
		}
	}
	if strings.TrimSpace(in.SubtitleURL) != "" && len(in.SubtitleSegments) > 0 {
		return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("subtitleUrl and subtitleSegments cannot be used together"))
	}
	if len(in.SubtitleSegments) > 500 {
		return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("subtitleSegments must contain at most 500 items"))
	}
	for i, segment := range in.SubtitleSegments {
		if segment.StartSeconds < 0 || segment.EndSeconds <= segment.StartSeconds ||
			strings.TrimSpace(segment.Text) == "" {
			return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op,
				temporalx.InvalidActivityInput("invalid subtitleSegments[%d]", i))
		}
	}
	if strings.TrimSpace(in.SubtitleURL) != "" {
		if in.SubtitleFormat == "" {
			in.SubtitleFormat = "srt"
		}
		if in.SubtitleFormat != "srt" && in.SubtitleFormat != "ass" {
			return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op,
				temporalx.InvalidActivityInput("subtitleFormat must be srt or ass"))
		}
	}

	bins, err := mediaffmpeg.Locate()
	if err != nil {
		return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op, temporalx.ConfigurationError(err))
	}
	if strings.TrimSpace(in.SubtitleURL) != "" || len(in.SubtitleSegments) > 0 {
		supported, err := mediaffmpeg.SupportsFilter(ctx, bins.FFmpeg, "subtitles")
		if err != nil {
			return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op, temporalx.ConfigurationError(err))
		}
		if !supported {
			return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op,
				temporalx.ConfigurationError(fmt.Errorf("ffmpeg subtitles filter is unavailable; install a build with libass")))
		}
	}

	// 第一阶段复用已有且经过验证的归一化与拼接实现。
	composed, err := concat(ctx, in.VideoURLs, in.Size)
	if err != nil {
		return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op, err)
	}
	ws, err := mediaffmpeg.NewWorkspace("compose-video")
	if err != nil {
		return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op, err)
	}
	defer func() { _ = ws.Close() }()
	outputPath := ws.Path("output.mp4")
	tracks := in.AudioTracks
	preserveOriginal := in.PreserveOriginalAudio == nil || *in.PreserveOriginalAudio
	needsPostProcess := len(tracks) > 0 || !preserveOriginal ||
		strings.TrimSpace(in.SubtitleURL) != "" || len(in.SubtitleSegments) > 0
	if !needsPostProcess {
		if err := os.WriteFile(outputPath, composed, 0o600); err != nil {
			return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op, err)
		}
	} else {
		basePath := ws.Path("base.mp4")
		if err := os.WriteFile(basePath, composed, 0o600); err != nil {
			return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op, err)
		}
		prepared := make([]preparedAudioTrack, 0, len(tracks))
		for i, track := range tracks {
			audioPath := ws.Path(fmt.Sprintf("audio-%d", i))
			if _, err := (mediaffmpeg.Resolver{Storage: a.storage}).Materialize(ctx, track.URL, audioPath); err != nil {
				return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op, err)
			}
			audioInfo, err := mediaffmpeg.Probe(ctx, bins.FFprobe, audioPath)
			if err != nil {
				return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op, err)
			}
			if !audioInfo.HasAudio {
				return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op,
					temporalx.InvalidActivityInput("audioTracks[%d] has no audio stream", i))
			}
			prepared = append(prepared, preparedAudioTrack{AudioTrack: track, Path: audioPath})
		}
		subtitlePath := ""
		if strings.TrimSpace(in.SubtitleURL) != "" {
			subtitlePath = ws.Path("subtitle." + in.SubtitleFormat)
			if _, err := (mediaffmpeg.Resolver{Storage: a.storage}).Materialize(ctx, in.SubtitleURL, subtitlePath); err != nil {
				return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op, err)
			}
		} else if len(in.SubtitleSegments) > 0 {
			subtitlePath = ws.Path("subtitle.srt")
			if err := os.WriteFile(subtitlePath, []byte(renderSRT(in.SubtitleSegments)), 0o600); err != nil {
				return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op, err)
			}
		}
		args := buildPostProcessArgs(basePath, prepared, preserveOriginal, subtitlePath, outputPath)
		if err := mediaffmpeg.Run(ctx, bins.FFmpeg, args); err != nil {
			return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op, err)
		}
	}
	outInfo, err := mediaffmpeg.Probe(ctx, bins.FFprobe, outputPath)
	if err != nil {
		return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op, err)
	}
	data, err := mediaffmpeg.ReadOutput(outputPath)
	if err != nil {
		return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op, err)
	}
	object, err := a.storage.Put(ctx, composeOutputKey(ctx), data, "video/mp4")
	if err != nil {
		return ComposeActivityOutput{}, temporalx.ClassifyActivityError(op+": store video", err)
	}
	return ComposeActivityOutput{
		Artifacts: []tool.ResultArtifact{{
			Type: "video", URI: object.URL, Description: composeDescription(in),
		}},
		DurationSecs: outInfo.Duration, ClipCount: len(in.VideoURLs),
		Width: outInfo.Width, Height: outInfo.Height,
	}, nil
}

func composeDescription(in ComposeActivityInput) string {
	extras := make([]string, 0, 2)
	if len(in.AudioTracks) > 0 {
		extras = append(extras, "混合音频")
	}
	if strings.TrimSpace(in.SubtitleURL) != "" || len(in.SubtitleSegments) > 0 {
		extras = append(extras, "添加字幕")
	}
	suffix := ""
	if len(extras) > 0 {
		suffix = "，并" + strings.Join(extras, "、")
	}
	return fmt.Sprintf("使用 compose_video 按顺序拼接 %d 段视频（%s）%s后生成",
		len(in.VideoURLs), strings.Join(in.VideoURLs, "、"), suffix)
}

type preparedAudioTrack struct {
	AudioTrack
	Path string
}

func buildPostProcessArgs(
	videoPath string,
	tracks []preparedAudioTrack,
	preserveOriginal bool,
	subtitlePath string,
	outputPath string,
) []string {
	args := []string{"-i", videoPath}
	for _, track := range tracks {
		if track.Loop {
			args = append(args, "-stream_loop", "-1")
		}
		args = append(args, "-i", track.Path)
	}

	var filters []string
	videoMap := "0:v:0"
	videoCodec := []string{"-c:v", "copy"}
	if subtitlePath != "" {
		filters = append(filters, fmt.Sprintf("[0:v]subtitles=filename='%s'[v]", escapeFilterPath(subtitlePath)))
		videoMap = "[v]"
		videoCodec = []string{"-c:v", "libx264", "-preset", "veryfast", "-crf", "23", "-pix_fmt", "yuv420p"}
	}

	audioMap := "0:a:0"
	audioCodec := []string{"-c:a", "copy"}
	removeAudio := !preserveOriginal && len(tracks) == 0
	if len(tracks) > 0 {
		baseVolume := "1"
		if !preserveOriginal {
			baseVolume = "0"
		}
		filters = append(filters, fmt.Sprintf("[0:a]volume=%s[basea]", baseVolume))
		mixInputs := []string{"[basea]"}
		for i, track := range tracks {
			inputIndex := i + 1
			var chain []string
			if track.TrimStartSeconds > 0 || track.DurationSeconds > 0 {
				trim := "atrim="
				if track.TrimStartSeconds > 0 {
					trim += "start=" + decimal(track.TrimStartSeconds)
				}
				if track.DurationSeconds > 0 {
					if track.TrimStartSeconds > 0 {
						trim += ":"
					}
					trim += "duration=" + decimal(track.DurationSeconds)
				}
				chain = append(chain, trim)
			}
			chain = append(chain, "asetpts=PTS-STARTPTS")
			volume := 1.0
			if track.Volume != nil {
				volume = *track.Volume
			}
			chain = append(chain, "volume="+decimal(volume))
			if track.StartSeconds > 0 {
				chain = append(chain, fmt.Sprintf("adelay=%d:all=1", int64(track.StartSeconds*1000)))
			}
			label := fmt.Sprintf("track%d", i)
			filters = append(filters, fmt.Sprintf("[%d:a]%s[%s]", inputIndex, strings.Join(chain, ","), label))
			mixInputs = append(mixInputs, "["+label+"]")
		}
		filters = append(filters, fmt.Sprintf("%samix=inputs=%d:duration=first:dropout_transition=2[a]",
			strings.Join(mixInputs, ""), len(mixInputs)))
		audioMap = "[a]"
		audioCodec = []string{"-c:a", "aac", "-b:a", "192k"}
	}

	if len(filters) > 0 {
		args = append(args, "-filter_complex", strings.Join(filters, ";"))
	}
	args = append(args, "-map", videoMap)
	if removeAudio {
		args = append(args, "-an")
	} else {
		args = append(args, "-map", audioMap)
	}
	args = append(args, videoCodec...)
	if !removeAudio {
		args = append(args, audioCodec...)
	}
	args = append(args, "-movflags", "+faststart", outputPath)
	return args
}

func escapeFilterPath(path string) string {
	path = strings.ReplaceAll(path, `\`, `\\`)
	path = strings.ReplaceAll(path, `:`, `\:`)
	return strings.ReplaceAll(path, `'`, `\'`)
}

func decimal(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func renderSRT(segments []SubtitleSegment) string {
	var out strings.Builder
	for i, segment := range segments {
		fmt.Fprintf(&out, "%d\n%s --> %s\n%s\n\n",
			i+1, srtTimestamp(segment.StartSeconds), srtTimestamp(segment.EndSeconds),
			strings.TrimSpace(segment.Text))
	}
	return out.String()
}

func srtTimestamp(seconds float64) string {
	duration := time.Duration(seconds * float64(time.Second))
	hours := duration / time.Hour
	duration -= hours * time.Hour
	minutes := duration / time.Minute
	duration -= minutes * time.Minute
	secs := duration / time.Second
	millis := (duration - secs*time.Second) / time.Millisecond
	return fmt.Sprintf("%02d:%02d:%02d,%03d", hours, minutes, secs, millis)
}

func composeOutputKey(ctx context.Context) string {
	sum := sha256.Sum256([]byte(temporalx.ActivityIdempotencyKey(ctx)))
	return fmt.Sprintf("capabilities/videos/compose/%x.mp4", sum[:16])
}
