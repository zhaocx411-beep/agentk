package render

import (
	"fmt"
	"math"
	"strings"

	"agentcut/timeline"
)

// UnsupportedRenderFeatureError prevents an executor from silently producing
// output with semantics different from the authoritative Timeline.
type UnsupportedRenderFeatureError struct {
	Feature string
}

func (e *UnsupportedRenderFeatureError) Error() string {
	return fmt.Sprintf("render executor does not support required feature: %s", e.Feature)
}

func rejectUnsupportedAgentKFeatures(doc timeline.Doc) error {
	if len(doc.Transitions) > 0 {
		return &UnsupportedRenderFeatureError{Feature: "transitions require local executor"}
	}
	tracks := make(map[string]timeline.Track, len(doc.Tracks))
	for _, track := range doc.Tracks {
		tracks[track.TrackID] = track
		if track.Hidden {
			return &UnsupportedRenderFeatureError{Feature: "video track visibility requires local executor"}
		}
		if track.Ducking != nil {
			return &UnsupportedRenderFeatureError{Feature: "audio ducking requires local executor"}
		}
	}
	if background := strings.TrimSpace(doc.Metadata.BackgroundColor); background != "" &&
		!isBlackColor(background) {
		return &UnsupportedRenderFeatureError{Feature: "metadata.backgroundColor requires local executor"}
	}
	for _, item := range doc.Items {
		track := tracks[item.TrackID]
		switch {
		case item.FadeInFrames != 0 || item.FadeOutFrames != 0:
			return &UnsupportedRenderFeatureError{Feature: "item fades require local executor"}
		case item.Transform != nil:
			return &UnsupportedRenderFeatureError{Feature: "item transform requires local executor"}
		case item.Kind == timeline.ItemText &&
			(captionStyleHasOverrides(item.CaptionStyle) ||
				isM3BStylePreset(strings.TrimSpace(item.StylePresetID))):
			return &UnsupportedRenderFeatureError{Feature: "styled captions require local executor"}
		case item.Kind == timeline.ItemAudio && item.EffectiveSpeed() != 1:
			return &UnsupportedRenderFeatureError{Feature: "audio speed requires local executor"}
		case item.Kind == timeline.ItemVideo &&
			effectiveVolume(track.Volume)*effectiveVolume(item.Volume) != 1:
			return &UnsupportedRenderFeatureError{Feature: "embedded video audio gain requires local executor"}
		}
	}
	return nil
}

func isBlackColor(value string) bool {
	return strings.EqualFold(value, "black") || strings.EqualFold(value, "#000000")
}

func renderBackground(doc timeline.Doc) (string, error) {
	background := strings.TrimSpace(doc.Metadata.BackgroundColor)
	if background == "" {
		return "black", nil
	}
	if err := timeline.ValidateBackgroundColor(background); err != nil {
		return "", fmt.Errorf("render: %w", err)
	}
	return background, nil
}

type pixelRegion struct {
	X       int
	Y       int
	Width   int
	Height  int
	Opacity float64
	Fit     string
}

func itemPixelRegion(item timeline.Item, canvasWidth, canvasHeight int) pixelRegion {
	if item.Transform == nil {
		return pixelRegion{
			Width: canvasWidth, Height: canvasHeight,
			Opacity: 1, Fit: timeline.ItemFitContain,
		}
	}
	transform := item.Transform
	x := int(math.Floor(transform.X * float64(canvasWidth)))
	y := int(math.Floor(transform.Y * float64(canvasHeight)))
	right := int(math.Ceil((transform.X + transform.Width) * float64(canvasWidth)))
	bottom := int(math.Ceil((transform.Y + transform.Height) * float64(canvasHeight)))
	right = min(canvasWidth, max(x+1, right))
	bottom = min(canvasHeight, max(y+1, bottom))
	return pixelRegion{
		X: x, Y: y, Width: right - x, Height: bottom - y,
		Opacity: item.EffectiveOpacity(), Fit: item.EffectiveFit(),
	}
}

func encodedRegionDimensions(region pixelRegion) (int, int) {
	width, height := region.Width, region.Height
	if width%2 != 0 {
		width++
	}
	if height%2 != 0 {
		height++
	}
	return max(2, width), max(2, height)
}

func trackHasTransform(doc timeline.Doc, trackID string) bool {
	for _, item := range doc.Items {
		if item.TrackID == trackID && item.Transform != nil {
			return true
		}
	}
	return false
}
