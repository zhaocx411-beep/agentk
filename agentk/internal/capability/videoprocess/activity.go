package videoprocess

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"

	"agentk/internal/artifact"
	"agentk/internal/mediaffmpeg"
	"agentk/internal/temporalx"
	"agentk/internal/tool"
)

type Operation struct {
	Type            string  `json:"type"`
	StartSeconds    float64 `json:"startSeconds,omitempty"`
	DurationSeconds float64 `json:"durationSeconds,omitempty"`
	Width           int     `json:"width,omitempty"`
	Height          int     `json:"height,omitempty"`
	Fit             string  `json:"fit,omitempty"`
	Factor          float64 `json:"factor,omitempty"`
}

type ActivityInput struct {
	VideoURL   string      `json:"videoUrl"`
	Operations []Operation `json:"operations"`
	Quality    string      `json:"quality,omitempty"`
}

type ActivityOutput struct {
	Artifacts    []tool.ResultArtifact `json:"artifacts"`
	DurationSecs float64               `json:"durationSeconds"`
	Width        int                   `json:"width"`
	Height       int                   `json:"height"`
}

type Activity struct {
	storage artifact.Storage
}

func NewActivity(storage artifact.Storage) *Activity {
	return &Activity{storage: storage}
}

func (a *Activity) Run(ctx context.Context, in ActivityInput) (ActivityOutput, error) {
	const op = "capability process_video"
	if a.storage == nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("videoprocess storage is required")))
	}
	if strings.TrimSpace(in.VideoURL) == "" || len(in.Operations) == 0 {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("videoUrl and operations are required"))
	}
	bins, err := mediaffmpeg.Locate()
	if err != nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op, temporalx.ConfigurationError(err))
	}
	ws, err := mediaffmpeg.NewWorkspace("process-video")
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
	outputPath := ws.Path("output.mp4")
	args, err := buildArgs(in, info, inputPath, outputPath)
	if err != nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op, temporalx.InvalidActivityInput("%v", err))
	}
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
	object, err := a.storage.Put(ctx, outputKey(ctx), data, "video/mp4")
	if err != nil {
		return ActivityOutput{}, temporalx.ClassifyActivityError(op+": store video", err)
	}
	params := map[string]any{
		"format": "mp4", "videoCodec": "h264", "quality": quality(in.Quality),
	}
	if outInfo.HasAudio {
		params["audioCodec"] = "aac"
	}
	return ActivityOutput{
		Artifacts: []tool.ResultArtifact{{
			Type: "video", URI: object.URL, Description: describe(in), Params: params,
		}},
		DurationSecs: outInfo.Duration,
		Width:        outInfo.Width, Height: outInfo.Height,
	}, nil
}

func describe(in ActivityInput) string {
	operations := make([]string, 0, len(in.Operations))
	for _, operation := range in.Operations {
		switch operation.Type {
		case "trim":
			operations = append(operations, fmt.Sprintf("裁剪 %s～%s 秒",
				number(operation.StartSeconds), number(operation.StartSeconds+operation.DurationSeconds)))
		case "resize", "reframe":
			operations = append(operations, fmt.Sprintf("%s为 %d×%d",
				map[string]string{"resize": "缩放", "reframe": "调整画幅"}[operation.Type],
				even(operation.Width), even(operation.Height)))
		case "speed":
			operations = append(operations, "调整为 "+number(operation.Factor)+" 倍速")
		case "mute":
			operations = append(operations, "移除音轨")
		case "normalize":
			operations = append(operations, "标准化音视频")
		default:
			operations = append(operations, operation.Type)
		}
	}
	return fmt.Sprintf("使用 process_video 对视频 %s 执行%s后生成",
		in.VideoURL, strings.Join(operations, "、"))
}

func buildArgs(in ActivityInput, info mediaffmpeg.Info, inputPath, outputPath string) ([]string, error) {
	if len(in.Operations) > 8 {
		return nil, fmt.Errorf("operations must contain at most 8 items")
	}
	videoFilters := []string{}
	audioFilters := []string{}
	mute := false
	for _, operation := range in.Operations {
		switch operation.Type {
		case "trim":
			if operation.StartSeconds < 0 || operation.DurationSeconds <= 0 {
				return nil, fmt.Errorf("trim requires startSeconds >= 0 and durationSeconds > 0")
			}
			if info.Duration > 0 && operation.StartSeconds >= info.Duration {
				return nil, fmt.Errorf("trim startSeconds is outside the video")
			}
			videoFilters = append(videoFilters,
				fmt.Sprintf("trim=start=%s:duration=%s,setpts=PTS-STARTPTS",
					number(operation.StartSeconds), number(operation.DurationSeconds)))
			if info.HasAudio {
				audioFilters = append(audioFilters,
					fmt.Sprintf("atrim=start=%s:duration=%s,asetpts=PTS-STARTPTS",
						number(operation.StartSeconds), number(operation.DurationSeconds)))
			}
		case "normalize":
			videoFilters = append(videoFilters, "fps=30", "setsar=1")
			if info.HasAudio {
				audioFilters = append(audioFilters, "aresample=48000", "aformat=channel_layouts=stereo")
			}
		case "resize", "reframe":
			if operation.Width < 2 || operation.Height < 2 || operation.Width > 3840 || operation.Height > 3840 {
				return nil, fmt.Errorf("%s requires width and height between 2 and 3840", operation.Type)
			}
			width, height := even(operation.Width), even(operation.Height)
			fit := operation.Fit
			if operation.Type == "resize" && fit == "" {
				fit = "stretch"
			}
			switch fit {
			case "cover", "":
				videoFilters = append(videoFilters,
					fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d", width, height, width, height))
			case "contain":
				videoFilters = append(videoFilters,
					fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2:black",
						width, height, width, height))
			case "stretch":
				videoFilters = append(videoFilters, fmt.Sprintf("scale=%d:%d", width, height))
			default:
				return nil, fmt.Errorf("fit must be cover, contain, or stretch")
			}
		case "speed":
			if operation.Factor < 0.5 || operation.Factor > 2 {
				return nil, fmt.Errorf("speed factor must be between 0.5 and 2")
			}
			videoFilters = append(videoFilters, fmt.Sprintf("setpts=PTS/%s", number(operation.Factor)))
			if info.HasAudio {
				audioFilters = append(audioFilters, "atempo="+number(operation.Factor))
			}
		case "mute":
			mute = true
		default:
			return nil, fmt.Errorf("unsupported operation %q", operation.Type)
		}
	}
	args := []string{"-i", inputPath}
	if len(videoFilters) > 0 {
		args = append(args, "-vf", strings.Join(videoFilters, ","))
	}
	if !mute && info.HasAudio && len(audioFilters) > 0 {
		args = append(args, "-af", strings.Join(audioFilters, ","))
	}
	args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-crf", crf(in.Quality), "-pix_fmt", "yuv420p")
	if mute || !info.HasAudio {
		args = append(args, "-an")
	} else {
		args = append(args, "-c:a", "aac", "-b:a", "192k")
	}
	args = append(args, "-movflags", "+faststart", outputPath)
	return args, nil
}

func quality(value string) string {
	switch value {
	case "high", "small":
		return value
	default:
		return "balanced"
	}
}

func crf(value string) string {
	switch quality(value) {
	case "high":
		return "18"
	case "small":
		return "28"
	default:
		return "23"
	}
}

func number(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func even(value int) int {
	return value - value%2
}

func outputKey(ctx context.Context) string {
	sum := sha256.Sum256([]byte(temporalx.ActivityIdempotencyKey(ctx)))
	return fmt.Sprintf("capabilities/videos/process/%x.mp4", sum[:16])
}
