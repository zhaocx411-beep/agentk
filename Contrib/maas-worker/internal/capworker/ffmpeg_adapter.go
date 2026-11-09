package capworker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"
)

type ffmpegPipelineStep struct {
	Op     string         `json:"op"`
	Params map[string]any `json:"params,omitempty"`
}

type composeVideoInput struct {
	DB                    string              `json:"db,omitempty"`
	Table                 string              `json:"table,omitempty"`
	VideoURLs             []string            `json:"videoUrls"`
	Size                  string              `json:"size,omitempty"`
	AudioTracks           []composeAudioTrack `json:"audioTracks,omitempty"`
	PreserveOriginalAudio *bool               `json:"preserveOriginalAudio,omitempty"`
	SubtitleURL           string              `json:"subtitleUrl,omitempty"`
	SubtitleFormat        string              `json:"subtitleFormat,omitempty"`
	SubtitleSegments      []subtitleSegment   `json:"subtitleSegments,omitempty"`
}

type composeAudioTrack struct {
	URL              string   `json:"url"`
	StartSeconds     float64  `json:"startSeconds,omitempty"`
	TrimStartSeconds float64  `json:"trimStartSeconds,omitempty"`
	DurationSeconds  float64  `json:"durationSeconds,omitempty"`
	Volume           *float64 `json:"volume,omitempty"`
	Loop             bool     `json:"loop,omitempty"`
}

type subtitleSegment struct {
	StartSeconds float64 `json:"startSeconds"`
	EndSeconds   float64 `json:"endSeconds"`
	Text         string  `json:"text"`
}

type extractAudioInput struct {
	DB                string  `json:"db,omitempty"`
	Table             string  `json:"table,omitempty"`
	VideoURL          string  `json:"videoUrl"`
	StartSeconds      float64 `json:"startSeconds,omitempty"`
	DurationSeconds   float64 `json:"durationSeconds,omitempty"`
	Format            string  `json:"format,omitempty"`
	BitRate           string  `json:"bitRate,omitempty"`
	SampleRate        int     `json:"sampleRate,omitempty"`
	Channels          int     `json:"channels,omitempty"`
	NormalizeLoudness bool    `json:"normalizeLoudness,omitempty"`
}

type processVideoInput struct {
	DB         string                  `json:"db,omitempty"`
	Table      string                  `json:"table,omitempty"`
	VideoURL   string                  `json:"videoUrl"`
	Operations []processVideoOperation `json:"operations"`
	Quality    string                  `json:"quality,omitempty"`
}

type processVideoOperation struct {
	Type            string  `json:"type"`
	Strength        string  `json:"strength,omitempty"`
	StartSeconds    float64 `json:"startSeconds,omitempty"`
	DurationSeconds float64 `json:"durationSeconds,omitempty"`
	Width           int     `json:"width,omitempty"`
	Height          int     `json:"height,omitempty"`
	Fit             string  `json:"fit,omitempty"`
	Factor          float64 `json:"factor,omitempty"`
}

type ffmpegProbe struct {
	Duration float64 `json:"duration"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	FPS      float64 `json:"fps"`
	HasAudio bool    `json:"has_audio"`
	HasVideo bool    `json:"has_video"`
}

type ffmpegOutputTarget struct {
	DB    string
	Table string
}

func (a *inferActivity) composeVideo(
	ctx context.Context,
	requestID string,
	features map[string]any,
) (map[string]any, error) {
	var input composeVideoInput
	if err := decodeAdapterInput(features, &input); err != nil {
		return nil, err
	}
	if err := validateComposeVideoInput(&input); err != nil {
		return nil, err
	}
	target, err := validateFFmpegOutputTarget(input.DB, input.Table)
	if err != nil {
		return nil, err
	}

	preserveOriginal := input.PreserveOriginalAudio == nil || *input.PreserveOriginalAudio
	steps := []ffmpegPipelineStep{{
		Op: "concat_av",
		Params: map[string]any{
			"size": input.Size, "fit": "cover", "fps": 30,
			"sample_rate": 48000, "audio_bitrate": "192k",
			"crf": 23, "preset": "veryfast",
		},
	}}
	// concat_av guarantees a normalized stereo timeline, including silence for
	// clips without an audio stream. Keep that timeline while mixing external
	// tracks so amix can use it as the exact output-duration anchor.
	if !preserveOriginal && len(input.AudioTracks) == 0 {
		steps = append(steps, ffmpegPipelineStep{Op: "strip_audio"})
	}
	steps = append(steps, ffmpegPipelineStep{Op: "probe"})

	baseRequest, err := a.ffmpegRequest(
		map[string]any{"videos": input.VideoURLs}, steps, target,
	)
	if err != nil {
		return nil, err
	}
	current, err := a.gateway.Infer(
		ctx, a.route, adapterRequestID(requestID, "compose-base"), baseRequest,
	)
	if err != nil {
		return nil, err
	}
	videoRef, probe, err := ffmpegMediaOutput(current, "video")
	if err != nil {
		return nil, err
	}

	if len(input.AudioTracks) > 0 {
		audios := make([]string, 0, len(input.AudioTracks))
		offsets := make([]int64, 0, len(input.AudioTracks))
		gains := make([]float64, 0, len(input.AudioTracks))
		trimStarts := make([]float64, 0, len(input.AudioTracks))
		durations := make([]float64, 0, len(input.AudioTracks))
		loops := make([]bool, 0, len(input.AudioTracks))
		for _, track := range input.AudioTracks {
			volume := 1.0
			if track.Volume != nil {
				volume = *track.Volume
			}
			audios = append(audios, track.URL)
			offsets = append(offsets, int64(math.Round(track.StartSeconds*1000)))
			gains = append(gains, volume)
			trimStarts = append(trimStarts, track.TrimStartSeconds)
			durations = append(durations, track.DurationSeconds)
			loops = append(loops, track.Loop)
		}
		videoGain := 0.0
		if preserveOriginal {
			videoGain = 1
		}
		mixRequest, err := a.ffmpegRequest(
			map[string]any{"video": videoRef, "audios": audios},
			[]ffmpegPipelineStep{
				{Op: "mix_audio", Params: map[string]any{
					"offsets_ms": offsets, "gains": gains,
					"trim_starts": trimStarts, "durations": durations, "loops": loops,
					"include_video_audio": true, "video_gain": videoGain,
					"duration_mode": "first", "sample_rate": 48000,
					"audio_bitrate": "192k", "normalize": true, "limit": false,
				}},
				{Op: "probe"},
			}, target,
		)
		if err != nil {
			return nil, err
		}
		current, err = a.gateway.Infer(
			ctx, a.route, adapterRequestID(requestID, "compose-mix"), mixRequest,
		)
		if err != nil {
			return nil, err
		}
		videoRef, probe, err = ffmpegMediaOutput(current, "video")
		if err != nil {
			return nil, err
		}
	}

	if input.SubtitleURL != "" || len(input.SubtitleSegments) > 0 {
		subtitleRequest, err := a.subtitleRequest(videoRef, input, target)
		if err != nil {
			return nil, err
		}
		current, err = a.gateway.Infer(
			ctx, a.route, adapterRequestID(requestID, "compose-subtitles"), subtitleRequest,
		)
		if err != nil {
			return nil, err
		}
		videoRef, probe, err = ffmpegMediaOutput(current, "video")
		if err != nil {
			return nil, err
		}
	}
	artifactURI, err := a.artifactURIs.URI(videoRef)
	if err != nil {
		return nil, invalidGatewayOutput("normalize composed video artifact: %v", err)
	}

	return map[string]any{
		"artifacts": []map[string]any{{
			"type": "video", "uri": artifactURI,
			"description": fmt.Sprintf("由 MaaS ffmpeg 合成 %d 段视频", len(input.VideoURLs)),
		}},
		"durationSeconds": probe.Duration,
		"clipCount":       len(input.VideoURLs),
		"width":           probe.Width,
		"height":          probe.Height,
	}, nil
}

func (a *inferActivity) extractAudio(
	ctx context.Context,
	requestID string,
	features map[string]any,
) (map[string]any, error) {
	var input extractAudioInput
	if err := decodeAdapterInput(features, &input); err != nil {
		return nil, err
	}
	target, err := validateFFmpegOutputTarget(input.DB, input.Table)
	if err != nil {
		return nil, err
	}
	input.VideoURL, err = normalizeFFmpegRef(input.VideoURL)
	if err != nil {
		return nil, invalidAdapterInput("videoUrl: %v", err)
	}
	if !finiteNonNegative(input.StartSeconds) || !finiteNonNegative(input.DurationSeconds) {
		return nil, invalidAdapterInput("startSeconds and durationSeconds must be finite and non-negative")
	}
	if input.Format == "" {
		input.Format = "mp3"
	}
	if input.Format != "mp3" && input.Format != "aac" && input.Format != "wav" {
		return nil, invalidAdapterInput("format must be mp3, aac, or wav")
	}
	if input.BitRate == "" {
		input.BitRate = "192k"
	}
	switch input.BitRate {
	case "96k", "128k", "192k", "256k":
	default:
		return nil, invalidAdapterInput("bitRate must be 96k, 128k, 192k, or 256k")
	}
	if input.SampleRate == 0 {
		input.SampleRate = 44100
	}
	switch input.SampleRate {
	case 16000, 24000, 44100, 48000:
	default:
		return nil, invalidAdapterInput("sampleRate must be 16000, 24000, 44100, or 48000")
	}
	if input.Channels == 0 {
		input.Channels = 2
	}
	if input.Channels != 1 && input.Channels != 2 {
		return nil, invalidAdapterInput("channels must be 1 or 2")
	}

	steps := []ffmpegPipelineStep{
		{Op: "extract_audio", Params: map[string]any{
			"start": input.StartSeconds, "duration": input.DurationSeconds,
			"format": input.Format, "bit_rate": input.BitRate,
			"sample_rate": input.SampleRate, "channels": input.Channels,
			"normalize_loudness": input.NormalizeLoudness,
		}},
		ffmpegPipelineStep{Op: "probe"},
	}
	request, err := a.ffmpegRequest(
		map[string]any{"video": input.VideoURL}, steps, target)
	if err != nil {
		return nil, err
	}
	output, err := a.gateway.Infer(
		ctx, a.route, adapterRequestID(requestID, "extract-audio"), request,
	)
	if err != nil {
		return nil, err
	}
	audioRef, probe, err := ffmpegMediaOutput(output, "audio")
	if err != nil {
		return nil, err
	}
	artifactURI, err := a.artifactURIs.URI(audioRef)
	if err != nil {
		return nil, invalidGatewayOutput("normalize extracted audio artifact: %v", err)
	}
	return map[string]any{
		"artifacts": []map[string]any{{
			"type": "audio", "uri": artifactURI,
			"description": fmt.Sprintf("由 MaaS ffmpeg 从视频提取的 %s 音轨", strings.ToUpper(input.Format)),
		}},
		"durationSeconds": probe.Duration,
		"format":          input.Format,
		"sampleRate":      input.SampleRate,
		"channels":        input.Channels,
	}, nil
}

func (a *inferActivity) processVideo(
	ctx context.Context,
	requestID string,
	features map[string]any,
) (map[string]any, error) {
	var input processVideoInput
	if err := decodeAdapterInput(features, &input); err != nil {
		return nil, err
	}
	target, err := validateFFmpegOutputTarget(input.DB, input.Table)
	if err != nil {
		return nil, err
	}
	input.VideoURL, err = normalizeFFmpegRef(input.VideoURL)
	if err != nil {
		return nil, invalidAdapterInput("videoUrl: %v", err)
	}
	if err := validateProcessVideoInput(&input); err != nil {
		return nil, err
	}
	request, err := a.ffmpegRequest(
		map[string]any{"video": input.VideoURL},
		[]ffmpegPipelineStep{
			{Op: "process_video", Params: map[string]any{
				"operations": processVideoOperationParams(input.Operations),
				"quality":    input.Quality,
			}},
			{Op: "probe"},
		},
		target,
	)
	if err != nil {
		return nil, err
	}
	output, err := a.gateway.Infer(
		ctx, a.route, adapterRequestID(requestID, "process-video"), request,
	)
	if err != nil {
		return nil, err
	}
	videoRef, probe, err := ffmpegMediaOutput(output, "video")
	if err != nil {
		return nil, err
	}
	if !probe.HasVideo {
		return nil, invalidGatewayOutput("processed artifact has no video stream")
	}
	artifactURI, err := a.artifactURIs.URI(videoRef)
	if err != nil {
		return nil, invalidGatewayOutput("normalize processed video artifact: %v", err)
	}
	artifactParams := map[string]any{
		"format": "mp4", "videoCodec": "h264", "quality": input.Quality,
	}
	if probe.HasAudio {
		artifactParams["audioCodec"] = "aac"
	}
	return map[string]any{
		"artifacts": []map[string]any{{
			"type": "video", "uri": artifactURI,
			"description": fmt.Sprintf(
				"由 MaaS ffmpeg 按顺序执行 %s 后生成",
				processVideoOperationNames(input.Operations)),
			"params": artifactParams,
		}},
		"durationSeconds": probe.Duration,
		"width":           probe.Width,
		"height":          probe.Height,
	}, nil
}

func validateProcessVideoInput(input *processVideoInput) error {
	if len(input.Operations) == 0 || len(input.Operations) > 8 {
		return invalidAdapterInput("operations must contain between 1 and 8 items")
	}
	if input.Quality == "" {
		input.Quality = "balanced"
	}
	if input.Quality != "high" && input.Quality != "balanced" && input.Quality != "small" {
		return invalidAdapterInput("quality must be high, balanced, or small")
	}
	for i, operation := range input.Operations {
		switch operation.Type {
		case "trim":
			if !finiteNonNegative(operation.StartSeconds) ||
				!finiteNonNegative(operation.DurationSeconds) || operation.DurationSeconds == 0 {
				return invalidAdapterInput(
					"operations[%d] trim requires finite startSeconds >= 0 and durationSeconds > 0", i)
			}
		case "normalize", "mute":
		case "denoise":
			if operation.Strength == "" {
				operation.Strength = "medium"
			}
			if operation.Strength != "light" && operation.Strength != "medium" &&
				operation.Strength != "strong" {
				return invalidAdapterInput(
					"operations[%d].strength must be light, medium, or strong", i)
			}
		case "resize", "reframe":
			if operation.Width < 2 || operation.Width > 3840 ||
				operation.Height < 2 || operation.Height > 3840 {
				return invalidAdapterInput(
					"operations[%d] width and height must be between 2 and 3840", i)
			}
			if operation.Fit == "" {
				if operation.Type == "resize" {
					operation.Fit = "stretch"
				} else {
					operation.Fit = "cover"
				}
			}
			if operation.Fit != "cover" && operation.Fit != "contain" &&
				operation.Fit != "stretch" {
				return invalidAdapterInput(
					"operations[%d].fit must be cover, contain, or stretch", i)
			}
		case "speed":
			if math.IsNaN(operation.Factor) || math.IsInf(operation.Factor, 0) ||
				operation.Factor < 0.5 || operation.Factor > 2 {
				return invalidAdapterInput(
					"operations[%d].factor must be between 0.5 and 2", i)
			}
		default:
			return invalidAdapterInput(
				"operations[%d] has unsupported type %q", i, operation.Type)
		}
		input.Operations[i] = operation
	}
	return nil
}

func processVideoOperationParams(operations []processVideoOperation) []map[string]any {
	result := make([]map[string]any, 0, len(operations))
	for _, operation := range operations {
		item := map[string]any{"type": operation.Type}
		switch operation.Type {
		case "trim":
			item["startSeconds"] = operation.StartSeconds
			item["durationSeconds"] = operation.DurationSeconds
		case "denoise":
			item["strength"] = operation.Strength
		case "resize", "reframe":
			item["width"] = operation.Width
			item["height"] = operation.Height
			item["fit"] = operation.Fit
		case "speed":
			item["factor"] = operation.Factor
		}
		result = append(result, item)
	}
	return result
}

func processVideoOperationNames(operations []processVideoOperation) string {
	names := make([]string, 0, len(operations))
	for _, operation := range operations {
		names = append(names, operation.Type)
	}
	return strings.Join(names, "、")
}

func (a *inferActivity) ffmpegRequest(
	refs map[string]any,
	steps []ffmpegPipelineStep,
	target ffmpegOutputTarget,
) (map[string]any, error) {
	refsJSON, err := json.Marshal(refs)
	if err != nil {
		return nil, invalidAdapterInput("encode ffmpeg refs: %v", err)
	}
	pipelineJSON, err := json.Marshal(steps)
	if err != nil {
		return nil, invalidAdapterInput("encode ffmpeg pipeline: %v", err)
	}
	request := map[string]any{
		"refs": string(refsJSON), "pipeline": string(pipelineJSON),
	}
	if target.DB != "" {
		request["db"] = target.DB
		request["table"] = target.Table
	}
	return request, nil
}

func (a *inferActivity) subtitleRequest(
	videoRef string,
	input composeVideoInput,
	target ffmpegOutputTarget,
) (map[string]any, error) {
	format := input.SubtitleFormat
	if format == "" {
		format = "srt"
	}
	refs := map[string]any{"video": videoRef}
	request, err := a.ffmpegRequest(refs, []ffmpegPipelineStep{
		{Op: "burn_subtitles", Params: map[string]any{
			"subtitle_format": format, "crf": 23, "preset": "veryfast",
		}},
		{Op: "probe"},
	}, target)
	if err != nil {
		return nil, err
	}
	if input.SubtitleURL != "" {
		refs["subtitle"] = input.SubtitleURL
		return a.ffmpegRequest(refs, []ffmpegPipelineStep{
			{Op: "burn_subtitles", Params: map[string]any{
				"subtitle_format": format, "crf": 23, "preset": "veryfast",
			}},
			{Op: "probe"},
		}, target)
	}
	request["subtitle"] = "base64:" + base64.StdEncoding.EncodeToString(
		[]byte(renderSRT(input.SubtitleSegments)),
	)
	return request, nil
}

func validateFFmpegOutputTarget(db, table string) (ffmpegOutputTarget, error) {
	db = strings.TrimSpace(db)
	table = strings.TrimSpace(table)
	if (db == "") != (table == "") {
		return ffmpegOutputTarget{}, invalidAdapterInput(
			"db and table must be provided together")
	}
	if db == "" {
		return ffmpegOutputTarget{}, nil
	}
	if strings.ContainsAny(db, ":/\\") || strings.ContainsAny(table, ":/\\") {
		return ffmpegOutputTarget{}, invalidAdapterInput(
			"db and table must not contain colon, slash, or backslash")
	}
	return ffmpegOutputTarget{DB: db, Table: table}, nil
}

func validateComposeVideoInput(input *composeVideoInput) error {
	if len(input.VideoURLs) == 0 || len(input.VideoURLs) > 20 {
		return invalidAdapterInput("videoUrls must contain between 1 and 20 items")
	}
	for i, ref := range input.VideoURLs {
		var err error
		ref, err = normalizeFFmpegRef(ref)
		if err != nil {
			return invalidAdapterInput("videoUrls[%d]: %v", i, err)
		}
		input.VideoURLs[i] = ref
	}
	if input.Size == "" {
		input.Size = "auto"
	}
	if input.Size != "auto" && input.Size != "1080p" {
		return invalidAdapterInput("size must be auto or 1080p")
	}
	if len(input.AudioTracks) > 8 {
		return invalidAdapterInput("audioTracks must contain at most 8 items")
	}
	for i, track := range input.AudioTracks {
		var err error
		track.URL, err = normalizeFFmpegRef(track.URL)
		if err != nil {
			return invalidAdapterInput("audioTracks[%d].url: %v", i, err)
		}
		if !finiteNonNegative(track.StartSeconds) ||
			!finiteNonNegative(track.TrimStartSeconds) ||
			!finiteNonNegative(track.DurationSeconds) ||
			(track.Volume != nil && (!finiteNonNegative(*track.Volume) || *track.Volume > 2)) {
			return invalidAdapterInput("audioTracks[%d] has invalid timing or volume", i)
		}
		input.AudioTracks[i] = track
	}
	input.SubtitleURL = strings.TrimSpace(input.SubtitleURL)
	if input.SubtitleURL != "" && len(input.SubtitleSegments) > 0 {
		return invalidAdapterInput("subtitleUrl and subtitleSegments cannot be used together")
	}
	if input.SubtitleURL != "" {
		var err error
		input.SubtitleURL, err = normalizeFFmpegRef(input.SubtitleURL)
		if err != nil {
			return invalidAdapterInput("subtitleUrl: %v", err)
		}
	}
	if input.SubtitleFormat == "" {
		input.SubtitleFormat = "srt"
	}
	if input.SubtitleFormat != "srt" && input.SubtitleFormat != "ass" {
		return invalidAdapterInput("subtitleFormat must be srt or ass")
	}
	if len(input.SubtitleSegments) > 500 {
		return invalidAdapterInput("subtitleSegments must contain at most 500 items")
	}
	if len(input.SubtitleSegments) > 0 && input.SubtitleFormat != "srt" {
		return invalidAdapterInput("subtitleSegments only support srt format")
	}
	for i, segment := range input.SubtitleSegments {
		if !finiteNonNegative(segment.StartSeconds) || !finiteNonNegative(segment.EndSeconds) ||
			segment.EndSeconds <= segment.StartSeconds || strings.TrimSpace(segment.Text) == "" {
			return invalidAdapterInput("subtitleSegments[%d] is invalid", i)
		}
	}
	return nil
}

func decodeAdapterInput(features map[string]any, target any) error {
	raw, err := json.Marshal(features)
	if err != nil {
		return invalidAdapterInput("encode AgentK input: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return invalidAdapterInput("decode AgentK input: %v", err)
	}
	return nil
}

func normalizeFFmpegRef(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("is required")
	}
	if strings.HasPrefix(ref, "blob://") {
		bucket, key, ok := strings.Cut(strings.TrimPrefix(ref, "blob://"), "/")
		if !ok || !validArtifactBucket(bucket) {
			return "", fmt.Errorf("must contain a valid blob bucket and key")
		}
		if err := validateArtifactKey(key); err != nil {
			return "", err
		}
		db, table, ok := strings.Cut(bucket, "-")
		if !ok || db == "" || table == "" {
			return "", fmt.Errorf("blob bucket must use db-table form")
		}
		return db + ":" + table + ":" + key, nil
	}
	parts := strings.SplitN(ref, ":", 3)
	if len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != "" {
		return ref, nil
	}
	parsed, err := url.Parse(ref)
	if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") &&
		parsed.Host != "" && parsed.User == nil {
		return ref, nil
	}
	return "", fmt.Errorf("must be blob://db-table/key, db:table:key, or an absolute HTTP(S) URL")
}

func finiteNonNegative(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func ffmpegMediaOutput(output map[string]any, mediaKind string) (string, ffmpegProbe, error) {
	ref, ok := nonEmptyString(output[mediaKind])
	if !ok {
		return "", ffmpegProbe{}, invalidGatewayOutput(
			"ffmpeg response is missing %s", mediaKind)
	}
	probe, err := parseFFmpegProbe(output)
	if err != nil {
		return "", ffmpegProbe{}, err
	}
	return ref, probe, nil
}

func parseFFmpegProbe(output map[string]any) (ffmpegProbe, error) {
	result, ok := nonEmptyString(output["result"])
	if !ok {
		return ffmpegProbe{}, invalidGatewayOutput("ffmpeg response is missing probe result")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(result), &raw); err != nil {
		return ffmpegProbe{}, invalidGatewayOutput("decode ffmpeg probe result: %v", err)
	}
	if nested, exists := raw["probe"]; exists {
		if err := json.Unmarshal(nested, &raw); err != nil {
			return ffmpegProbe{}, invalidGatewayOutput("decode nested ffmpeg probe result: %v", err)
		}
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return ffmpegProbe{}, invalidGatewayOutput("encode ffmpeg probe result: %v", err)
	}
	var probe ffmpegProbe
	if err := json.Unmarshal(encoded, &probe); err != nil {
		return ffmpegProbe{}, invalidGatewayOutput("decode ffmpeg probe fields: %v", err)
	}
	if probe.Duration <= 0 {
		return ffmpegProbe{}, invalidGatewayOutput("ffmpeg probe returned invalid duration")
	}
	return probe, nil
}

func nonEmptyString(value any) (string, bool) {
	text, ok := value.(string)
	return text, ok && strings.TrimSpace(text) != ""
}

func invalidAdapterInput(format string, args ...any) error {
	return &CallError{
		Code: "INVALID_ARGUMENT", Message: fmt.Sprintf(format, args...), Permanent: true,
	}
}

func invalidGatewayOutput(format string, args ...any) error {
	return &CallError{
		Code: "INVALID_RESPONSE", Message: fmt.Sprintf(format, args...), Permanent: true,
	}
}

func adapterRequestID(requestID string, step string) string {
	return requestID + "-" + step
}

func renderSRT(segments []subtitleSegment) string {
	var builder strings.Builder
	for index, segment := range segments {
		fmt.Fprintf(&builder, "%d\n%s --> %s\n%s\n\n",
			index+1, srtTimestamp(segment.StartSeconds), srtTimestamp(segment.EndSeconds),
			strings.ReplaceAll(segment.Text, "\r\n", "\n"))
	}
	return builder.String()
}

func srtTimestamp(seconds float64) string {
	milliseconds := int64(math.Round(seconds * 1000))
	hours := milliseconds / 3_600_000
	milliseconds %= 3_600_000
	minutes := milliseconds / 60_000
	milliseconds %= 60_000
	wholeSeconds := milliseconds / 1000
	milliseconds %= 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", hours, minutes, wholeSeconds, milliseconds)
}
