package render

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"agentcut/timeline"
)

type frameInterval struct {
	Start int64
	End   int64
}

type gainPoint struct {
	Frame int64
	Gain  float64
	Step  bool
}

func transitionIndexes(doc timeline.Doc) (
	map[string]timeline.Transition,
	map[string]timeline.Transition,
) {
	incoming := make(map[string]timeline.Transition, len(doc.Transitions))
	outgoing := make(map[string]timeline.Transition, len(doc.Transitions))
	for _, transition := range doc.Transitions {
		incoming[transition.ToItemID] = transition
		outgoing[transition.FromItemID] = transition
	}
	return incoming, outgoing
}

// transitionAdjustedItem applies the shared edge-ownership rule used by the
// preview: an incoming transition replaces fade-in and an outgoing transition
// replaces fade-out. Fades on the opposite edge remain unchanged.
func transitionAdjustedItem(
	item timeline.Item,
	incoming map[string]timeline.Transition,
	outgoing map[string]timeline.Transition,
) timeline.Item {
	if _, exists := incoming[item.ItemID]; exists {
		item.FadeInFrames = 0
	}
	if _, exists := outgoing[item.ItemID]; exists {
		item.FadeOutFrames = 0
	}
	return item
}

func itemByID(doc timeline.Doc, itemID string) (timeline.Item, bool) {
	for _, item := range doc.Items {
		if item.ItemID == itemID {
			return item, true
		}
	}
	return timeline.Item{}, false
}

func addVisualTransformParams(params map[string]any, item timeline.Item) {
	if item.Transform == nil {
		return
	}
	if crop := item.Transform.Crop; crop != nil {
		params["crop_x"] = crop.X
		params["crop_y"] = crop.Y
		params["crop_width"] = crop.Width
		params["crop_height"] = crop.Height
	}
	if item.Transform.RotationDegrees != 0 {
		params["rotation_degrees"] = item.Transform.RotationDegrees
	}
}

func hasM3BVisualTransform(item timeline.Item) bool {
	return item.Transform != nil &&
		(item.Transform.Crop != nil || item.Transform.RotationDegrees != 0)
}

func trackHasTransition(doc timeline.Doc, trackID string) bool {
	for _, transition := range doc.Transitions {
		from, fromOK := itemByID(doc, transition.FromItemID)
		if fromOK && from.TrackID == trackID {
			return true
		}
	}
	return false
}

func requiresCanvasComposition(doc timeline.Doc, trackID string) bool {
	if trackHasTransform(doc, trackID) || trackHasTransition(doc, trackID) {
		return true
	}
	for _, item := range doc.Items {
		if item.TrackID == trackID && hasM3BVisualTransform(item) {
			return true
		}
	}
	return false
}

func duckEnvelope(doc timeline.Doc, target timeline.Track, totalFrames int64) []gainPoint {
	if target.Ducking == nil || len(target.Ducking.SidechainTrackIDs) == 0 {
		return nil
	}
	sidechains := make(map[string]bool, len(target.Ducking.SidechainTrackIDs))
	for _, trackID := range target.Ducking.SidechainTrackIDs {
		sidechains[trackID] = true
	}
	tracks := make(map[string]timeline.Track, len(doc.Tracks))
	for _, track := range doc.Tracks {
		tracks[track.TrackID] = track
	}
	media := make(map[string]timeline.Media, len(doc.Media))
	for _, source := range doc.Media {
		media[source.MediaID] = source
	}
	intervals := make([]frameInterval, 0)
	for _, item := range doc.Items {
		if !sidechains[item.TrackID] || item.Muted {
			continue
		}
		track, ok := tracks[item.TrackID]
		if !ok || track.Muted {
			continue
		}
		hasSound := item.Kind == timeline.ItemAudio
		if item.Kind == timeline.ItemVideo {
			source, exists := media[item.MediaID]
			hasSound = exists && source.Probe != nil && source.Probe.HasAudio
		}
		if !hasSound {
			continue
		}
		intervals = append(intervals, frameInterval{
			Start: max(int64(0), item.From),
			End:   min(totalFrames, item.End()),
		})
	}
	intervals = mergeFrameIntervals(intervals)
	if len(intervals) == 0 {
		return nil
	}

	config := target.Ducking
	values := map[int64]float64{0: 1, totalFrames: 1}
	steps := make(map[int64]bool)
	putMin := func(frame int64, gain float64) {
		frame = min(totalFrames, max(int64(0), frame))
		gain = min(1.0, max(config.Gain, gain))
		if current, exists := values[frame]; !exists || gain < current {
			values[frame] = gain
		}
	}
	for _, interval := range intervals {
		attackStart := max(int64(0), interval.Start-config.AttackFrames)
		if config.AttackFrames == 0 || attackStart == interval.Start {
			if interval.Start > 0 {
				putMin(interval.Start-1, 1)
			}
			putMin(interval.Start, config.Gain)
			steps[interval.Start] = true
		} else {
			for frame := attackStart; frame <= interval.Start; frame++ {
				gain := 1 - (1-config.Gain)*
					float64(frame-attackStart)/float64(interval.Start-attackStart)
				putMin(frame, gain)
			}
		}
		putMin(interval.Start, config.Gain)
		if interval.End > interval.Start {
			putMin(interval.End-1, config.Gain)
		}
		releaseEnd := min(totalFrames, interval.End+config.ReleaseFrames)
		if config.ReleaseFrames == 0 || releaseEnd == interval.End {
			putMin(interval.End, 1)
			steps[interval.End] = true
		} else {
			for frame := interval.End; frame <= releaseEnd; frame++ {
				gain := config.Gain + (1-config.Gain)*
					float64(frame-interval.End)/float64(releaseEnd-interval.End)
				putMin(frame, gain)
			}
		}
	}
	frames := make([]int64, 0, len(values))
	for frame := range values {
		frames = append(frames, frame)
	}
	sort.Slice(frames, func(i, j int) bool { return frames[i] < frames[j] })
	points := make([]gainPoint, 0, len(frames))
	for _, frame := range frames {
		points = append(points, gainPoint{
			Frame: frame, Gain: values[frame], Step: steps[frame],
		})
	}
	return compressGainPoints(points)
}

func mergeFrameIntervals(intervals []frameInterval) []frameInterval {
	sort.SliceStable(intervals, func(i, j int) bool {
		if intervals[i].Start != intervals[j].Start {
			return intervals[i].Start < intervals[j].Start
		}
		return intervals[i].End < intervals[j].End
	})
	merged := make([]frameInterval, 0, len(intervals))
	for _, interval := range intervals {
		if interval.End <= interval.Start {
			continue
		}
		if len(merged) == 0 || interval.Start > merged[len(merged)-1].End {
			merged = append(merged, interval)
			continue
		}
		if interval.End > merged[len(merged)-1].End {
			merged[len(merged)-1].End = interval.End
		}
	}
	return merged
}

func compressGainPoints(points []gainPoint) []gainPoint {
	if len(points) < 3 {
		return points
	}
	out := []gainPoint{points[0]}
	for index := 1; index < len(points)-1; index++ {
		previous := out[len(out)-1]
		current := points[index]
		next := points[index+1]
		if current.Step {
			out = append(out, current)
			continue
		}
		leftFrames := current.Frame - previous.Frame
		rightFrames := next.Frame - current.Frame
		if leftFrames > 0 && rightFrames > 0 {
			leftSlope := (current.Gain - previous.Gain) / float64(leftFrames)
			rightSlope := (next.Gain - current.Gain) / float64(rightFrames)
			if math.Abs(leftSlope-rightSlope) <= 1e-12 {
				continue
			}
		}
		out = append(out, current)
	}
	return append(out, points[len(points)-1])
}

func clipGainEnvelope(points []gainPoint, from, to int64, fps int) []map[string]any {
	if len(points) == 0 || to <= from {
		return nil
	}
	frames := []int64{from}
	for _, point := range points {
		if point.Frame > from && point.Frame < to {
			frames = append(frames, point.Frame)
		}
	}
	frames = append(frames, to)
	out := make([]map[string]any, 0, len(frames))
	for _, frame := range frames {
		point := map[string]any{
			"time": float64(frame-from) / float64(fps),
			"gain": gainAtFrame(points, frame),
		}
		if gainStepAtFrame(points, frame) {
			point["step"] = true
		}
		out = append(out, point)
	}
	if len(out) == 2 {
		first, _ := number(out[0]["gain"])
		second, _ := number(out[1]["gain"])
		if first == 1 && second == 1 {
			return nil
		}
	}
	return out
}

func gainStepAtFrame(points []gainPoint, frame int64) bool {
	for _, point := range points {
		if point.Frame == frame {
			return point.Step
		}
		if point.Frame > frame {
			break
		}
	}
	return false
}

func gainAtFrame(points []gainPoint, frame int64) float64 {
	if frame <= points[0].Frame {
		return points[0].Gain
	}
	for index := 1; index < len(points); index++ {
		if frame <= points[index].Frame {
			left, right := points[index-1], points[index]
			if right.Frame == left.Frame {
				return right.Gain
			}
			ratio := float64(frame-left.Frame) / float64(right.Frame-left.Frame)
			return left.Gain + (right.Gain-left.Gain)*ratio
		}
	}
	return points[len(points)-1].Gain
}

func hasStyledCaptions(items []timeline.Item) bool {
	for _, item := range items {
		if captionStyleHasOverrides(item.CaptionStyle) || isM3BStylePreset(item.StylePresetID) {
			return true
		}
	}
	return false
}

func isM3BStylePreset(presetID string) bool {
	switch presetID {
	case "subtitle-default", "subtitle-bold", "subtitle-outline":
		return true
	default:
		return false
	}
}

func captionStyleHasOverrides(style *timeline.CaptionStyle) bool {
	return style != nil && (style.FontFamily != nil || style.FontSize != nil ||
		style.TextColor != nil || style.BackgroundColor != nil ||
		style.OutlineColor != nil || style.OutlineWidth != nil ||
		style.Bold != nil || style.Italic != nil || style.Align != nil ||
		style.X != nil || style.Y != nil || style.MaxWidth != nil ||
		style.LineHeight != nil)
}

func buildStyledCaptionPayload(items []timeline.Item, fps, canvasHeight int) ([]byte, error) {
	if len(items) == 0 || len(items) > maxSubtitleEntries {
		return nil, fmt.Errorf(
			"render: styled captions must contain 1..%d entries",
			maxSubtitleEntries,
		)
	}
	payload := styledSubtitlePayload{
		Version: styledSubtitlePayloadVersion,
		Entries: make([]styledSubtitleEntry, 0, len(items)),
	}
	for _, item := range items {
		if len(item.Text) == 0 || len(item.Text) > maxStyledSubtitleTextSize {
			return nil, fmt.Errorf(
				"render: styled caption %q text must be within 1 byte..16 KiB",
				item.ItemID,
			)
		}
		style, legacy, err := resolveCaptionStyle(item, canvasHeight)
		if err != nil {
			return nil, err
		}
		payload.Entries = append(payload.Entries, styledSubtitleEntry{
			Start:  float64(item.From) / float64(fps),
			End:    float64(item.End()) / float64(fps),
			Text:   item.Text,
			Style:  style,
			Legacy: legacy,
		})
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("render: encode styled captions: %w", err)
	}
	if len(raw) > maxStyledSubtitlePayloadSize {
		return nil, fmt.Errorf("render: decoded styled subtitle content exceeds 1 MiB")
	}
	return raw, nil
}

func resolveCaptionStyle(item timeline.Item, canvasHeight int) (styledSubtitleStyle, bool, error) {
	fontSize := math.Max(20, math.Min(64, float64(canvasHeight)/14))
	style := styledSubtitleStyle{
		FontFamily: "default", FontSize: fontSize,
		TextColor: "#FFFFFF", BackgroundColor: "#00000096",
		OutlineColor: "#000000", OutlineWidth: 2,
		Align: "center", X: 0.5, Y: 0.8, MaxWidth: 0.8, LineHeight: 1.1,
	}
	legacy := !isM3BStylePreset(item.StylePresetID) &&
		!captionStyleHasOverrides(item.CaptionStyle)
	switch item.StylePresetID {
	case "", "subtitle-default":
	case "subtitle-bold":
		style.Bold = true
	case "subtitle-outline":
		style.BackgroundColor = ""
		style.OutlineWidth = 4
	default:
		// Historical schema-v1 preset IDs had no authoritative backend visual
		// definition. Preserve their legacy SRT appearance rather than
		// reinterpreting or rejecting old documents.
	}
	override := item.CaptionStyle
	if !captionStyleHasOverrides(override) {
		return style, legacy, nil
	}
	if override.FontFamily != nil {
		style.FontFamily = *override.FontFamily
	}
	if override.FontSize != nil {
		style.FontSize = *override.FontSize
	}
	if override.TextColor != nil {
		style.TextColor = *override.TextColor
	}
	if override.BackgroundColor != nil {
		style.BackgroundColor = *override.BackgroundColor
	}
	if override.OutlineColor != nil {
		style.OutlineColor = *override.OutlineColor
	}
	if override.OutlineWidth != nil {
		style.OutlineWidth = *override.OutlineWidth
	}
	if override.Bold != nil {
		style.Bold = *override.Bold
	}
	if override.Italic != nil {
		style.Italic = *override.Italic
	}
	if override.Align != nil {
		style.Align = *override.Align
	}
	if override.X != nil {
		style.X = *override.X
	}
	if override.Y != nil {
		style.Y = *override.Y
	}
	if override.MaxWidth != nil {
		style.MaxWidth = *override.MaxWidth
	}
	if override.LineHeight != nil {
		style.LineHeight = *override.LineHeight
	}
	return style, false, nil
}
