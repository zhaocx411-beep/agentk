// Package audioextract provides constrained extraction of audio from media.
package audioextract

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"agentk/internal/artifact"
	"agentk/internal/mediaffmpeg"
	"agentk/internal/temporalx"
	"agentk/internal/tool"
)

const (
	ToolName     = "extract_audio"
	ActivityName = "agentk.capability.extract_audio"
)

type ActivityInput struct {
	VideoURL        string  `json:"videoUrl"`
	StartSeconds    float64 `json:"startSeconds,omitempty"`
	DurationSeconds float64 `json:"durationSeconds,omitempty"`
	Format          string  `json:"format,omitempty"`
	BitRate         string  `json:"bitRate,omitempty"`
	SampleRate      int     `json:"sampleRate,omitempty"`
	Channels        int     `json:"channels,omitempty"`
	Normalize       bool    `json:"normalizeLoudness,omitempty"`
}

type ActivityOutput struct {
	Artifacts    []tool.ResultArtifact `json:"artifacts"`
	DurationSecs float64               `json:"durationSeconds"`
	Format       string                `json:"format"`
	SampleRate   int                   `json:"sampleRate"`
	Channels     int                   `json:"channels"`
}

type Activity struct {
	storage artifact.Storage
}

func NewActivity(storage artifact.Storage) *Activity {
	return &Activity{storage: storage}
}

func BuildTool() tool.Spec {
	return tool.Spec{
		Name: ToolName,
		Description: "从一个含音轨的视频中提取真实音频 artifact，可选时间范围、MP3/AAC/WAV、采样率、声道与响度归一化。" +
			"它只生成音频文件，不做语音识别；需要文字内容请把结果交给 transcribe_audio。",
		InputSchema: json.RawMessage(`{
			"type":"object","required":["videoUrl"],
			"properties":{
				"videoUrl":{"type":"string","description":"系统 artifact URL 或执行机器可下载的公网 HTTP/HTTPS 视频 URL"},
				"startSeconds":{"type":"number","minimum":0,"description":"可选起始秒数；缺省从头开始"},
				"durationSeconds":{"type":"number","exclusiveMinimum":0,"description":"可选提取时长；缺省提取到结尾"},
				"format":{"type":"string","enum":["mp3","aac","wav"],"default":"mp3","description":"aac 以 M4A 容器交付"},
				"bitRate":{"type":"string","enum":["96k","128k","192k","256k"],"default":"192k","description":"MP3/AAC 码率；WAV 忽略"},
				"sampleRate":{"type":"integer","enum":[16000,24000,44100,48000],"default":44100,"description":"输出采样率"},
				"channels":{"type":"integer","enum":[1,2],"default":2,"description":"1 为单声道，2 为立体声"},
				"normalizeLoudness":{"type":"boolean","default":false,"description":"是否使用 loudnorm 做响度归一化"}
			}
		}`),
		Binding: tool.ActivityBinding{
			ActivityName: ActivityName, StartToCloseTimeout: 11 * time.Minute, MaxAttempts: 2,
		},
	}
}

func (a *Activity) Run(ctx context.Context, in ActivityInput) (ActivityOutput, error) {
	const op = "capability extract_audio"
	if a.storage == nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("audioextract storage is required")))
	}
	if strings.TrimSpace(in.VideoURL) == "" {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("videoUrl is required"))
	}
	bins, err := mediaffmpeg.Locate()
	if err != nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op, temporalx.ConfigurationError(err))
	}
	ws, err := mediaffmpeg.NewWorkspace("extract-audio")
	if err != nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op, err)
	}
	defer func() { _ = ws.Close() }()
	inputPath := ws.Path("input")
	if _, err := (mediaffmpeg.Resolver{Storage: a.storage}).Materialize(ctx, in.VideoURL, inputPath); err != nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op, err)
	}
	info, err := mediaffmpeg.Probe(ctx, bins.FFprobe, inputPath)
	if err != nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op, err)
	}
	if !info.HasAudio {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("input has no audio stream"))
	}
	args, format, extension, mimeType, sampleRate, channels, err := buildArgs(in, inputPath, ws.Path("output"))
	if err != nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op, temporalx.InvalidActivityInput("%v", err))
	}
	outputPath := ws.Path("output." + extension)
	args[len(args)-1] = outputPath
	if err := mediaffmpeg.Run(ctx, bins.FFmpeg, args); err != nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op, err)
	}
	outInfo, err := mediaffmpeg.Probe(ctx, bins.FFprobe, outputPath)
	if err != nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op, err)
	}
	data, err := mediaffmpeg.ReadOutput(outputPath)
	if err != nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op, err)
	}
	object, err := a.storage.Put(ctx, outputKey(ctx, extension), data, mimeType)
	if err != nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op+": store audio", err)
	}
	return ActivityOutput{
		Artifacts: []tool.ResultArtifact{{
			Type: "audio", URI: object.URL, Description: describe(in, format),
		}},
		DurationSecs: outInfo.Duration, Format: format,
		SampleRate: sampleRate, Channels: channels,
	}, nil
}

func describe(in ActivityInput, format string) string {
	scope := "完整音轨"
	if in.StartSeconds > 0 || in.DurationSeconds > 0 {
		end := "结尾"
		if in.DurationSeconds > 0 {
			end = number(in.StartSeconds+in.DurationSeconds) + " 秒"
		}
		scope = fmt.Sprintf("%s～%s", number(in.StartSeconds), end)
	}
	return fmt.Sprintf("使用 extract_audio 从视频 %s 提取%s并生成 %s 音频",
		in.VideoURL, scope, strings.ToUpper(format))
}

func buildArgs(in ActivityInput, inputPath, outputPath string) ([]string, string, string, string, int, int, error) {
	if in.StartSeconds < 0 || in.DurationSeconds < 0 {
		return nil, "", "", "", 0, 0, fmt.Errorf("startSeconds and durationSeconds must not be negative")
	}
	format := in.Format
	if format == "" {
		format = "mp3"
	}
	codec, extension, mimeType := "", format, ""
	switch format {
	case "mp3":
		codec, mimeType = "libmp3lame", "audio/mpeg"
	case "aac":
		codec, extension, mimeType = "aac", "m4a", "audio/mp4"
	case "wav":
		codec, mimeType = "pcm_s16le", "audio/wav"
	default:
		return nil, "", "", "", 0, 0, fmt.Errorf("format must be mp3, aac, or wav")
	}
	bitRate := in.BitRate
	if bitRate == "" {
		bitRate = "192k"
	}
	switch bitRate {
	case "96k", "128k", "192k", "256k":
	default:
		return nil, "", "", "", 0, 0, fmt.Errorf("unsupported bitRate")
	}
	sampleRate := in.SampleRate
	if sampleRate == 0 {
		sampleRate = 44100
	}
	switch sampleRate {
	case 16000, 24000, 44100, 48000:
	default:
		return nil, "", "", "", 0, 0, fmt.Errorf("unsupported sampleRate")
	}
	channels := in.Channels
	if channels == 0 {
		channels = 2
	}
	if channels != 1 && channels != 2 {
		return nil, "", "", "", 0, 0, fmt.Errorf("channels must be 1 or 2")
	}
	args := []string{"-i", inputPath}
	if in.StartSeconds > 0 {
		args = append(args, "-ss", number(in.StartSeconds))
	}
	if in.DurationSeconds > 0 {
		args = append(args, "-t", number(in.DurationSeconds))
	}
	args = append(args, "-vn")
	if in.Normalize {
		args = append(args, "-af", "loudnorm")
	}
	args = append(args, "-c:a", codec, "-ar", strconv.Itoa(sampleRate), "-ac", strconv.Itoa(channels))
	if format != "wav" {
		args = append(args, "-b:a", bitRate)
	}
	args = append(args, outputPath)
	return args, format, extension, mimeType, sampleRate, channels, nil
}

func number(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func outputKey(ctx context.Context, extension string) string {
	sum := sha256.Sum256([]byte(temporalx.ActivityIdempotencyKey(ctx)))
	return fmt.Sprintf("capabilities/audio/extract/%x.%s", sum[:16], extension)
}
