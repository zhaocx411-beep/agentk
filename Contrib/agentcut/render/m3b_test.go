package render

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"agentcut/timeline"
)

func TestDuckEnvelopeUsesUnionAndLookahead(t *testing.T) {
	doc := timeline.NewDoc("duck", 1920, 1080, 10)
	doc.Media = []timeline.Media{
		{MediaID: "voice", Kind: timeline.MediaAudio, DurationInFrames: 100},
		{MediaID: "silent-video", Kind: timeline.MediaVideo, DurationInFrames: 100},
	}
	doc.Tracks = []timeline.Track{
		{TrackID: "bed", Kind: timeline.TrackAudio, Ducking: &timeline.DuckingConfig{
			SidechainTrackIDs: []string{"voice-track", "video-track"},
			Gain:              0.25, AttackFrames: 2, ReleaseFrames: 2,
		}},
		{TrackID: "voice-track", Kind: timeline.TrackAudio},
		{TrackID: "video-track", Kind: timeline.TrackVideo},
	}
	doc.Items = []timeline.Item{
		{ItemID: "voice-1", TrackID: "voice-track", Kind: timeline.ItemAudio, MediaID: "voice", From: 10, DurationInFrames: 10},
		{ItemID: "voice-2", TrackID: "voice-track", Kind: timeline.ItemAudio, MediaID: "voice", From: 15, DurationInFrames: 10},
		{ItemID: "silent", TrackID: "video-track", Kind: timeline.ItemVideo, MediaID: "silent-video", From: 30, DurationInFrames: 10},
	}

	points := duckEnvelope(doc, doc.Tracks[0], 50)
	require.NotEmpty(t, points)
	assert.InDelta(t, 1, gainAtFrame(points, 8), 1e-9)
	assert.InDelta(t, 0.625, gainAtFrame(points, 9), 1e-9)
	assert.InDelta(t, 0.25, gainAtFrame(points, 10), 1e-9)
	assert.InDelta(t, 0.25, gainAtFrame(points, 25), 1e-9)
	assert.InDelta(t, 0.625, gainAtFrame(points, 26), 1e-9)
	assert.InDelta(t, 1, gainAtFrame(points, 27), 1e-9)
	assert.InDelta(t, 1, gainAtFrame(points, 32), 1e-9,
		"video without Probe.HasAudio must not trigger ducking")
}

func TestDuckEnvelopeZeroAttackReleaseStepsAtFrameBoundary(t *testing.T) {
	doc := timeline.NewDoc("duck-step", 320, 180, 10)
	doc.Media = []timeline.Media{{MediaID: "voice", Kind: timeline.MediaAudio, DurationInFrames: 50}}
	doc.Tracks = []timeline.Track{
		{TrackID: "bed", Kind: timeline.TrackAudio, Ducking: &timeline.DuckingConfig{
			SidechainTrackIDs: []string{"voice"}, Gain: 0.2,
		}},
		{TrackID: "voice", Kind: timeline.TrackAudio},
	}
	doc.Items = []timeline.Item{{
		ItemID: "voice", TrackID: "voice", Kind: timeline.ItemAudio,
		MediaID: "voice", From: 10, DurationInFrames: 10,
	}}
	points := duckEnvelope(doc, doc.Tracks[0], 30)
	assert.InDelta(t, 1, gainAtFrame(points, 9), 1e-9)
	assert.InDelta(t, 0.2, gainAtFrame(points, 10), 1e-9)
	assert.InDelta(t, 0.2, gainAtFrame(points, 19), 1e-9)
	assert.InDelta(t, 1, gainAtFrame(points, 20), 1e-9)
	envelope := clipGainEnvelope(points, 0, 30, 10)
	var stepTimes []float64
	for _, point := range envelope {
		if step, _ := point["step"].(bool); step {
			stepTimes = append(stepTimes, point["time"].(float64))
		}
	}
	assert.Equal(t, []float64{1, 2}, stepTimes)
}

func TestClipGainEnvelopeUsesItemLocalSeconds(t *testing.T) {
	points := []gainPoint{
		{Frame: 0, Gain: 1},
		{Frame: 10, Gain: 0.2},
		{Frame: 20, Gain: 0.2},
		{Frame: 30, Gain: 1},
	}
	clipped := clipGainEnvelope(points, 5, 25, 10)
	require.Len(t, clipped, 4)
	assert.Equal(t, 0.0, clipped[0]["time"])
	assert.Equal(t, 2.0, clipped[len(clipped)-1]["time"])
	assert.InDelta(t, 0.6, clipped[0]["gain"], 1e-9)
	assert.InDelta(t, 0.6, clipped[len(clipped)-1]["gain"], 1e-9)
}

func TestStyledCaptionPayloadResolvesPresetAndOverrides(t *testing.T) {
	bold := false
	color := "#FF0000"
	align := "right"
	item := timeline.Item{
		ItemID: "caption", Kind: timeline.ItemText,
		From: 10, DurationInFrames: 20, Text: "你好\n世界",
		StylePresetID: "subtitle-bold",
		CaptionStyle: &timeline.CaptionStyle{
			Bold: &bold, TextColor: &color, Align: &align,
		},
	}
	raw, err := buildStyledCaptionPayload([]timeline.Item{item}, 10, 720)
	require.NoError(t, err)
	payload, err := parseStyledSubtitlePayload(raw)
	require.NoError(t, err)
	require.Len(t, payload.Entries, 1)
	assert.False(t, payload.Entries[0].Style.Bold,
		"an explicit false override must replace the bold preset")
	assert.Equal(t, "#FF0000", payload.Entries[0].Style.TextColor)
	assert.Equal(t, "right", payload.Entries[0].Style.Align)
	assert.Equal(t, 1.0, payload.Entries[0].Start)
	assert.Equal(t, 3.0, payload.Entries[0].End)

	encoded := "styled-caption-v1:" + base64.StdEncoding.EncodeToString(raw)
	assert.True(t, strings.HasPrefix(encoded, "styled-caption-v1:"))

	legacyStyle := item
	legacyStyle.StylePresetID = "style-1"
	legacyStyle.CaptionStyle = &timeline.CaptionStyle{}
	assert.False(t, hasStyledCaptions([]timeline.Item{legacyStyle}))
	_, legacy, err := resolveCaptionStyle(legacyStyle, 720)
	require.NoError(t, err)
	assert.True(t, legacy)

	legacyDoc := timeline.NewDoc("legacy", 320, 180, 24)
	legacyDoc.Items = []timeline.Item{legacyStyle}
	require.NoError(t, rejectUnsupportedAgentKFeatures(legacyDoc))

	tooMany := make([]timeline.Item, maxSubtitleEntries+1)
	for index := range tooMany {
		tooMany[index] = item
	}
	_, err = buildStyledCaptionPayload(tooMany, 10, 720)
	require.Error(t, err)
	tooLong := item
	tooLong.Text = strings.Repeat("x", maxStyledSubtitleTextSize+1)
	_, err = buildStyledCaptionPayload([]timeline.Item{tooLong}, 10, 720)
	require.Error(t, err)
}

func TestFirstVideoTrackCarriesCropRotationIntoSourcePrep(t *testing.T) {
	doc := timeline.NewDoc("transform", 320, 180, 24)
	doc.Media = []timeline.Media{{
		MediaID: "v", Kind: timeline.MediaVideo, URI: "/v.mp4", DurationInFrames: 96,
	}}
	doc.Tracks = []timeline.Track{{TrackID: "v", Kind: timeline.TrackVideo}}
	doc.Items = []timeline.Item{{
		ItemID: "v", TrackID: "v", Kind: timeline.ItemVideo, MediaID: "v",
		From: 0, DurationInFrames: 24,
		Transform: &timeline.ItemTransform{
			X: 0, Y: 0, Width: 1, Height: 1, Fit: timeline.ItemFitCover,
			Crop:            &timeline.NormalizedRect{X: 0.1, Y: 0.2, Width: 0.7, Height: 0.6},
			RotationDegrees: 90,
		},
	}}
	plan, err := CompileManaged(doc)
	require.NoError(t, err)
	itemCall := callByID(t, plan, "item:v")
	steps := itemCall.Input["pipeline"].([]map[string]any)
	params := steps[0]["params"].(map[string]any)
	assert.Equal(t, 0.1, params["crop_x"])
	assert.Equal(t, 0.2, params["crop_y"])
	assert.Equal(t, 90.0, params["rotation_degrees"])
	assert.Contains(t, callIDs(plan), "canvas:background",
		"the first visual track must not bypass crop/rotation composition")
}

func TestCompileTransitionOwnsAdjacentFadesOnFirstAndOverlayTracks(t *testing.T) {
	doc := timeline.NewDoc("transition-fades", 320, 180, 30)
	doc.Media = []timeline.Media{{
		MediaID: "v", Kind: timeline.MediaVideo, URI: "/v.mp4", DurationInFrames: 240,
		Probe: &timeline.MediaProbe{HasAudio: true},
	}}
	doc.Tracks = []timeline.Track{
		{TrackID: "first", Kind: timeline.TrackVideo},
		{TrackID: "overlay", Kind: timeline.TrackVideo},
	}
	for _, trackID := range []string{"first", "overlay"} {
		doc.Items = append(doc.Items,
			timeline.Item{
				ItemID: trackID + "-from", TrackID: trackID, Kind: timeline.ItemVideo,
				MediaID: "v", From: 0, DurationInFrames: 30,
				FadeInFrames: 4, FadeOutFrames: 20,
			},
			timeline.Item{
				ItemID: trackID + "-to", TrackID: trackID, Kind: timeline.ItemVideo,
				MediaID: "v", From: 30, DurationInFrames: 30, TrimStart: 100,
				FadeInFrames: 20, FadeOutFrames: 5,
			},
		)
		doc.Transitions = append(doc.Transitions, timeline.Transition{
			TransitionID: trackID + "-transition",
			FromItemID:   trackID + "-from",
			ToItemID:     trackID + "-to",
			Kind:         timeline.TransitionCrossfade,
			Frames:       10,
		})
	}
	require.Nil(t, timeline.ValidateDoc(doc))

	plan, err := CompileManaged(doc)
	require.NoError(t, err)
	for _, trackID := range []string{"first", "overlay"} {
		fromOverlay := callByID(t, plan, "overlay:"+trackID+"-from").
			Input["params"].(map[string]any)
		toOverlay := callByID(t, plan, "overlay:"+trackID+"-to").
			Input["params"].(map[string]any)
		assert.Equal(t, 4.0/30.0, fromOverlay["fade_in"])
		assert.Equal(t, 0.0, fromOverlay["fade_out"],
			"outgoing transition must replace the full fadeOut")
		assert.Equal(t, 0.0, toOverlay["fade_in"],
			"incoming transition must replace the full fadeIn")
		assert.Equal(t, 5.0/30.0, toOverlay["fade_out"])

		fromAudio := callByID(t, plan, "embedded-audio:"+trackID+"-from").
			Input["params"].(map[string]any)
		toAudio := callByID(t, plan, "embedded-audio:"+trackID+"-to").
			Input["params"].(map[string]any)
		assert.Equal(t, 4.0/30.0, fromAudio["fade_in"])
		assert.Equal(t, 0.0, fromAudio["fade_out"])
		assert.Equal(t, 0.0, toAudio["fade_in"])
		assert.Equal(t, 5.0/30.0, toAudio["fade_out"])
	}
}

func TestCompileRejectsOversizedDuckEnvelopeBeforeActivity(t *testing.T) {
	doc := timeline.NewDoc("many-ducks", 320, 180, 30)
	doc.Media = []timeline.Media{
		{MediaID: "v", Kind: timeline.MediaVideo, URI: "/v", DurationInFrames: 2000},
		{MediaID: "bed", Kind: timeline.MediaAudio, URI: "/bed", DurationInFrames: 2000},
		{MediaID: "voice", Kind: timeline.MediaAudio, URI: "/voice", DurationInFrames: 2000},
	}
	doc.Tracks = []timeline.Track{
		{TrackID: "v", Kind: timeline.TrackVideo},
		{TrackID: "bed", Kind: timeline.TrackAudio, Ducking: &timeline.DuckingConfig{
			SidechainTrackIDs: []string{"voice"}, Gain: 0.2,
		}},
		{TrackID: "voice", Kind: timeline.TrackAudio},
	}
	doc.Items = []timeline.Item{
		{ItemID: "v", TrackID: "v", Kind: timeline.ItemVideo, MediaID: "v", DurationInFrames: 1200},
		{ItemID: "bed", TrackID: "bed", Kind: timeline.ItemAudio, MediaID: "bed", DurationInFrames: 1200},
	}
	for index := 0; index < 300; index++ {
		doc.Items = append(doc.Items, timeline.Item{
			ItemID:  "voice-" + fmt.Sprint(index),
			TrackID: "voice", Kind: timeline.ItemAudio, MediaID: "voice",
			From: int64(index * 4), DurationInFrames: 1,
		})
	}
	_, err := CompileManaged(doc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duck envelope")
	assert.Contains(t, err.Error(), "keyframes")
}
