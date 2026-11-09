package videoprobe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const maxVideoBytes int64 = 512 << 20

type videoSource func(io.Writer) (int64, string, error)

type Output struct {
	FileSizeBytes   int64            `json:"fileSizeBytes"`
	DurationSecs    float64          `json:"durationSeconds,omitempty"`
	Format          FormatInfo       `json:"format"`
	VideoStreams    []VideoStream    `json:"videoStreams"`
	AudioStreams    []AudioStream    `json:"audioStreams"`
	SubtitleStreams []SubtitleStream `json:"subtitleStreams"`
	OtherStreams    []StreamInfo     `json:"otherStreams"`
	Raw             json.RawMessage  `json:"raw"`
}

type FormatInfo struct {
	Name       string            `json:"name,omitempty"`
	LongName   string            `json:"longName,omitempty"`
	MIMEType   string            `json:"mimeType,omitempty"`
	BitRate    int64             `json:"bitRate,omitempty"`
	ProbeScore int               `json:"probeScore,omitempty"`
	Tags       map[string]string `json:"tags,omitempty"`
}

type StreamInfo struct {
	Index         int               `json:"index"`
	Type          string            `json:"type"`
	CodecName     string            `json:"codecName,omitempty"`
	CodecLongName string            `json:"codecLongName,omitempty"`
	Profile       string            `json:"profile,omitempty"`
	CodecTag      string            `json:"codecTag,omitempty"`
	DurationSecs  float64           `json:"durationSeconds,omitempty"`
	BitRate       int64             `json:"bitRate,omitempty"`
	TimeBase      string            `json:"timeBase,omitempty"`
	Language      string            `json:"language,omitempty"`
	Title         string            `json:"title,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`
}

type VideoStream struct {
	StreamInfo
	Width              int     `json:"width,omitempty"`
	Height             int     `json:"height,omitempty"`
	DisplayWidth       int     `json:"displayWidth,omitempty"`
	DisplayHeight      int     `json:"displayHeight,omitempty"`
	Rotation           int     `json:"rotation,omitempty"`
	PixelFormat        string  `json:"pixelFormat,omitempty"`
	Level              int     `json:"level,omitempty"`
	FrameRate          float64 `json:"frameRate,omitempty"`
	FrameRateRaw       string  `json:"frameRateRaw,omitempty"`
	RealFrameRate      float64 `json:"realFrameRate,omitempty"`
	RealFrameRateRaw   string  `json:"realFrameRateRaw,omitempty"`
	SampleAspectRatio  string  `json:"sampleAspectRatio,omitempty"`
	DisplayAspectRatio string  `json:"displayAspectRatio,omitempty"`
	FieldOrder         string  `json:"fieldOrder,omitempty"`
	ColorRange         string  `json:"colorRange,omitempty"`
	ColorSpace         string  `json:"colorSpace,omitempty"`
	ColorTransfer      string  `json:"colorTransfer,omitempty"`
	ColorPrimaries     string  `json:"colorPrimaries,omitempty"`
}

type AudioStream struct {
	StreamInfo
	SampleFormat  string `json:"sampleFormat,omitempty"`
	SampleRate    int    `json:"sampleRate,omitempty"`
	Channels      int    `json:"channels,omitempty"`
	ChannelLayout string `json:"channelLayout,omitempty"`
	BitsPerSample int    `json:"bitsPerSample,omitempty"`
}

type SubtitleStream struct {
	StreamInfo
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
}

type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
	Format  ffprobeFormat   `json:"format"`
}

type ffprobeFormat struct {
	FormatName     string            `json:"format_name"`
	FormatLongName string            `json:"format_long_name"`
	Duration       string            `json:"duration"`
	BitRate        string            `json:"bit_rate"`
	ProbeScore     int               `json:"probe_score"`
	Tags           map[string]string `json:"tags"`
}

type ffprobeStream struct {
	Index              int               `json:"index"`
	CodecName          string            `json:"codec_name"`
	CodecLongName      string            `json:"codec_long_name"`
	Profile            string            `json:"profile"`
	CodecType          string            `json:"codec_type"`
	CodecTagString     string            `json:"codec_tag_string"`
	Width              int               `json:"width"`
	Height             int               `json:"height"`
	PixelFormat        string            `json:"pix_fmt"`
	Level              int               `json:"level"`
	AverageFrameRate   string            `json:"avg_frame_rate"`
	RealFrameRate      string            `json:"r_frame_rate"`
	SampleAspectRatio  string            `json:"sample_aspect_ratio"`
	DisplayAspectRatio string            `json:"display_aspect_ratio"`
	FieldOrder         string            `json:"field_order"`
	ColorRange         string            `json:"color_range"`
	ColorSpace         string            `json:"color_space"`
	ColorTransfer      string            `json:"color_transfer"`
	ColorPrimaries     string            `json:"color_primaries"`
	SampleFormat       string            `json:"sample_fmt"`
	SampleRate         string            `json:"sample_rate"`
	Channels           int               `json:"channels"`
	ChannelLayout      string            `json:"channel_layout"`
	BitsPerSample      int               `json:"bits_per_sample"`
	Duration           string            `json:"duration"`
	BitRate            string            `json:"bit_rate"`
	TimeBase           string            `json:"time_base"`
	Tags               map[string]string `json:"tags"`
	SideDataList       []struct {
		Rotation float64 `json:"rotation"`
	} `json:"side_data_list"`
}

func probe(ctx context.Context, videoURL string, source videoSource) (Output, error) {
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return Output{}, fmt.Errorf("ffprobe not found in PATH (must be installed with ffmpeg): %w", err)
	}
	file, err := os.CreateTemp("", "agentk-probe-video-*")
	if err != nil {
		return Output{}, err
	}
	path := file.Name()
	defer func() { _ = os.Remove(path) }()

	size, mimeType, err := source(file)
	if err != nil {
		_ = file.Close()
		return Output{}, fmt.Errorf("download video: %w", err)
	}
	if err := file.Close(); err != nil {
		return Output{}, fmt.Errorf("close downloaded video: %w", err)
	}

	cmd := exec.CommandContext(ctx, ffprobe,
		"-v", "error",
		"-show_format",
		"-show_streams",
		"-of", "json",
		path,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Output{}, fmt.Errorf("ffprobe: %w: %s", err, tail(stderr.String(), 800))
	}

	var decoded ffprobeOutput
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		return Output{}, fmt.Errorf("decode ffprobe output: %w", err)
	}
	raw, err := sanitizedRaw(stdout.Bytes(), videoURL)
	if err != nil {
		return Output{}, fmt.Errorf("sanitize ffprobe output: %w", err)
	}
	out := normalize(decoded, size, raw)
	out.Format.MIMEType = mimeType
	return out, nil
}

func downloadVideo(ctx context.Context, client *http.Client, videoURL string, dst io.Writer) (int64, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, videoURL, nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return 0, "", fmt.Errorf("GET %s: status %d", videoURL, resp.StatusCode)
	}
	if resp.ContentLength > maxVideoBytes {
		return 0, "", fmt.Errorf("video exceeds %d bytes", maxVideoBytes)
	}
	size, err := copyVideo(dst, resp.Body)
	if err != nil {
		return 0, "", err
	}
	return size, resp.Header.Get("Content-Type"), nil
}

func copyVideo(dst io.Writer, src io.Reader) (int64, error) {
	size, err := io.Copy(dst, io.LimitReader(src, maxVideoBytes+1))
	if err != nil {
		return 0, err
	}
	if size > maxVideoBytes {
		return 0, fmt.Errorf("video exceeds %d bytes", maxVideoBytes)
	}
	return size, nil
}

func normalize(in ffprobeOutput, size int64, raw json.RawMessage) Output {
	out := Output{
		FileSizeBytes: size,
		DurationSecs:  parseFloat(in.Format.Duration),
		Format: FormatInfo{
			Name:       in.Format.FormatName,
			LongName:   in.Format.FormatLongName,
			BitRate:    parseInt64(in.Format.BitRate),
			ProbeScore: in.Format.ProbeScore,
			Tags:       in.Format.Tags,
		},
		VideoStreams:    []VideoStream{},
		AudioStreams:    []AudioStream{},
		SubtitleStreams: []SubtitleStream{},
		OtherStreams:    []StreamInfo{},
		Raw:             raw,
	}
	for _, stream := range in.Streams {
		base := normalizeStream(stream)
		if base.DurationSecs > out.DurationSecs {
			out.DurationSecs = base.DurationSecs
		}
		switch stream.CodecType {
		case "video":
			rotation := streamRotation(stream)
			displayWidth, displayHeight := stream.Width, stream.Height
			if normalizedRotation := ((rotation % 360) + 360) % 360; normalizedRotation == 90 || normalizedRotation == 270 {
				displayWidth, displayHeight = displayHeight, displayWidth
			}
			out.VideoStreams = append(out.VideoStreams, VideoStream{
				StreamInfo:         base,
				Width:              stream.Width,
				Height:             stream.Height,
				DisplayWidth:       displayWidth,
				DisplayHeight:      displayHeight,
				Rotation:           rotation,
				PixelFormat:        stream.PixelFormat,
				Level:              stream.Level,
				FrameRate:          parseRate(stream.AverageFrameRate),
				FrameRateRaw:       stream.AverageFrameRate,
				RealFrameRate:      parseRate(stream.RealFrameRate),
				RealFrameRateRaw:   stream.RealFrameRate,
				SampleAspectRatio:  stream.SampleAspectRatio,
				DisplayAspectRatio: stream.DisplayAspectRatio,
				FieldOrder:         stream.FieldOrder,
				ColorRange:         stream.ColorRange,
				ColorSpace:         stream.ColorSpace,
				ColorTransfer:      stream.ColorTransfer,
				ColorPrimaries:     stream.ColorPrimaries,
			})
		case "audio":
			out.AudioStreams = append(out.AudioStreams, AudioStream{
				StreamInfo:    base,
				SampleFormat:  stream.SampleFormat,
				SampleRate:    int(parseInt64(stream.SampleRate)),
				Channels:      stream.Channels,
				ChannelLayout: stream.ChannelLayout,
				BitsPerSample: stream.BitsPerSample,
			})
		case "subtitle":
			out.SubtitleStreams = append(out.SubtitleStreams, SubtitleStream{
				StreamInfo: base,
				Width:      stream.Width,
				Height:     stream.Height,
			})
		default:
			out.OtherStreams = append(out.OtherStreams, base)
		}
	}
	return out
}

func normalizeStream(stream ffprobeStream) StreamInfo {
	return StreamInfo{
		Index:         stream.Index,
		Type:          stream.CodecType,
		CodecName:     stream.CodecName,
		CodecLongName: stream.CodecLongName,
		Profile:       stream.Profile,
		CodecTag:      stream.CodecTagString,
		DurationSecs:  parseFloat(stream.Duration),
		BitRate:       parseInt64(stream.BitRate),
		TimeBase:      stream.TimeBase,
		Language:      stream.Tags["language"],
		Title:         stream.Tags["title"],
		Tags:          stream.Tags,
	}
}

func streamRotation(stream ffprobeStream) int {
	for _, sideData := range stream.SideDataList {
		if sideData.Rotation != 0 {
			return int(sideData.Rotation)
		}
	}
	rotation, _ := strconv.Atoi(stream.Tags["rotate"])
	return rotation
}

func parseFloat(value string) float64 {
	parsed, _ := strconv.ParseFloat(value, 64)
	return parsed
}

func parseInt64(value string) int64 {
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}

func parseRate(value string) float64 {
	parts := strings.Split(value, "/")
	if len(parts) == 2 {
		numerator := parseFloat(parts[0])
		denominator := parseFloat(parts[1])
		if denominator != 0 {
			return numerator / denominator
		}
	}
	return parseFloat(value)
}

func sanitizedRaw(raw []byte, videoURL string) (json.RawMessage, error) {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	if format, ok := value["format"].(map[string]any); ok {
		format["filename"] = videoURL
	}
	return json.Marshal(value)
}

func tail(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return "…" + value[len(value)-limit:]
}
