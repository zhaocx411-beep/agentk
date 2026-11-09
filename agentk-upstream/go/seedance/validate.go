package seedance

import (
	"fmt"
	"slices"
	"strings"
)

// Endpoint IDs use the 2.0 limits; the 2.5 model identifier selects its expanded limits.
func (r Request) Validate() error {
	if strings.TrimSpace(r.Prompt) == "" {
		return fmt.Errorf("seedance: prompt is required")
	}
	var first, last bool
	images, videos, audios := 0, 0, 0
	for _, media := range r.Media {
		switch media.Role {
		case "first_frame":
			first = true
		case "last_frame":
			last = true
		case "reference_image":
			images++
		case "reference_video":
			videos++
		case "reference_audio":
			audios++
		}
	}
	if last && !first {
		return fmt.Errorf("seedance: last frame requires first frame")
	}
	version25 := r.Model == "doubao-seedance-2.5"
	maxImages, maxVideos, maxAudios, maxDuration := 9, 3, 3, 15
	if version25 {
		maxImages, maxVideos, maxAudios, maxDuration = 30, 10, 10, 30
		if len(r.Media) > 50 {
			return fmt.Errorf("seedance: 2.5 accepts at most 50 media items")
		}
	} else if audios > 0 && !first && images == 0 && videos == 0 {
		return fmt.Errorf("seedance: reference audio requires an image or video input")
	}
	for _, limit := range []struct {
		kind       string
		count, max int
	}{
		{"referenceImages", images, maxImages}, {"referenceVideos", videos, maxVideos}, {"referenceAudios", audios, maxAudios},
	} {
		if limit.count > limit.max {
			return fmt.Errorf("seedance: accepts at most %d %s, got %d", limit.max, limit.kind, limit.count)
		}
	}
	if version25 && r.Duration == -1 {
		if videos == 0 {
			return fmt.Errorf("seedance: 2.5 duration -1 requires a reference video")
		}
	} else if r.Duration < 4 || r.Duration > maxDuration {
		return fmt.Errorf("seedance: duration must be between 4 and %d seconds, got %d", maxDuration, r.Duration)
	}
	if !slices.Contains([]string{"480p", "720p", "1080p", "4k"}, r.Resolution) {
		return fmt.Errorf("seedance: unsupported resolution %q", r.Resolution)
	}
	if version25 && r.Resolution == "4k" {
		return fmt.Errorf("seedance: 2.5 supports at most 1080p")
	}
	if !slices.Contains([]string{"21:9", "16:9", "4:3", "1:1", "3:4", "9:16", "adaptive"}, r.Ratio) {
		return fmt.Errorf("seedance: unsupported ratio %q", r.Ratio)
	}
	if version25 && (first || r.Duration == -1) && r.Ratio != "adaptive" {
		return fmt.Errorf("seedance: 2.5 first frame and video editing require adaptive ratio")
	}
	return nil
}
