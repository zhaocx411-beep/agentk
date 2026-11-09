package render

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"

	"agentcut/platform/blob"
	platformmedia "agentcut/platform/media"
	"agentcut/timeline"
)

const (
	LocalActivityName              = "agentcut.render.local_ffmpeg.v1"
	LocalCleanupActivityName       = "agentcut.render.local_cleanup.v1"
	maxSubtitleBytes         int64 = 1 << 20
	maxPipelineSteps               = 16
	maxCallInputs                  = 256
	maxEnvelopePoints              = 512
)

type LocalActivityInput struct {
	ProjectID       string           `json:"projectId"`
	RenderKey       string           `json:"renderKey"`
	CallID          string           `json:"callId"`
	Input           map[string]any   `json:"input"`
	Media           []timeline.Media `json:"media"`
	Width           int              `json:"width"`
	Height          int              `json:"height"`
	FPS             int              `json:"fps"`
	BackgroundColor string           `json:"backgroundColor,omitempty"`
	Final           bool             `json:"final"`
}

type LocalActivityOutput struct {
	Key string `json:"key"`
}

type LocalCleanupInput struct {
	ProjectID string   `json:"projectId"`
	RenderKey string   `json:"renderKey"`
	CallIDs   []string `json:"callIds"`
}

type LocalActivities struct {
	Store         blob.Storage
	FFmpegBinary  string
	FFprobeBinary string
	Timeout       time.Duration
	TempRoot      string
}

func (a *LocalActivities) Execute(ctx context.Context, req LocalActivityInput) (LocalActivityOutput, error) {
	if a.Store == nil {
		return LocalActivityOutput{}, nonRetryable("BLOB_NOT_CONFIGURED", "local render requires Blob Storage")
	}
	if err := validateActivityRequest(req); err != nil {
		return LocalActivityOutput{}, nonRetryable("LOCAL_RENDER_INVALID", err.Error())
	}
	ops, err := validateCallInput(req.Input)
	if err != nil {
		return LocalActivityOutput{}, nonRetryable("LOCAL_RENDER_INVALID", err.Error())
	}

	tempDir, err := os.MkdirTemp(a.TempRoot, "agentcut-render-*")
	if err != nil {
		return LocalActivityOutput{}, fmt.Errorf("create render temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	stopHeartbeats := make(chan struct{})
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				activity.RecordHeartbeat(ctx, "running", req.CallID)
			case <-ctx.Done():
				return
			case <-stopHeartbeats:
				return
			}
		}
	}()
	defer close(stopHeartbeats)

	runner := &localRunner{
		activities: a,
		request:    req,
		tempDir:    tempDir,
		probes:     make(map[string]timeline.MediaProbe),
	}
	resolved, err := runner.resolveInputs(ctx, req.Input)
	if err != nil {
		return LocalActivityOutput{}, err
	}
	activity.RecordHeartbeat(ctx, "sources-ready", req.CallID)

	log.Printf("render local start projectId=%s renderKey=%s callId=%s",
		req.ProjectID, req.RenderKey, req.CallID)
	outputPath, outputExt, err := runner.execute(ctx, resolved, ops)
	if err != nil {
		return LocalActivityOutput{}, fmt.Errorf("local ffmpeg projectId=%s renderKey=%s callId=%s: %w",
			req.ProjectID, req.RenderKey, req.CallID, err)
	}
	if err := platformmedia.ValidateFFmpegSource(outputPath); err != nil {
		return LocalActivityOutput{}, fmt.Errorf("validate render output: %w", err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		return LocalActivityOutput{}, fmt.Errorf("stat render output: %w", err)
	}

	key := IntermediateKey(req.ProjectID, req.RenderKey, req.CallID, outputExt)
	if req.Final {
		if outputExt != ".mp4" {
			return LocalActivityOutput{}, nonRetryable("LOCAL_RENDER_INVALID", "final render artifact must be MP4")
		}
		key = blob.OutputKey(req.ProjectID, req.RenderKey)
	}
	file, err := os.Open(outputPath)
	if err != nil {
		return LocalActivityOutput{}, fmt.Errorf("open render output: %w", err)
	}
	defer file.Close()
	if _, err := a.Store.Put(ctx, key, file, info.Size(), contentTypeForExt(outputExt)); err != nil {
		return LocalActivityOutput{}, fmt.Errorf("upload render output: %w", err)
	}
	activity.RecordHeartbeat(ctx, "uploaded", req.CallID)
	log.Printf("render local complete projectId=%s renderKey=%s callId=%s key=%s size=%d",
		req.ProjectID, req.RenderKey, req.CallID, key, info.Size())
	return LocalActivityOutput{Key: key}, nil
}

// Cleanup removes deterministic Blob intermediates after either workflow
// success or failure. The final output lives under blob.OutputKey and is never
// part of this deletion set.
func (a *LocalActivities) Cleanup(ctx context.Context, req LocalCleanupInput) error {
	if a.Store == nil {
		return nonRetryable("BLOB_NOT_CONFIGURED", "local render cleanup requires Blob Storage")
	}
	if !safeIDPattern.MatchString(req.ProjectID) || len(req.ProjectID) > 160 ||
		!safeIDPattern.MatchString(req.RenderKey) || len(req.RenderKey) > 160 {
		return nonRetryable("LOCAL_RENDER_INVALID", "invalid render cleanup scope")
	}
	var errs []error
	for _, callID := range req.CallIDs {
		if strings.TrimSpace(callID) == "" || len(callID) > 512 {
			return nonRetryable("LOCAL_RENDER_INVALID", "invalid render cleanup callId")
		}
		for _, ext := range []string{".mp4", ".m4a"} {
			key := IntermediateKey(req.ProjectID, req.RenderKey, callID, ext)
			if err := a.Store.Delete(ctx, key); err != nil {
				errs = append(errs, fmt.Errorf("delete %s: %w", key, err))
			}
		}
	}
	return errors.Join(errs...)
}

func nonRetryable(kind, message string) error {
	return temporal.NewNonRetryableApplicationError(message, kind, nil)
}

var safeIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func validateActivityRequest(req LocalActivityInput) error {
	if !safeIDPattern.MatchString(req.ProjectID) || len(req.ProjectID) > 160 {
		return fmt.Errorf("invalid projectId")
	}
	if !safeIDPattern.MatchString(req.RenderKey) || len(req.RenderKey) > 160 {
		return fmt.Errorf("invalid renderKey")
	}
	if strings.TrimSpace(req.CallID) == "" || len(req.CallID) > 512 {
		return fmt.Errorf("invalid callId")
	}
	if req.Width < 16 || req.Width > 8192 || req.Height < 16 || req.Height > 8192 {
		return fmt.Errorf("render dimensions must be within 16..8192")
	}
	if req.FPS < 1 || req.FPS > 120 {
		return fmt.Errorf("render fps must be within 1..120")
	}
	return nil
}

// IntermediateKey is deterministic and collision-safe even when two call IDs
// normalize to the same filesystem-friendly slug.
func IntermediateKey(projectID, renderKey, callID, ext string) string {
	if ext != ".mp4" && ext != ".m4a" {
		ext = ".mp4"
	}
	var slug strings.Builder
	for _, r := range callID {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			slug.WriteRune(r)
		} else {
			slug.WriteByte('_')
		}
		if slug.Len() >= 80 {
			break
		}
	}
	if slug.Len() == 0 {
		slug.WriteString("call")
	}
	sum := sha256.Sum256([]byte(callID))
	safe := slug.String() + "-" + hex.EncodeToString(sum[:4])
	return fmt.Sprintf("agentcut/%s/renders/%s/intermediate/%s%s", projectID, renderKey, safe, ext)
}

func contentTypeForExt(ext string) string {
	if ext == ".m4a" {
		return "audio/mp4"
	}
	return "video/mp4"
}

type validatedOp struct {
	name   string
	params map[string]any
}

func validateCallInput(input map[string]any) ([]validatedOp, error) {
	if input == nil {
		return nil, fmt.Errorf("call input is required")
	}
	if raw, ok := input["pipeline"]; ok {
		if err := onlyKeys(input, "pipeline", "video", "audio", "image"); err != nil {
			return nil, err
		}
		sourceCount := 0
		for _, key := range []string{"video", "audio", "image"} {
			if _, exists := input[key]; exists {
				sourceCount++
			}
		}
		if sourceCount != 1 {
			return nil, fmt.Errorf("pipeline requires exactly one input")
		}
		steps, err := mapSlice(raw)
		if err != nil || len(steps) == 0 || len(steps) > maxPipelineSteps {
			return nil, fmt.Errorf("pipeline must contain 1..%d operations", maxPipelineSteps)
		}
		out := make([]validatedOp, 0, len(steps))
		for _, step := range steps {
			if err := onlyKeys(step, "op", "params"); err != nil {
				return nil, err
			}
			op, err := validateOperation(step)
			if err != nil {
				return nil, err
			}
			switch op.name {
			case "clip_av", "retime_av", "apply_av_effects", "strip_audio", "add_silent_audio", "cut", "fit_audio_duration":
			default:
				return nil, fmt.Errorf("operation %q is not allowed in a single-input pipeline", op.name)
			}
			out = append(out, op)
		}
		return out, nil
	}
	op, err := validateOperation(input)
	if err != nil {
		return nil, err
	}
	allowed := map[string][]string{
		"clip_av":            {"op", "params", "video"},
		"retime_av":          {"op", "params", "video"},
		"concat_av":          {"op", "params", "videos"},
		"title_card":         {"op", "params"},
		"freeze":             {"op", "params", "image"},
		"strip_audio":        {"op", "params", "video"},
		"add_silent_audio":   {"op", "params", "video"},
		"overlay_region":     {"op", "params", "video", "video2"},
		"transition_overlay": {"op", "params", "video", "video2", "video3"},
		"transition_audio":   {"op", "params", "audio", "audio2"},
		"cut":                {"op", "params", "video", "audio"},
		"fit_audio_duration": {"op", "params", "audio"},
		"prepare_audio":      {"op", "params", "audio"},
		"mix_audio":          {"op", "params", "video", "audios"},
		"burn_subtitles":     {"op", "params", "video", "subtitle"},
	}
	if err := onlyKeys(input, allowed[op.name]...); err != nil {
		return nil, err
	}
	if err := validateSlots(input, op.name); err != nil {
		return nil, err
	}
	return []validatedOp{op}, nil
}

func validateOperation(raw map[string]any) (validatedOp, error) {
	name, ok := raw["op"].(string)
	if !ok {
		return validatedOp{}, fmt.Errorf("operation op must be a string")
	}
	params, err := stringMap(raw["params"])
	if err != nil {
		return validatedOp{}, fmt.Errorf("%s params: %w", name, err)
	}
	var allowed []string
	switch name {
	case "clip_av":
		allowed = []string{"start", "duration", "w", "h", "fit", "bg",
			"crop_x", "crop_y", "crop_width", "crop_height", "rotation_degrees"}
		err = validateSeconds(params, "start", true)
		if err == nil {
			err = validateSeconds(params, "duration", false)
		}
		if err == nil {
			err = validateOptionalVisualParams(params)
		}
	case "retime_av":
		allowed = []string{"target_duration", "w", "h", "fit", "bg",
			"crop_x", "crop_y", "crop_width", "crop_height", "rotation_degrees"}
		err = validateSeconds(params, "target_duration", false)
		if err == nil {
			err = validateOptionalVisualParams(params)
		}
	case "apply_av_effects":
		allowed = []string{"duration", "gain", "muted", "fade_in", "fade_out", "visual_fade", "w", "h", "fit", "bg",
			"crop_x", "crop_y", "crop_width", "crop_height", "rotation_degrees"}
		err = validateAVEffectsParams(params)
	case "concat_av":
		allowed = []string{"w", "h", "fps", "bg"}
		err = validateDimensions(params)
		if err == nil {
			err = validateOptionalColor(params, "bg")
		}
	case "title_card":
		allowed = []string{"text", "bg", "w", "h", "fps", "duration"}
		err = validateDimensions(params)
		if err == nil {
			err = validateSeconds(params, "duration", false)
		}
		if text, ok := params["text"].(string); !ok || len(text) > 16<<10 {
			err = fmt.Errorf("text must be a string no larger than 16 KiB")
		}
		if bg, ok := params["bg"].(string); !ok || !validColor(bg) {
			err = fmt.Errorf("bg must be a named color or #RRGGBB")
		}
	case "freeze":
		allowed = []string{"duration", "w", "h", "fps", "with_audio", "fit", "bg",
			"crop_x", "crop_y", "crop_width", "crop_height", "rotation_degrees"}
		err = validateDimensions(params)
		if err == nil {
			err = validateSeconds(params, "duration", false)
		}
		if _, ok := params["with_audio"].(bool); !ok {
			err = fmt.Errorf("with_audio must be boolean")
		}
		if err == nil {
			err = validateOptionalVisualParams(params)
		}
	case "strip_audio", "add_silent_audio":
		allowed = nil
	case "overlay_region":
		allowed = []string{"x", "y", "w", "h", "start", "end", "opacity", "fit", "bg", "fade_in", "fade_out"}
		_, err = boundedInt(params, "w", 1, 8192)
		if err == nil {
			_, err = boundedInt(params, "h", 1, 8192)
		}
		for _, key := range []string{"x", "y"} {
			if err == nil {
				_, err = boundedInt(params, key, -8192, 8192)
			}
		}
		if err == nil {
			err = validateSeconds(params, "start", true)
		}
		if err == nil {
			err = validateSeconds(params, "end", false)
		}
		if err == nil {
			start, _ := number(params["start"])
			end, _ := number(params["end"])
			if end <= start {
				err = fmt.Errorf("end must be greater than start")
			}
		}
		if err == nil {
			err = validateOptionalVisualParams(params)
		}
		if err == nil {
			err = validateOptionalUnit(params, "opacity")
		}
		if err == nil {
			err = validateOptionalSeconds(params, "fade_in")
		}
		if err == nil {
			err = validateOptionalSeconds(params, "fade_out")
		}
		if err == nil {
			start, _ := number(params["start"])
			end, _ := number(params["end"])
			fadeIn, _ := number(params["fade_in"])
			fadeOut, _ := number(params["fade_out"])
			if fadeIn+fadeOut > end-start+1e-9 {
				err = fmt.Errorf("overlay fades must not exceed item duration")
			}
		}
	case "transition_overlay":
		allowed = []string{
			"kind", "start", "duration",
			"from_x", "from_y", "from_w", "from_h", "from_opacity",
			"to_x", "to_y", "to_w", "to_h", "to_opacity",
		}
		err = validateTransitionOverlayParams(params)
	case "transition_audio":
		allowed = []string{"kind", "duration", "from_gain", "to_gain"}
		err = validateTransitionAudioParams(params)
	case "cut":
		allowed = []string{"start", "end"}
		err = validateSeconds(params, "start", true)
		if err == nil {
			err = validateSeconds(params, "end", false)
		}
		if err == nil {
			start, _ := number(params["start"])
			end, _ := number(params["end"])
			if end <= start {
				err = fmt.Errorf("end must be greater than start")
			}
		}
	case "fit_audio_duration":
		allowed = []string{"target_duration"}
		err = validateSeconds(params, "target_duration", false)
	case "prepare_audio":
		allowed = []string{"start", "source_duration", "target_duration", "fade_in", "fade_out"}
		err = validatePrepareAudioParams(params)
	case "mix_audio":
		allowed = []string{"offsets_ms", "gains", "envelopes"}
		err = validateMixParams(params)
	case "burn_subtitles":
		allowed = nil
	default:
		return validatedOp{}, fmt.Errorf("unsupported operation %q", name)
	}
	if err != nil {
		return validatedOp{}, fmt.Errorf("%s: %w", name, err)
	}
	if err := onlyKeys(params, allowed...); err != nil {
		return validatedOp{}, fmt.Errorf("%s: %w", name, err)
	}
	return validatedOp{name: name, params: params}, nil
}

func validateSlots(input map[string]any, op string) error {
	requireString := func(key string) error {
		if value, ok := input[key].(string); !ok || strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s requires string slot %s", op, key)
		}
		return nil
	}
	requireStrings := func(key string) error {
		values, err := stringSlice(input[key])
		if err != nil || len(values) == 0 || len(values) > maxCallInputs {
			return fmt.Errorf("%s requires 1..%d values in %s", op, maxCallInputs, key)
		}
		return nil
	}
	switch op {
	case "title_card":
		return nil
	case "concat_av":
		return requireStrings("videos")
	case "freeze":
		return requireString("image")
	case "overlay_region":
		if err := requireString("video"); err != nil {
			return err
		}
		return requireString("video2")
	case "transition_overlay":
		for _, key := range []string{"video", "video2", "video3"} {
			if err := requireString(key); err != nil {
				return err
			}
		}
		return nil
	case "transition_audio":
		if err := requireString("audio"); err != nil {
			return err
		}
		return requireString("audio2")
	case "fit_audio_duration", "prepare_audio":
		return requireString("audio")
	case "mix_audio":
		if err := requireString("video"); err != nil {
			return err
		}
		return requireStrings("audios")
	case "burn_subtitles":
		if err := requireString("video"); err != nil {
			return err
		}
		return requireString("subtitle")
	case "cut":
		_, video := input["video"]
		_, audio := input["audio"]
		if video == audio {
			return fmt.Errorf("cut requires exactly one of video or audio")
		}
		if video {
			return requireString("video")
		}
		return requireString("audio")
	default:
		return requireString("video")
	}
}

func onlyKeys(values map[string]any, allowed ...string) error {
	set := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		set[key] = true
	}
	for key := range values {
		if !set[key] {
			return fmt.Errorf("unexpected field %q", key)
		}
	}
	return nil
}

func stringMap(value any) (map[string]any, error) {
	if value == nil {
		return map[string]any{}, nil
	}
	out, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("must be an object")
	}
	return out, nil
}

func mapSlice(value any) ([]map[string]any, error) {
	switch typed := value.(type) {
	case []map[string]any:
		return typed, nil
	case []any:
		out := make([]map[string]any, len(typed))
		for i, item := range typed {
			var ok bool
			out[i], ok = item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("pipeline item %d must be an object", i)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("must be an array")
	}
}

func stringSlice(value any) ([]string, error) {
	switch typed := value.(type) {
	case []string:
		return typed, nil
	case []any:
		out := make([]string, len(typed))
		for i, item := range typed {
			var ok bool
			out[i], ok = item.(string)
			if !ok {
				return nil, fmt.Errorf("item %d must be a string", i)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("must be an array of strings")
	}
}

func number(value any) (float64, bool) {
	switch n := value.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}

func boundedInt(params map[string]any, key string, min, max int) (int, error) {
	value, ok := number(params[key])
	if !ok || math.Trunc(value) != value || value < float64(min) || value > float64(max) {
		return 0, fmt.Errorf("%s must be an integer within %d..%d", key, min, max)
	}
	return int(value), nil
}

func validateDimensions(params map[string]any) error {
	if _, err := boundedInt(params, "w", 16, 8192); err != nil {
		return err
	}
	if _, err := boundedInt(params, "h", 16, 8192); err != nil {
		return err
	}
	if _, exists := params["fps"]; exists {
		if _, err := boundedInt(params, "fps", 1, 120); err != nil {
			return err
		}
	}
	return nil
}

func validateOptionalVisualParams(params map[string]any) error {
	_, hasWidth := params["w"]
	_, hasHeight := params["h"]
	if hasWidth != hasHeight {
		return fmt.Errorf("w and h must be provided together")
	}
	if hasWidth {
		if _, err := boundedInt(params, "w", 1, 8192); err != nil {
			return err
		}
		if _, err := boundedInt(params, "h", 1, 8192); err != nil {
			return err
		}
	}
	if fit, exists := params["fit"]; exists {
		value, ok := fit.(string)
		if !ok || (value != timeline.ItemFitContain && value != timeline.ItemFitCover) {
			return fmt.Errorf("fit must be contain or cover")
		}
	}
	cropKeys := []string{"crop_x", "crop_y", "crop_width", "crop_height"}
	cropCount := 0
	for _, key := range cropKeys {
		if _, exists := params[key]; exists {
			cropCount++
		}
	}
	if cropCount != 0 && cropCount != len(cropKeys) {
		return fmt.Errorf("crop_x, crop_y, crop_width and crop_height must be provided together")
	}
	if cropCount != 0 {
		values := make([]float64, len(cropKeys))
		for index, key := range cropKeys {
			value, ok := number(params[key])
			if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("%s must be a finite number", key)
			}
			values[index] = value
		}
		if values[0] < 0 || values[1] < 0 || values[2] <= 0 || values[3] <= 0 ||
			values[0]+values[2] > 1+1e-12 || values[1]+values[3] > 1+1e-12 {
			return fmt.Errorf("crop rectangle must be positive and remain within normalized source bounds")
		}
	}
	if raw, exists := params["rotation_degrees"]; exists {
		value, ok := number(raw)
		if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value < -180 || value > 180 {
			return fmt.Errorf("rotation_degrees must be within [-180, 180]")
		}
	}
	return validateOptionalColor(params, "bg")
}

func validateOptionalColor(params map[string]any, key string) error {
	value, exists := params[key]
	if !exists {
		return nil
	}
	color, ok := value.(string)
	if !ok || !validColor(color) || strings.EqualFold(color, "transparent") {
		return fmt.Errorf("%s must be an opaque named color or #RRGGBB", key)
	}
	return nil
}

func validateOptionalUnit(params map[string]any, key string) error {
	value, exists := params[key]
	if !exists {
		return nil
	}
	numeric, ok := number(value)
	if !ok || math.IsNaN(numeric) || math.IsInf(numeric, 0) || numeric < 0 || numeric > 1 {
		return fmt.Errorf("%s must be within [0, 1]", key)
	}
	return nil
}

func validateTransitionOverlayParams(params map[string]any) error {
	kind, ok := params["kind"].(string)
	if !ok || (kind != "crossfade" && kind != "dipToBlack") {
		return fmt.Errorf("kind must be crossfade or dipToBlack")
	}
	if err := validateSeconds(params, "start", true); err != nil {
		return err
	}
	if err := validateSeconds(params, "duration", false); err != nil {
		return err
	}
	for _, prefix := range []string{"from", "to"} {
		if _, err := boundedInt(params, prefix+"_x", -8192, 8192); err != nil {
			return err
		}
		if _, err := boundedInt(params, prefix+"_y", -8192, 8192); err != nil {
			return err
		}
		if _, err := boundedInt(params, prefix+"_w", 1, 8192); err != nil {
			return err
		}
		if _, err := boundedInt(params, prefix+"_h", 1, 8192); err != nil {
			return err
		}
		if err := validateOptionalUnit(params, prefix+"_opacity"); err != nil {
			return err
		}
	}
	return nil
}

func validateTransitionAudioParams(params map[string]any) error {
	kind, ok := params["kind"].(string)
	if !ok || (kind != "crossfade" && kind != "dipToBlack") {
		return fmt.Errorf("kind must be crossfade or dipToBlack")
	}
	if err := validateSeconds(params, "duration", false); err != nil {
		return err
	}
	for _, key := range []string{"from_gain", "to_gain"} {
		value, ok := number(params[key])
		if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 4 {
			return fmt.Errorf("%s must be within [0, 4]", key)
		}
	}
	return nil
}

func validateAVEffectsParams(params map[string]any) error {
	if err := validateSeconds(params, "duration", false); err != nil {
		return err
	}
	gain, ok := number(params["gain"])
	if !ok || math.IsNaN(gain) || math.IsInf(gain, 0) || gain < 0 || gain > 4 {
		return fmt.Errorf("gain must be within [0, 4]")
	}
	if _, ok := params["muted"].(bool); !ok {
		return fmt.Errorf("muted must be boolean")
	}
	if _, ok := params["visual_fade"].(bool); !ok {
		return fmt.Errorf("visual_fade must be boolean")
	}
	if err := validateSeconds(params, "fade_in", true); err != nil {
		return err
	}
	if err := validateSeconds(params, "fade_out", true); err != nil {
		return err
	}
	duration, _ := number(params["duration"])
	fadeIn, _ := number(params["fade_in"])
	fadeOut, _ := number(params["fade_out"])
	if fadeIn+fadeOut > duration+1e-9 {
		return fmt.Errorf("fade durations must not exceed duration")
	}
	return validateOptionalVisualParams(params)
}

func validatePrepareAudioParams(params map[string]any) error {
	if err := validateSeconds(params, "start", true); err != nil {
		return err
	}
	if err := validateSeconds(params, "source_duration", false); err != nil {
		return err
	}
	if err := validateSeconds(params, "target_duration", false); err != nil {
		return err
	}
	if err := validateSeconds(params, "fade_in", true); err != nil {
		return err
	}
	if err := validateSeconds(params, "fade_out", true); err != nil {
		return err
	}
	target, _ := number(params["target_duration"])
	fadeIn, _ := number(params["fade_in"])
	fadeOut, _ := number(params["fade_out"])
	if fadeIn+fadeOut > target+1e-9 {
		return fmt.Errorf("fade durations must not exceed target_duration")
	}
	return nil
}

func validateSeconds(params map[string]any, key string, allowZero bool) error {
	value, ok := number(params[key])
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) ||
		value > platformmedia.MaxMediaDurationSec || value < 0 || (!allowZero && value == 0) {
		return fmt.Errorf("%s must be within %s..%d seconds", key,
			map[bool]string{true: "0", false: "(0"}[allowZero], platformmedia.MaxMediaDurationSec)
	}
	return nil
}

func validateOptionalSeconds(params map[string]any, key string) error {
	if _, exists := params[key]; !exists {
		return nil
	}
	return validateSeconds(params, key, true)
}

func validateMixParams(params map[string]any) error {
	offsets, err := numericSlice(params["offsets_ms"])
	if err != nil {
		return fmt.Errorf("offsets_ms: %w", err)
	}
	gains, err := numericSlice(params["gains"])
	if err != nil {
		return fmt.Errorf("gains: %w", err)
	}
	if len(offsets) == 0 || len(offsets) != len(gains) || len(offsets) > maxCallInputs {
		return fmt.Errorf("offsets_ms and gains must have the same length within 1..%d", maxCallInputs)
	}
	for _, offset := range offsets {
		if offset < 0 || offset > platformmedia.MaxMediaDurationSec*1000 || math.Trunc(offset) != offset {
			return fmt.Errorf("offsets_ms values must be integer milliseconds within six hours")
		}
	}
	for _, gain := range gains {
		if gain < 0 || gain > 4 || math.IsNaN(gain) || math.IsInf(gain, 0) {
			return fmt.Errorf("gains values must be within 0..4")
		}
	}
	if raw, exists := params["envelopes"]; exists {
		envelopes, err := envelopeSlice(raw)
		if err != nil {
			return fmt.Errorf("envelopes: %w", err)
		}
		if len(envelopes) != len(offsets) {
			return fmt.Errorf("envelopes must have one entry per audio input")
		}
		for envelopeIndex, points := range envelopes {
			if len(points) == 0 {
				continue
			}
			if len(points) < 2 || len(points) > maxEnvelopePoints {
				return fmt.Errorf("envelope %d must contain 0 or 2..%d points", envelopeIndex, maxEnvelopePoints)
			}
			previous := -1.0
			for pointIndex, point := range points {
				if err := onlyKeys(point, "time", "gain", "step"); err != nil {
					return fmt.Errorf("envelope %d point %d: %w", envelopeIndex, pointIndex, err)
				}
				at, ok := number(point["time"])
				if !ok || math.IsNaN(at) || math.IsInf(at, 0) ||
					at < 0 || at > platformmedia.MaxMediaDurationSec || at <= previous {
					return fmt.Errorf("envelope %d point %d time must be finite, increasing, and within six hours", envelopeIndex, pointIndex)
				}
				gain, ok := number(point["gain"])
				if !ok || math.IsNaN(gain) || math.IsInf(gain, 0) || gain <= 0 || gain > 1 {
					return fmt.Errorf("envelope %d point %d gain must be within (0, 1]", envelopeIndex, pointIndex)
				}
				if rawStep, exists := point["step"]; exists {
					if _, ok := rawStep.(bool); !ok {
						return fmt.Errorf("envelope %d point %d step must be boolean", envelopeIndex, pointIndex)
					}
				}
				previous = at
			}
		}
	}
	return nil
}

func envelopeSlice(value any) ([][]map[string]any, error) {
	switch typed := value.(type) {
	case [][]map[string]any:
		return typed, nil
	case []any:
		out := make([][]map[string]any, len(typed))
		for index, raw := range typed {
			if raw == nil {
				continue
			}
			points, err := mapSlice(raw)
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", index, err)
			}
			out[index] = points
		}
		return out, nil
	default:
		return nil, fmt.Errorf("must be an array of point arrays")
	}
}

func numericSlice(value any) ([]float64, error) {
	switch typed := value.(type) {
	case []float64:
		return typed, nil
	case []int64:
		out := make([]float64, len(typed))
		for i, value := range typed {
			out[i] = float64(value)
		}
		return out, nil
	case []any:
		out := make([]float64, len(typed))
		for i, item := range typed {
			var ok bool
			out[i], ok = number(item)
			if !ok {
				return nil, fmt.Errorf("item %d must be numeric", i)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("must be a numeric array")
	}
}

func validColor(value string) bool {
	switch strings.ToLower(value) {
	case "black", "white", "red", "green", "blue", "yellow", "gray", "grey", "transparent":
		return true
	}
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	_, err := hex.DecodeString(value[1:])
	return err == nil
}

type localRunner struct {
	activities *LocalActivities
	request    LocalActivityInput
	tempDir    string
	probes     map[string]timeline.MediaProbe
	downloaded map[string]string
}

func (r *localRunner) resolveInputs(ctx context.Context, input map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(input))
	for key, value := range input {
		switch key {
		case "video", "video2", "video3", "audio", "audio2", "image":
			ref, ok := value.(string)
			if !ok {
				return nil, nonRetryable("LOCAL_RENDER_INVALID", key+" must be a string")
			}
			path, err := r.resolveRef(ctx, ref)
			if err != nil {
				return nil, err
			}
			out[key] = path
		case "videos", "audios":
			refs, err := stringSlice(value)
			if err != nil {
				return nil, nonRetryable("LOCAL_RENDER_INVALID", key+" must be strings")
			}
			paths := make([]string, len(refs))
			for i, ref := range refs {
				paths[i], err = r.resolveRef(ctx, ref)
				if err != nil {
					return nil, err
				}
			}
			out[key] = paths
		default:
			out[key] = value
		}
	}
	return out, nil
}

func (r *localRunner) resolveRef(ctx context.Context, ref string) (string, error) {
	if mediaID, ok := IsMediaRef(ref); ok {
		for _, media := range r.request.Media {
			if media.MediaID != mediaID {
				continue
			}
			key, err := blob.ResolveRenderSourceKey(
				r.request.ProjectID, media.MediaID, media.BlobKey)
			if err != nil {
				return "", nonRetryable("RENDER_SOURCE_REJECTED",
					fmt.Sprintf("media %s source rejected: %v", mediaID, err))
			}
			return r.download(ctx, key, media.Kind)
		}
		return "", nonRetryable("RENDER_SOURCE_REJECTED",
			fmt.Sprintf("media %s is not registered in this Timeline", mediaID))
	}
	prefix := fmt.Sprintf("agentcut/%s/renders/%s/intermediate/",
		r.request.ProjectID, r.request.RenderKey)
	if !strings.HasPrefix(ref, prefix) || strings.Contains(ref, "..") {
		return "", nonRetryable("RENDER_SOURCE_REJECTED", "input is not a managed source or current render intermediate")
	}
	return r.download(ctx, ref, "")
}

func (r *localRunner) download(ctx context.Context, key string, kind timeline.MediaKind) (string, error) {
	if r.downloaded == nil {
		r.downloaded = make(map[string]string)
	}
	if path := r.downloaded[key]; path != "" {
		return path, nil
	}
	object, err := r.activities.Store.Get(ctx, key)
	if err != nil {
		return "", fmt.Errorf("download managed source %q: %w", key, err)
	}
	defer object.Body.Close()
	if object.Size == 0 || object.Size > platformmedia.MaxFFmpegSourceBytes {
		return "", nonRetryable("RENDER_SOURCE_REJECTED",
			fmt.Sprintf("managed source size %d is outside (0, 2 GiB]", object.Size))
	}
	ext := filepath.Ext(key)
	if len(ext) > 10 || ext == "" {
		ext = ".bin"
	}
	path := filepath.Join(r.tempDir, fmt.Sprintf("input-%d%s", len(r.downloaded), ext))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("create source temp file: %w", err)
	}
	written, copyErr := io.Copy(file, io.LimitReader(object.Body, platformmedia.MaxFFmpegSourceBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return "", fmt.Errorf("download managed source: %w", copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close source temp file: %w", closeErr)
	}
	if written <= 0 || written > platformmedia.MaxFFmpegSourceBytes ||
		(object.Size >= 0 && object.Size != written) {
		return "", nonRetryable("RENDER_SOURCE_REJECTED", "managed source size is invalid")
	}
	if err := platformmedia.ValidateFFmpegSource(path); err != nil {
		return "", nonRetryable("RENDER_SOURCE_REJECTED", err.Error())
	}
	if kind == timeline.MediaAudio || kind == timeline.MediaVideo {
		if _, err := r.probe(ctx, path); err != nil {
			return "", nonRetryable("RENDER_SOURCE_REJECTED", err.Error())
		}
	}
	r.downloaded[key] = path
	return path, nil
}

func (r *localRunner) probe(ctx context.Context, path string) (timeline.MediaProbe, error) {
	if probe, ok := r.probes[path]; ok {
		return probe, nil
	}
	probe, err := (platformmedia.FFProber{
		Binary: r.activities.FFprobeBinary, Timeout: 30 * time.Second,
	}).Probe(ctx, path)
	if err != nil {
		return timeline.MediaProbe{}, err
	}
	r.probes[path] = probe
	return probe, nil
}

func (r *localRunner) execute(ctx context.Context, input map[string]any, ops []validatedOp) (string, string, error) {
	if _, pipeline := input["pipeline"]; pipeline {
		var current string
		for _, key := range []string{"video", "audio", "image"} {
			if value, ok := input[key].(string); ok {
				current = value
				break
			}
		}
		for i, op := range ops {
			stepInput := map[string]any{"video": current}
			if op.name == "fit_audio_duration" {
				stepInput = map[string]any{"audio": current}
			}
			output, ext, err := r.runOperation(ctx, op, stepInput, fmt.Sprintf("pipeline-%02d", i))
			if err != nil {
				return "", "", err
			}
			current = output
			if i == len(ops)-1 {
				return output, ext, nil
			}
			activity.RecordHeartbeat(ctx, "pipeline", r.request.CallID, i+1)
		}
	}
	return r.runOperation(ctx, ops[0], input, "operation")
}

func (r *localRunner) runOperation(
	ctx context.Context, op validatedOp, input map[string]any, label string,
) (string, string, error) {
	ext, err := r.outputExtension(ctx, op.name, input)
	if err != nil {
		return "", "", err
	}
	output := filepath.Join(r.tempDir, label+ext)
	args, err := r.buildArgs(ctx, op, input, output)
	if err != nil {
		return "", "", err
	}
	args = append([]string{"-hide_banner", "-nostdin", "-loglevel", "error", "-y"}, args...)
	timeout := r.activities.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	if err := platformmedia.RunFFmpeg(ctx, r.activities.FFmpegBinary, timeout, args...); err != nil {
		return "", "", err
	}
	if err := platformmedia.ValidateFFmpegSource(output); err != nil {
		return "", "", err
	}
	return output, ext, nil
}

func (r *localRunner) outputExtension(ctx context.Context, op string, input map[string]any) (string, error) {
	if op == "fit_audio_duration" || op == "prepare_audio" || op == "transition_audio" {
		return ".m4a", nil
	}
	if op == "cut" {
		path, _ := firstString(input, "video", "audio")
		probe, err := r.probe(ctx, path)
		if err != nil {
			return "", err
		}
		if probe.Width == 0 {
			return ".m4a", nil
		}
	}
	return ".mp4", nil
}

func firstString(input map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := input[key].(string); ok && value != "" {
			return value, true
		}
	}
	return "", false
}

func (r *localRunner) buildArgs(
	ctx context.Context, op validatedOp, input map[string]any, output string,
) ([]string, error) {
	switch op.name {
	case "clip_av":
		return r.argsClip(input["video"].(string), op.params, output), nil
	case "retime_av":
		return r.argsRetime(ctx, input["video"].(string), op.params, output)
	case "apply_av_effects":
		return r.argsAVEffects(ctx, input["video"].(string), op.params, output)
	case "concat_av":
		return r.argsConcat(ctx, input["videos"].([]string), op.params, output)
	case "title_card":
		return r.argsTitle(op.params, output)
	case "freeze":
		return r.argsFreeze(input["image"].(string), op.params, output), nil
	case "strip_audio":
		return r.argsStrip(input["video"].(string), output), nil
	case "add_silent_audio":
		return r.argsAddSilence(ctx, input["video"].(string), output)
	case "overlay_region":
		return r.argsOverlay(ctx, input["video"].(string), input["video2"].(string), op.params, output)
	case "transition_overlay":
		return r.argsTransitionOverlay(
			ctx,
			input["video"].(string),
			input["video2"].(string),
			input["video3"].(string),
			op.params,
			output,
		)
	case "transition_audio":
		return r.argsTransitionAudio(
			input["audio"].(string),
			input["audio2"].(string),
			op.params,
			output,
		), nil
	case "cut":
		path, _ := firstString(input, "video", "audio")
		return r.argsCut(ctx, path, op.params, output)
	case "fit_audio_duration":
		return r.argsFitAudio(input["audio"].(string), op.params, output), nil
	case "prepare_audio":
		return r.argsPrepareAudio(ctx, input["audio"].(string), op.params, output)
	case "mix_audio":
		return r.argsMix(ctx, input["video"].(string), input["audios"].([]string), op.params, output)
	case "burn_subtitles":
		return r.argsSubtitles(ctx, input["video"].(string), input["subtitle"].(string), output)
	default:
		return nil, fmt.Errorf("unsupported operation %q", op.name)
	}
}

func (r *localRunner) scaleFilter() string {
	return r.scaleFilterFor(r.request.Width, r.request.Height, timeline.ItemFitContain, r.backgroundColor())
}

func (r *localRunner) backgroundColor() string {
	background := strings.TrimSpace(r.request.BackgroundColor)
	if background == "" || !validColor(background) || strings.EqualFold(background, "transparent") {
		return "black"
	}
	return background
}

func ffmpegColor(color string) string {
	if strings.HasPrefix(color, "#") {
		return "0x" + strings.TrimPrefix(color, "#")
	}
	return color
}

func (r *localRunner) scaleFilterFor(width, height int, fit, background string) string {
	if fit == "" {
		fit = timeline.ItemFitContain
	}
	if background == "" {
		background = r.backgroundColor()
	}
	if fit == timeline.ItemFitCover {
		return fmt.Sprintf(
			"scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d,setsar=1,fps=%d",
			width, height, width, height, r.request.FPS)
	}
	return fmt.Sprintf(
		"scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=%s,setsar=1,fps=%d",
		width, height, width, height, ffmpegColor(background), r.request.FPS)
}

func (r *localRunner) visualScaleFilter(params map[string]any) string {
	width, height := r.request.Width, r.request.Height
	if value, exists := params["w"]; exists {
		width, _ = boundedInt(params, "w", 1, 8192)
		_ = value
		height, _ = boundedInt(params, "h", 1, 8192)
	}
	fit, _ := params["fit"].(string)
	background, _ := params["bg"].(string)
	if background == "" {
		background = r.backgroundColor()
	}
	rotated := false
	filters := make([]string, 0, 3)
	if _, exists := params["crop_x"]; exists {
		x, _ := number(params["crop_x"])
		y, _ := number(params["crop_y"])
		cropWidth, _ := number(params["crop_width"])
		cropHeight, _ := number(params["crop_height"])
		left := fmt.Sprintf("floor(iw*%s)", filterNumber(x))
		top := fmt.Sprintf("floor(ih*%s)", filterNumber(y))
		right := fmt.Sprintf("ceil(iw*%s)", filterNumber(x+cropWidth))
		bottom := fmt.Sprintf("ceil(ih*%s)", filterNumber(y+cropHeight))
		filters = append(filters, fmt.Sprintf(
			"crop=w='max(1,%s-%s)':h='max(1,%s-%s)':x='%s':y='%s'",
			right, left, bottom, top, left, top))
	}
	if raw, exists := params["rotation_degrees"]; exists {
		degrees, _ := number(raw)
		if math.Abs(degrees) > 1e-12 {
			rotated = true
			radians := degrees * math.Pi / 180
			filters = append(filters, fmt.Sprintf(
				"format=rgba,rotate=%s:ow=rotw(%s):oh=roth(%s):c=none",
				filterNumber(radians), filterNumber(radians), filterNumber(radians)))
		}
	}
	filters = append(filters, r.scaleFilterFor(width, height, fit, background))
	if rotated {
		red, green, blue := colorRGB(background)
		filters = append(filters, fmt.Sprintf(
			"format=rgba,geq="+
				"r='r(X,Y)*alpha(X,Y)/255+%d*(1-alpha(X,Y)/255)':"+
				"g='g(X,Y)*alpha(X,Y)/255+%d*(1-alpha(X,Y)/255)':"+
				"b='b(X,Y)*alpha(X,Y)/255+%d*(1-alpha(X,Y)/255)':a=255",
			red, green, blue))
	}
	return strings.Join(filters, ",")
}

func colorRGB(value string) (int, int, int) {
	switch strings.ToLower(value) {
	case "white":
		return 255, 255, 255
	case "red":
		return 255, 0, 0
	case "green":
		return 0, 128, 0
	case "blue":
		return 0, 0, 255
	case "yellow":
		return 255, 255, 0
	case "gray", "grey":
		return 128, 128, 128
	}
	if len(value) == 7 && value[0] == '#' {
		decoded, err := hex.DecodeString(value[1:])
		if err == nil && len(decoded) == 3 {
			return int(decoded[0]), int(decoded[1]), int(decoded[2])
		}
	}
	return 0, 0, 0
}

func (r *localRunner) videoCodecs() []string {
	pixelFormat := "yuv420p"
	if r.request.Width%2 != 0 || r.request.Height%2 != 0 {
		// 4:2:0 chroma subsampling requires even dimensions. Preserve the
		// authoritative odd canvas rather than silently padding/cropping it.
		pixelFormat = "yuv444p"
	}
	return []string{
		"-c:v", "libx264", "-preset", "veryfast", "-pix_fmt", pixelFormat,
		"-r", strconv.Itoa(r.request.FPS),
		"-c:a", "aac", "-ar", "48000", "-ac", "2",
		"-movflags", "+faststart", "-max_muxing_queue_size", "1024",
	}
}

func (r *localRunner) argsClip(path string, params map[string]any, output string) []string {
	start, _ := number(params["start"])
	duration, _ := number(params["duration"])
	args := []string{
		"-ss", seconds(start), "-i", path, "-t", seconds(duration),
		"-map", "0:v:0", "-map", "0:a:0?", "-vf", r.visualScaleFilter(params),
		"-af", "aresample=48000",
	}
	return append(append(args, r.videoCodecs()...), output)
}

func (r *localRunner) argsRetime(
	ctx context.Context, path string, params map[string]any, output string,
) ([]string, error) {
	probe, err := r.probe(ctx, path)
	if err != nil {
		return nil, err
	}
	target, _ := number(params["target_duration"])
	source := float64(probe.DurationMs) / 1000
	ratio := target / source
	videoFilter := fmt.Sprintf("setpts=%.9f*PTS,%s", ratio, r.visualScaleFilter(params))
	args := []string{"-i", path, "-t", seconds(target), "-map", "0:v:0", "-vf", videoFilter}
	if probe.HasAudio {
		args = append(args, "-map", "0:a:0", "-af", atempoFilter(source/target))
	} else {
		args = append(args, "-an")
	}
	return append(append(args, r.videoCodecs()...), output), nil
}

func atempoFilter(speed float64) string {
	var filters []string
	for speed > 2 {
		filters = append(filters, "atempo=2")
		speed /= 2
	}
	for speed < 0.5 {
		filters = append(filters, "atempo=0.5")
		speed /= 0.5
	}
	filters = append(filters, fmt.Sprintf("atempo=%.9f", speed))
	return strings.Join(filters, ",")
}

func (r *localRunner) argsAVEffects(
	ctx context.Context, path string, params map[string]any, output string,
) ([]string, error) {
	probe, err := r.probe(ctx, path)
	if err != nil {
		return nil, err
	}
	duration, _ := number(params["duration"])
	gain, _ := number(params["gain"])
	muted, _ := params["muted"].(bool)
	visualFade, _ := params["visual_fade"].(bool)
	fadeIn, _ := number(params["fade_in"])
	fadeOut, _ := number(params["fade_out"])
	background, _ := params["bg"].(string)
	if background == "" {
		background = r.backgroundColor()
	}

	videoFilters := []string{r.visualScaleFilter(params)}
	if visualFade && fadeIn > 0 {
		videoFilters = append(videoFilters, fmt.Sprintf(
			"fade=t=in:st=0:d=%s:color=%s", seconds(fadeIn), ffmpegColor(background)))
	}
	if visualFade && fadeOut > 0 {
		videoFilters = append(videoFilters, fmt.Sprintf(
			"fade=t=out:st=%s:d=%s:color=%s",
			seconds(duration-fadeOut), seconds(fadeOut), ffmpegColor(background)))
	}
	args := []string{
		"-i", path, "-t", seconds(duration), "-map", "0:v:0",
		"-vf", strings.Join(videoFilters, ","),
	}
	if probe.HasAudio {
		if muted {
			gain = 0
		}
		audioFilters := []string{"aresample=48000", "volume=" + filterNumber(gain)}
		if fadeIn > 0 {
			audioFilters = append(audioFilters,
				fmt.Sprintf("afade=t=in:st=0:d=%s", seconds(fadeIn)))
		}
		if fadeOut > 0 {
			audioFilters = append(audioFilters, fmt.Sprintf(
				"afade=t=out:st=%s:d=%s", seconds(duration-fadeOut), seconds(fadeOut)))
		}
		args = append(args, "-map", "0:a:0", "-af", strings.Join(audioFilters, ","))
	} else {
		args = append(args, "-an")
	}
	return append(append(args, r.videoCodecs()...), output), nil
}

func (r *localRunner) argsConcat(
	ctx context.Context, paths []string, params map[string]any, output string,
) ([]string, error) {
	var args []string
	var filters []string
	var concatInputs strings.Builder
	nextInput := len(paths)
	for _, path := range paths {
		args = append(args, "-i", path)
	}
	for i, path := range paths {
		probe, err := r.probe(ctx, path)
		if err != nil {
			return nil, err
		}
		filters = append(filters, fmt.Sprintf(
			"[%d:v:0]%s,setpts=PTS-STARTPTS[v%d]", i, r.visualScaleFilter(params), i))
		audioLabel := fmt.Sprintf("a%d", i)
		if probe.HasAudio {
			filters = append(filters, fmt.Sprintf(
				"[%d:a:0]aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo,asetpts=PTS-STARTPTS[%s]",
				i, audioLabel))
		} else {
			duration := float64(probe.DurationMs) / 1000
			args = append(args, "-f", "lavfi", "-t", seconds(duration), "-i",
				"anullsrc=channel_layout=stereo:sample_rate=48000")
			filters = append(filters, fmt.Sprintf("[%d:a:0]asetpts=PTS-STARTPTS[%s]", nextInput, audioLabel))
			nextInput++
		}
		fmt.Fprintf(&concatInputs, "[v%d][a%d]", i, i)
	}
	filters = append(filters, fmt.Sprintf("%sconcat=n=%d:v=1:a=1[v][a]", concatInputs.String(), len(paths)))
	args = append(args, "-filter_complex", strings.Join(filters, ";"), "-map", "[v]", "-map", "[a]")
	return append(append(args, r.videoCodecs()...), output), nil
}

func (r *localRunner) argsTitle(params map[string]any, output string) ([]string, error) {
	duration, _ := number(params["duration"])
	bg := params["bg"].(string)
	if strings.HasPrefix(bg, "#") {
		bg = "0x" + strings.TrimPrefix(bg, "#")
	}
	text := params["text"].(string)
	args := []string{
		"-f", "lavfi", "-i", fmt.Sprintf("color=c=%s:s=%dx%d:r=%d:d=%s",
			bg, r.request.Width, r.request.Height, r.request.FPS, seconds(duration)),
		"-f", "lavfi", "-t", seconds(duration), "-i",
		"anullsrc=channel_layout=stereo:sample_rate=48000",
	}
	if text != "" {
		imagePath, err := rasterizeTitleText(text, r.request.Width, r.request.Height, r.tempDir)
		if err != nil {
			return nil, fmt.Errorf("rasterize title text: %w", err)
		}
		args = append(args,
			"-loop", "1", "-framerate", strconv.Itoa(r.request.FPS), "-i", imagePath,
			"-filter_complex", "[0:v:0][2:v:0]overlay=0:0:format=auto[v]",
			"-map", "[v]", "-map", "1:a:0")
	} else {
		args = append(args, "-map", "0:v:0", "-map", "1:a:0")
	}
	args = append(args, "-t", seconds(duration))
	return append(append(args, r.videoCodecs()...), output), nil
}

func (r *localRunner) argsFreeze(path string, params map[string]any, output string) []string {
	duration, _ := number(params["duration"])
	withAudio := params["with_audio"].(bool)
	args := []string{"-loop", "1", "-framerate", strconv.Itoa(r.request.FPS), "-i", path}
	if withAudio {
		args = append(args,
			"-f", "lavfi", "-t", seconds(duration), "-i",
			"anullsrc=channel_layout=stereo:sample_rate=48000",
			"-map", "0:v:0", "-map", "1:a:0")
	} else {
		args = append(args, "-map", "0:v:0", "-an")
	}
	args = append(args, "-t", seconds(duration), "-vf", r.visualScaleFilter(params))
	return append(append(args, r.videoCodecs()...), output)
}

func (r *localRunner) argsStrip(path, output string) []string {
	args := []string{"-i", path, "-map", "0:v:0", "-an", "-vf", r.scaleFilter()}
	return append(append(args, r.videoCodecs()...), output)
}

func (r *localRunner) argsAddSilence(ctx context.Context, path, output string) ([]string, error) {
	probe, err := r.probe(ctx, path)
	if err != nil {
		return nil, err
	}
	duration := float64(probe.DurationMs) / 1000
	args := []string{
		"-i", path,
		"-f", "lavfi", "-t", seconds(duration), "-i",
		"anullsrc=channel_layout=stereo:sample_rate=48000",
		"-map", "0:v:0", "-map", "1:a:0", "-t", seconds(duration),
		"-vf", r.scaleFilter(),
	}
	return append(append(args, r.videoCodecs()...), output), nil
}

func (r *localRunner) argsOverlay(
	ctx context.Context, base, upper string, params map[string]any, output string,
) ([]string, error) {
	probe, err := r.probe(ctx, base)
	if err != nil {
		return nil, err
	}
	x, _ := boundedInt(params, "x", -8192, 8192)
	y, _ := boundedInt(params, "y", -8192, 8192)
	width, _ := boundedInt(params, "w", 1, 8192)
	height, _ := boundedInt(params, "h", 1, 8192)
	start, _ := number(params["start"])
	end, _ := number(params["end"])
	opacity := 1.0
	if value, exists := params["opacity"]; exists {
		opacity, _ = number(value)
		if opacity == 0 {
			opacity = 1
		}
	}
	fit, _ := params["fit"].(string)
	background, _ := params["bg"].(string)
	upperScale := r.scaleFilterFor(width, height, fit, background)
	fadeIn, _ := number(params["fade_in"])
	fadeOut, _ := number(params["fade_out"])
	upperFilters := []string{upperScale, "format=rgba"}
	if fadeIn > 0 {
		upperFilters = append(upperFilters,
			fmt.Sprintf("fade=t=in:st=0:d=%s:alpha=1", seconds(fadeIn)))
	}
	if fadeOut > 0 {
		upperFilters = append(upperFilters, fmt.Sprintf(
			"fade=t=out:st=%s:d=%s:alpha=1", seconds(end-start-fadeOut), seconds(fadeOut)))
	}
	upperFilters = append(upperFilters,
		"colorchannelmixer=aa="+filterNumber(opacity),
		fmt.Sprintf("setpts=PTS-STARTPTS+%.6f/TB", start))
	filter := fmt.Sprintf(
		"[0:v:0]%s[base];[1:v:0]%s[upper];[base][upper]overlay=%d:%d:enable='between(t,%.6f,%.6f)':eof_action=pass[v]",
		r.scaleFilter(), strings.Join(upperFilters, ","), x, y, start, end)
	args := []string{
		"-i", base, "-i", upper, "-filter_complex", filter,
		"-map", "[v]", "-map", "0:a:0?", "-t", seconds(float64(probe.DurationMs) / 1000),
	}
	return append(append(args, r.videoCodecs()...), output), nil
}

func (r *localRunner) argsTransitionOverlay(
	ctx context.Context,
	base, from, to string,
	params map[string]any,
	output string,
) ([]string, error) {
	probe, err := r.probe(ctx, base)
	if err != nil {
		return nil, err
	}
	start, _ := number(params["start"])
	duration, _ := number(params["duration"])
	kind, _ := params["kind"].(string)
	transition := "fade"
	if kind == "dipToBlack" {
		transition = "fadeblack"
	}
	fromX, _ := boundedInt(params, "from_x", -8192, 8192)
	fromY, _ := boundedInt(params, "from_y", -8192, 8192)
	fromWidth, _ := boundedInt(params, "from_w", 1, 8192)
	fromHeight, _ := boundedInt(params, "from_h", 1, 8192)
	toX, _ := boundedInt(params, "to_x", -8192, 8192)
	toY, _ := boundedInt(params, "to_y", -8192, 8192)
	toWidth, _ := boundedInt(params, "to_w", 1, 8192)
	toHeight, _ := boundedInt(params, "to_h", 1, 8192)
	fromOpacity, toOpacity := 1.0, 1.0
	if raw, exists := params["from_opacity"]; exists {
		fromOpacity, _ = number(raw)
	}
	if raw, exists := params["to_opacity"]; exists {
		toOpacity, _ = number(raw)
	}
	canvasWidth, canvasHeight := r.request.Width, r.request.Height
	fromFilter := fmt.Sprintf(
		"scale=%d:%d,format=rgba,colorchannelmixer=aa=%s,pad=%d:%d:%d:%d:color=black@0,setsar=1,fps=%d,settb=AVTB,setpts=PTS-STARTPTS",
		fromWidth, fromHeight, filterNumber(fromOpacity), canvasWidth, canvasHeight, fromX, fromY, r.request.FPS)
	toFilter := fmt.Sprintf(
		"scale=%d:%d,format=rgba,colorchannelmixer=aa=%s,pad=%d:%d:%d:%d:color=black@0,setsar=1,fps=%d,settb=AVTB,setpts=PTS-STARTPTS",
		toWidth, toHeight, filterNumber(toOpacity), canvasWidth, canvasHeight, toX, toY, r.request.FPS)
	filter := fmt.Sprintf(
		"[0:v:0]%s[base];[1:v:0]%s[from];[2:v:0]%s[to];"+
			"[from][to]xfade=transition=%s:duration=%s:offset=0,trim=duration=%s,setpts=PTS-STARTPTS+%s/TB[transition];"+
			"[base][transition]overlay=0:0:enable='between(t,%s,%s)':eof_action=pass[v]",
		r.scaleFilter(), fromFilter, toFilter,
		transition, seconds(duration), seconds(duration), seconds(start),
		seconds(start), seconds(start+duration))
	args := []string{
		"-i", base, "-i", from, "-i", to,
		"-filter_complex", filter,
		"-map", "[v]", "-map", "0:a:0?",
		"-t", seconds(float64(probe.DurationMs) / 1000),
	}
	return append(append(args, r.videoCodecs()...), output), nil
}

func (r *localRunner) argsTransitionAudio(
	from, to string,
	params map[string]any,
	output string,
) []string {
	duration, _ := number(params["duration"])
	kind, _ := params["kind"].(string)
	fromGain, _ := number(params["from_gain"])
	toGain, _ := number(params["to_gain"])
	filter := fmt.Sprintf(
		"[0:a:0]volume=%s[from];[1:a:0]volume=%s[to];"+
			"[from][to]acrossfade=d=%s:c1=tri:c2=tri[a]",
		filterNumber(fromGain), filterNumber(toGain), seconds(duration))
	if kind == "dipToBlack" {
		half := duration / 2
		delay := int64(math.Round(half * 1000))
		filter = fmt.Sprintf(
			"[0:a:0]aresample=48000,volume=%s,atrim=0:%s,asetpts=PTS-STARTPTS,afade=t=out:st=0:d=%s[a0];"+
				"[1:a:0]aresample=48000,volume=%s,atrim=%s:%s,asetpts=PTS-STARTPTS,afade=t=in:st=0:d=%s,adelay=%d:all=1[a1];"+
				"[a0][a1]amix=inputs=2:duration=longest:normalize=0,atrim=0:%s[a]",
			filterNumber(fromGain), seconds(half), seconds(half),
			filterNumber(toGain), seconds(half), seconds(duration), seconds(half), delay,
			seconds(duration))
	}
	return []string{
		"-i", from, "-i", to,
		"-filter_complex", filter,
		"-map", "[a]", "-vn", "-t", seconds(duration),
		"-c:a", "aac", "-ar", "48000", "-ac", "2", output,
	}
}

func (r *localRunner) argsCut(
	ctx context.Context, path string, params map[string]any, output string,
) ([]string, error) {
	probe, err := r.probe(ctx, path)
	if err != nil {
		return nil, err
	}
	start, _ := number(params["start"])
	end, _ := number(params["end"])
	duration := end - start
	args := []string{"-ss", seconds(start), "-i", path, "-t", seconds(duration)}
	if probe.Width == 0 {
		return append(args, "-vn", "-map", "0:a:0", "-c:a", "aac", "-ar", "48000", "-ac", "2", output), nil
	}
	args = append(args, "-map", "0:v:0", "-map", "0:a:0?", "-vf", r.scaleFilter())
	return append(append(args, r.videoCodecs()...), output), nil
}

func (r *localRunner) argsFitAudio(path string, params map[string]any, output string) []string {
	duration, _ := number(params["target_duration"])
	return []string{
		"-i", path, "-vn", "-map", "0:a:0",
		"-af", fmt.Sprintf("aresample=48000,apad,atrim=0:%s", seconds(duration)),
		"-t", seconds(duration), "-c:a", "aac", "-ar", "48000", "-ac", "2", output,
	}
}

func (r *localRunner) argsPrepareAudio(
	ctx context.Context, path string, params map[string]any, output string,
) ([]string, error) {
	start, _ := number(params["start"])
	sourceDuration, _ := number(params["source_duration"])
	targetDuration, _ := number(params["target_duration"])
	fadeIn, _ := number(params["fade_in"])
	fadeOut, _ := number(params["fade_out"])
	probe, err := r.probe(ctx, path)
	if err != nil {
		return nil, err
	}
	if !probe.HasAudio {
		return []string{
			"-f", "lavfi", "-t", seconds(targetDuration), "-i",
			"anullsrc=channel_layout=stereo:sample_rate=48000",
			"-vn", "-map", "0:a:0", "-t", seconds(targetDuration),
			"-c:a", "aac", "-ar", "48000", "-ac", "2", output,
		}, nil
	}
	filters := []string{
		"aresample=48000",
		fmt.Sprintf("atrim=0:%s", seconds(sourceDuration)),
		"asetpts=PTS-STARTPTS",
		atempoFilter(sourceDuration / targetDuration),
	}
	if fadeIn > 0 {
		filters = append(filters, fmt.Sprintf("afade=t=in:st=0:d=%s", seconds(fadeIn)))
	}
	if fadeOut > 0 {
		filters = append(filters, fmt.Sprintf(
			"afade=t=out:st=%s:d=%s",
			seconds(targetDuration-fadeOut), seconds(fadeOut)))
	}
	filters = append(filters,
		fmt.Sprintf("apad,atrim=0:%s", seconds(targetDuration)))
	return []string{
		"-ss", seconds(start), "-i", path,
		"-vn", "-map", "0:a:0", "-af", strings.Join(filters, ","),
		"-t", seconds(targetDuration), "-c:a", "aac", "-ar", "48000", "-ac", "2", output,
	}, nil
}

func (r *localRunner) argsMix(
	ctx context.Context, video string, audios []string, params map[string]any, output string,
) ([]string, error) {
	baseProbe, err := r.probe(ctx, video)
	if err != nil {
		return nil, err
	}
	offsets, _ := numericSlice(params["offsets_ms"])
	gains, _ := numericSlice(params["gains"])
	envelopes := make([][]map[string]any, len(audios))
	if raw, exists := params["envelopes"]; exists {
		envelopes, _ = envelopeSlice(raw)
	}
	args := []string{"-i", video}
	for _, audio := range audios {
		args = append(args, "-i", audio)
	}
	duration := float64(baseProbe.DurationMs) / 1000
	silenceIndex := len(audios) + 1
	args = append(args, "-f", "lavfi", "-t", seconds(duration), "-i",
		"anullsrc=channel_layout=stereo:sample_rate=48000")
	filters := []string{
		fmt.Sprintf("[%d:a:0]atrim=0:%s,asetpts=PTS-STARTPTS[silence]", silenceIndex, seconds(duration)),
	}
	labels := []string{"[silence]"}
	if baseProbe.HasAudio {
		filters = append(filters, "[0:a:0]aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo,asetpts=PTS-STARTPTS[basea]")
		labels = append(labels, "[basea]")
	}
	for i := range audios {
		label := fmt.Sprintf("mix%d", i)
		chain := fmt.Sprintf(
			"[%d:a:0]aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo,volume=%s",
			i+1, filterNumber(gains[i]))
		if len(envelopes[i]) > 0 {
			chain += ",volume='" + audioEnvelopeExpression(envelopes[i]) + "':eval=frame"
		}
		chain += fmt.Sprintf(",adelay=%d:all=1[%s]", int64(offsets[i]), label)
		filters = append(filters, chain)
		labels = append(labels, "["+label+"]")
	}
	filters = append(filters, fmt.Sprintf("%samix=inputs=%d:duration=first:normalize=0[a]",
		strings.Join(labels, ""), len(labels)))
	args = append(args,
		"-filter_complex", strings.Join(filters, ";"),
		"-map", "0:v:0", "-map", "[a]", "-t", seconds(duration), "-vf", r.scaleFilter())
	return append(append(args, r.videoCodecs()...), output), nil
}

func audioEnvelopeExpression(points []map[string]any) string {
	lastGain, _ := number(points[len(points)-1]["gain"])
	expression := filterNumber(lastGain)
	for index := len(points) - 2; index >= 0; index-- {
		fromTime, _ := number(points[index]["time"])
		toTime, _ := number(points[index+1]["time"])
		fromGain, _ := number(points[index]["gain"])
		toGain, _ := number(points[index+1]["gain"])
		step, _ := points[index+1]["step"].(bool)
		segment := filterNumber(fromGain)
		if !step {
			position := fmt.Sprintf(
				"max(0,min(1,(t-%s)/%s))",
				filterNumber(fromTime), filterNumber(toTime-fromTime))
			segment = fmt.Sprintf(
				"%s+(%s-%s)*%s",
				filterNumber(fromGain), filterNumber(toGain), filterNumber(fromGain), position)
		}
		expression = fmt.Sprintf("if(lt(t,%s),%s,%s)",
			filterNumber(toTime), segment, expression)
	}
	return expression
}

func (r *localRunner) argsSubtitles(
	ctx context.Context, video, encoded, output string,
) ([]string, error) {
	prefix := "base64:"
	styled := false
	if strings.HasPrefix(encoded, "styled-caption-v1:") {
		prefix = "styled-caption-v1:"
		styled = true
	} else if !strings.HasPrefix(encoded, prefix) {
		return nil, nonRetryable("LOCAL_RENDER_INVALID", "subtitle must use a supported versioned base64 encoding")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, prefix))
	if err != nil {
		return nil, nonRetryable("LOCAL_RENDER_INVALID", "subtitle is not valid base64")
	}
	if len(raw) == 0 || int64(len(raw)) > maxSubtitleBytes {
		return nil, nonRetryable("LOCAL_RENDER_INVALID", "decoded subtitle must be within 1 byte..1 MiB")
	}
	var starts, ends []float64
	var images []string
	if styled {
		payload, parseErr := parseStyledSubtitlePayload(raw)
		if parseErr != nil {
			return nil, nonRetryable("LOCAL_RENDER_INVALID", parseErr.Error())
		}
		images, err = rasterizeStyledSubtitles(
			payload.Entries, r.request.Width, r.request.Height, r.tempDir)
		starts = make([]float64, len(payload.Entries))
		ends = make([]float64, len(payload.Entries))
		for index, entry := range payload.Entries {
			starts[index], ends[index] = entry.Start, entry.End
		}
	} else {
		entries, parseErr := parseSRT(raw)
		if parseErr != nil {
			return nil, nonRetryable("LOCAL_RENDER_INVALID", parseErr.Error())
		}
		images, err = rasterizeSubtitles(
			entries, r.request.Width, r.request.Height, r.tempDir)
		starts = make([]float64, len(entries))
		ends = make([]float64, len(entries))
		for index, entry := range entries {
			starts[index], ends[index] = entry.start.Seconds(), entry.end.Seconds()
		}
	}
	if err != nil {
		return nil, fmt.Errorf("rasterize subtitles: %w", err)
	}
	probe, err := r.probe(ctx, video)
	if err != nil {
		return nil, err
	}
	args := []string{"-i", video}
	for _, imagePath := range images {
		args = append(args, "-loop", "1", "-framerate", strconv.Itoa(r.request.FPS), "-i", imagePath)
	}
	filters := []string{fmt.Sprintf("[0:v:0]%s[v0]", r.scaleFilter())}
	previous := "v0"
	for i := range images {
		caption := fmt.Sprintf("caption%d", i)
		next := fmt.Sprintf("v%d", i+1)
		start := starts[i]
		end := ends[i]
		filters = append(filters,
			fmt.Sprintf("[%d:v:0]format=rgba,setpts=PTS-STARTPTS+%.6f/TB[%s]",
				i+1, start, caption),
			fmt.Sprintf("[%s][%s]overlay=0:0:enable='between(t,%.6f,%.6f)':eof_action=pass[%s]",
				previous, caption, start, end, next))
		previous = next
	}
	args = append(args,
		"-filter_complex", strings.Join(filters, ";"),
		"-map", "["+previous+"]", "-map", "0:a:0?",
		"-t", seconds(float64(probe.DurationMs)/1000))
	return append(append(args, r.videoCodecs()...), output), nil
}

func seconds(value float64) string {
	return strconv.FormatFloat(value, 'f', 6, 64)
}

func filterNumber(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}
