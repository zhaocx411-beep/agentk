// Package frameextract extracts a real image artifact from a video.
package frameextract

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
	ToolName     = "extract_frame"
	ActivityName = "agentk.capability.extract_frame"
)

type ActivityInput struct {
	VideoURL         string  `json:"videoUrl"`
	TimestampSeconds float64 `json:"timestampSeconds"`
	Format           string  `json:"format,omitempty"`
	Width            int     `json:"width,omitempty"`
	Quality          int     `json:"quality,omitempty"`
}

type ActivityOutput struct {
	Artifacts        []tool.ResultArtifact `json:"artifacts"`
	TimestampSeconds float64               `json:"timestampSeconds"`
	Width            int                   `json:"width"`
	Height           int                   `json:"height"`
	Format           string                `json:"format"`
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
		Description: "从一个视频的指定时间点提取一张真实图片 artifact，可输出 JPEG 或 PNG，并可按宽度等比例缩放。" +
			"每次只提取一帧，不用于批量抽帧，也不负责理解画面内容；语义分析请用 understand_media。",
		InputSchema: json.RawMessage(`{
			"type":"object","required":["videoUrl","timestampSeconds"],
			"properties":{
				"videoUrl":{"type":"string","description":"系统 artifact URL 或执行机器可下载的公网 HTTP/HTTPS 视频 URL"},
				"timestampSeconds":{"type":"number","minimum":0,"description":"要提取的画面时间点，不能超过视频时长"},
				"format":{"type":"string","enum":["jpg","png"],"default":"jpg","description":"输出图片格式"},
				"width":{"type":"integer","minimum":2,"maximum":3840,"description":"可选输出宽度；缺省保持原始尺寸，高度按比例计算"},
				"quality":{"type":"integer","minimum":2,"maximum":31,"default":3,"description":"仅 JPEG 生效的 qscale，越小质量越高"}
			}
		}`),
		Binding: tool.ActivityBinding{
			ActivityName: ActivityName, StartToCloseTimeout: 6 * time.Minute, MaxAttempts: 2,
		},
	}
}

func (a *Activity) Run(ctx context.Context, in ActivityInput) (ActivityOutput, error) {
	const op = "capability extract_frame"
	if a.storage == nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("frameextract storage is required")))
	}
	if strings.TrimSpace(in.VideoURL) == "" || in.TimestampSeconds < 0 {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("videoUrl is required and timestampSeconds must not be negative"))
	}
	bins, err := mediaffmpeg.Locate()
	if err != nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op, temporalx.ConfigurationError(err))
	}
	ws, err := mediaffmpeg.NewWorkspace("extract-frame")
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
	if !info.HasVideo {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("input has no video stream"))
	}
	if info.Duration > 0 && in.TimestampSeconds > info.Duration {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("timestampSeconds exceeds video duration"))
	}
	args, format, extension, mimeType, err := buildArgs(in, inputPath, ws.Path("output"))
	if err != nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op, temporalx.InvalidActivityInput("%v", err))
	}
	outputPath := ws.Path("output." + extension)
	args[len(args)-1] = outputPath
	if err := mediaffmpeg.Run(ctx, bins.FFmpeg, args); err != nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op, err)
	}
	imageInfo, err := probeImage(ctx, bins.FFprobe, outputPath)
	if err != nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op, err)
	}
	data, err := mediaffmpeg.ReadOutput(outputPath)
	if err != nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op, err)
	}
	object, err := a.storage.Put(ctx, outputKey(ctx, extension), data, mimeType)
	if err != nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op+": store image", err)
	}
	return ActivityOutput{
		Artifacts: []tool.ResultArtifact{{
			Type: "image", URI: object.URL,
			Description: fmt.Sprintf("使用 extract_frame 从视频 %s 的 %s 秒处提取的画面",
				in.VideoURL, number(in.TimestampSeconds)),
		}},
		TimestampSeconds: in.TimestampSeconds,
		Width:            imageInfo.Width, Height: imageInfo.Height, Format: format,
	}, nil
}

func buildArgs(in ActivityInput, inputPath, outputPath string) ([]string, string, string, string, error) {
	if in.TimestampSeconds < 0 {
		return nil, "", "", "", fmt.Errorf("timestampSeconds must not be negative")
	}
	format := in.Format
	if format == "" {
		format = "jpg"
	}
	mimeType := ""
	switch format {
	case "jpg":
		mimeType = "image/jpeg"
	case "png":
		mimeType = "image/png"
	default:
		return nil, "", "", "", fmt.Errorf("format must be jpg or png")
	}
	if in.Width < 0 || in.Width == 1 || in.Width > 3840 {
		return nil, "", "", "", fmt.Errorf("width must be zero or between 2 and 3840")
	}
	args := []string{"-ss", number(in.TimestampSeconds), "-i", inputPath, "-frames:v", "1"}
	if in.Width > 0 {
		args = append(args, "-vf", fmt.Sprintf("scale=%d:-2", in.Width))
	}
	if format == "jpg" {
		quality := in.Quality
		if quality == 0 {
			quality = 3
		}
		if quality < 2 || quality > 31 {
			return nil, "", "", "", fmt.Errorf("quality must be between 2 and 31")
		}
		args = append(args, "-q:v", strconv.Itoa(quality))
	}
	args = append(args, outputPath)
	return args, format, format, mimeType, nil
}

func probeImage(ctx context.Context, ffprobe, path string) (mediaffmpeg.Info, error) {
	return mediaffmpeg.Probe(ctx, ffprobe, path)
}

func number(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func outputKey(ctx context.Context, extension string) string {
	sum := sha256.Sum256([]byte(temporalx.ActivityIdempotencyKey(ctx)))
	return fmt.Sprintf("capabilities/images/frame/%x.%s", sum[:16], extension)
}
