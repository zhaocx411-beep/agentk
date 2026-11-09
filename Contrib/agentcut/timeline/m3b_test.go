package timeline

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func transitionDoc() Doc {
	d := baseDoc()
	d.Items = []Item{
		{ItemID: "out", TrackID: "v1", Kind: ItemVideo, MediaID: "m_video", From: 0, DurationInFrames: 100},
		{ItemID: "in", TrackID: "v1", Kind: ItemVideo, MediaID: "m_video", From: 100, DurationInFrames: 60, TrimStart: 100},
	}
	return d
}

func transition(id string, frames int64) Transition {
	return Transition{
		TransitionID: id,
		FromItemID:   "out",
		ToItemID:     "in",
		Kind:         TransitionCrossfade,
		Frames:       frames,
	}
}

func TestTransitionCRUDAndSummary(t *testing.T) {
	d := transitionDoc()
	added, addSummary := mustApply(t, d, Operation{
		Op: OpAddTransition, Transition: ptrTransition(transition("tr", 12)),
	})
	require.Len(t, added.Transitions, 1)
	assert.Equal(t, []string{"tr"}, addSummary.AddedTransitionIDs)
	assert.Empty(t, d.Transitions, "input document must remain unchanged")

	kind := TransitionDipToBlack
	updated, updateSummary := mustApply(t, added, Operation{
		Op: OpUpdateTransition, TransitionID: "tr",
		TransitionPatch: &TransitionPatch{Kind: &kind, Frames: i64(20)},
	})
	assert.Equal(t, TransitionDipToBlack, updated.Transitions[0].Kind)
	assert.Equal(t, int64(20), updated.Transitions[0].Frames)
	assert.Equal(t, []string{"tr"}, updateSummary.UpdatedTransitionIDs)

	removed, removeSummary := mustApply(t, updated, Operation{
		Op: OpRemoveTransition, TransitionID: "tr",
	})
	assert.Empty(t, removed.Transitions)
	assert.Equal(t, []string{"tr"}, removeSummary.RemovedTransitionIDs)

	mustFail(t, d, ErrBadRequest, Operation{Op: OpAddTransition})
	mustFail(t, added, ErrConflictingID, Operation{
		Op: OpAddTransition, Transition: ptrTransition(transition("tr", 5)),
	})
	mustFail(t, added, ErrBadRequest, Operation{
		Op: OpUpdateTransition, TransitionID: "tr", TransitionPatch: &TransitionPatch{},
	})
	mustFail(t, d, ErrNotFound, Operation{
		Op: OpRemoveTransition, TransitionID: "missing",
	})
}

func ptrTransition(value Transition) *Transition { return &value }

func TestTransitionValidation(t *testing.T) {
	t.Run("kind", func(t *testing.T) {
		d := transitionDoc()
		tr := transition("tr", 5)
		tr.Kind = "wipe"
		mustFail(t, d, ErrInvalidTransition, Operation{Op: OpAddTransition, Transition: &tr})
	})
	t.Run("missing item", func(t *testing.T) {
		d := transitionDoc()
		tr := transition("tr", 5)
		tr.ToItemID = "missing"
		mustFail(t, d, ErrNotFound, Operation{Op: OpAddTransition, Transition: &tr})
	})
	t.Run("non visual", func(t *testing.T) {
		d := transitionDoc()
		d.Items[1] = Item{
			ItemID: "in", TrackID: "a1", Kind: ItemAudio, MediaID: "m_audio",
			From: 100, DurationInFrames: 60,
		}
		tr := transition("tr", 5)
		mustFail(t, d, ErrInvalidTransition, Operation{Op: OpAddTransition, Transition: &tr})
	})
	t.Run("cross track", func(t *testing.T) {
		d := transitionDoc()
		d.Tracks = append(d.Tracks, Track{TrackID: "v2", Kind: TrackVideo})
		d.Items[1].TrackID = "v2"
		tr := transition("tr", 5)
		mustFail(t, d, ErrInvalidTransition, Operation{Op: OpAddTransition, Transition: &tr})
	})
	t.Run("not adjacent", func(t *testing.T) {
		d := transitionDoc()
		d.Items[1].From++
		tr := transition("tr", 5)
		mustFail(t, d, ErrInvalidTransition, Operation{Op: OpAddTransition, Transition: &tr})
	})
	t.Run("frames", func(t *testing.T) {
		d := transitionDoc()
		for _, frames := range []int64{0, 61} {
			tr := transition("tr", frames)
			mustFail(t, d, ErrInvalidTransition, Operation{Op: OpAddTransition, Transition: &tr})
		}
	})
	t.Run("video handle", func(t *testing.T) {
		d := transitionDoc()
		d.Items[0].DurationInFrames = 298
		d.Items[1].From = 298
		tr := transition("tr", 3)
		mustFail(t, d, ErrInsufficientHandle, Operation{Op: OpAddTransition, Transition: &tr})
	})
	t.Run("speed float safety", func(t *testing.T) {
		d := transitionDoc()
		d.Media[0].DurationInFrames = 130
		d.Items[0].Speed = 1.25
		d.Items[1].Kind = ItemImage
		d.Items[1].MediaID = "m_image"
		d.Items[1].TrimStart = 0
		tr := transition("tr", 4) // source end 125, handle 5, required 5
		mustApply(t, d, Operation{Op: OpAddTransition, Transition: &tr})
		d.Media[0].DurationInFrames = 129
		mustFail(t, d, ErrInsufficientHandle, Operation{Op: OpAddTransition, Transition: &tr})
	})
	t.Run("image handle unlimited", func(t *testing.T) {
		d := transitionDoc()
		d.Items[0].Kind = ItemImage
		d.Items[0].MediaID = "m_image"
		tr := transition("tr", 30)
		mustApply(t, d, Operation{Op: OpAddTransition, Transition: &tr})
	})
	t.Run("one per boundary", func(t *testing.T) {
		d := transitionDoc()
		d.Transitions = []Transition{transition("first", 5)}
		tr := transition("second", 5)
		mustFail(t, d, ErrInvalidTransition, Operation{Op: OpAddTransition, Transition: &tr})
	})
	t.Run("locked", func(t *testing.T) {
		d := transitionDoc()
		d.Tracks[0].Locked = true
		tr := transition("tr", 5)
		mustFail(t, d, ErrLockedTrack, Operation{Op: OpAddTransition, Transition: &tr})

		d.Tracks[0].Locked = false
		added, _ := mustApply(t, d, Operation{Op: OpAddTransition, Transition: &tr})
		added.Tracks[0].Locked = true
		mustFail(t, added, ErrLockedTrack, Operation{
			Op: OpUpdateTransition, TransitionID: "tr",
			TransitionPatch: &TransitionPatch{Frames: i64(6)},
		})
		mustFail(t, added, ErrLockedTrack, Operation{
			Op: OpRemoveTransition, TransitionID: "tr",
		})
	})
	t.Run("unsafe or oversized id", func(t *testing.T) {
		for _, id := range []string{"bad:id", strings.Repeat("x", MaxTransitionIDLength+1)} {
			d := transitionDoc()
			tr := transition(id, 5)
			mustFail(t, d, ErrInvalidTransition, Operation{
				Op: OpAddTransition, Transition: &tr,
			})
		}
	})
}

func TestTransitionCascades(t *testing.T) {
	withTransition := func(t *testing.T) Doc {
		t.Helper()
		d, _ := mustApply(t, transitionDoc(), Operation{
			Op: OpAddTransition, Transition: ptrTransition(transition("tr", 10)),
		})
		return d
	}

	t.Run("remove item", func(t *testing.T) {
		next, summary := mustApply(t, withTransition(t), Operation{
			Op: OpRemoveItems, ItemIDs: []string{"in"},
		})
		assert.Empty(t, next.Transitions)
		assert.Equal(t, []string{"tr"}, summary.RemovedTransitionIDs)
	})
	t.Run("remove track", func(t *testing.T) {
		next, summary := mustApply(t, withTransition(t), Operation{
			Op: OpRemoveTrack, TrackID: "v1",
		})
		assert.Empty(t, next.Transitions)
		assert.Equal(t, []string{"tr"}, summary.RemovedTransitionIDs)
	})
	t.Run("split", func(t *testing.T) {
		next, summary := mustApply(t, withTransition(t), Operation{
			Op: OpSplit, ItemID: "out", Frame: i64(50),
		})
		assert.Empty(t, next.Transitions)
		assert.Equal(t, []string{"tr"}, summary.RemovedTransitionIDs)
	})
	t.Run("trim", func(t *testing.T) {
		next, summary := mustApply(t, withTransition(t), Operation{
			Op: OpTrimEnd, ItemID: "out", Frame: i64(90),
		})
		assert.Empty(t, next.Transitions)
		assert.Equal(t, []string{"tr"}, summary.RemovedTransitionIDs)
	})
	t.Run("move", func(t *testing.T) {
		next, summary := mustApply(t, withTransition(t), Operation{
			Op: OpMoveItem, ItemID: "in", From: i64(170),
		})
		assert.Empty(t, next.Transitions)
		assert.Equal(t, []string{"tr"}, summary.RemovedTransitionIDs)
	})
	t.Run("ripple remove", func(t *testing.T) {
		next, summary := mustApply(t, withTransition(t), Operation{
			Op: OpRippleDelete, FromFrame: i64(100), ToFrame: i64(160), TrackIDs: []string{"v1"},
		})
		assert.Empty(t, next.Transitions)
		assert.Equal(t, []string{"tr"}, summary.RemovedTransitionIDs)
	})
	t.Run("batch move preserves final valid boundary", func(t *testing.T) {
		next, summary, err := ApplyOps(withTransition(t), []Operation{
			{Op: OpMoveItem, ItemID: "out", From: i64(10)},
			{Op: OpMoveItem, ItemID: "in", From: i64(110)},
		})
		require.NoError(t, err)
		require.Len(t, next.Transitions, 1)
		assert.Empty(t, summary.RemovedTransitionIDs)
	})
}

func TestDuckingConfigAndTrackRemovalCleanup(t *testing.T) {
	d := baseDoc()
	d.Tracks = append(d.Tracks, Track{TrackID: "a2", Kind: TrackAudio})
	config := &DuckingConfig{
		SidechainTrackIDs: []string{"v1", "a2"},
		Gain:              0.25,
		AttackFrames:      30,
		ReleaseFrames:     45,
	}
	next, summary := mustApply(t, d, Operation{
		Op: OpUpdateTrack, TrackID: "a1", TrackPatch: &TrackPatch{Ducking: config},
	})
	require.NotNil(t, next.Tracks[1].Ducking)
	assert.Equal(t, []string{"v1", "a2"}, next.Tracks[1].Ducking.SidechainTrackIDs)
	assert.Equal(t, []string{"a1"}, summary.UpdatedTrackIDs)

	config.SidechainTrackIDs[0] = "mutated"
	assert.Equal(t, "v1", next.Tracks[1].Ducking.SidechainTrackIDs[0], "patch slices must be cloned")

	removed, removeSummary := mustApply(t, next, Operation{
		Op: OpRemoveTrack, TrackID: "v1",
	})
	var audio Track
	for _, track := range removed.Tracks {
		if track.TrackID == "a1" {
			audio = track
		}
	}
	require.NotNil(t, audio.Ducking)
	assert.Equal(t, []string{"a2"}, audio.Ducking.SidechainTrackIDs)
	assert.Contains(t, removeSummary.UpdatedTrackIDs, "a1")

	cleared, _ := mustApply(t, removed, Operation{
		Op: OpRemoveTrack, TrackID: "a2",
	})
	for _, track := range cleared.Tracks {
		if track.TrackID == "a1" {
			assert.Nil(t, track.Ducking)
		}
	}

	explicitlyCleared, _ := mustApply(t, next, Operation{
		Op: OpUpdateTrack, TrackID: "a1",
		TrackPatch: &TrackPatch{Ducking: &DuckingConfig{SidechainTrackIDs: []string{}}},
	})
	assert.Nil(t, explicitlyCleared.Tracks[1].Ducking)
}

func TestDuckingValidation(t *testing.T) {
	d := baseDoc()
	valid := func() *DuckingConfig {
		return &DuckingConfig{SidechainTrackIDs: []string{"v1"}, Gain: 0.5, AttackFrames: 10, ReleaseFrames: 10}
	}
	mustApply(t, d, Operation{
		Op: OpUpdateTrack, TrackID: "a1", TrackPatch: &TrackPatch{Ducking: valid()},
	})
	t.Run("zero gain", func(t *testing.T) {
		d := baseDoc()
		d.Tracks = append(d.Tracks, Track{TrackID: "a2", Kind: TrackAudio})
		mustFail(t, d, ErrInvalidDucking, Operation{
			Op: OpUpdateTrack, TrackID: "a1",
			TrackPatch: &TrackPatch{Ducking: &DuckingConfig{
				SidechainTrackIDs: []string{"a2"}, Gain: 0,
			}},
		})
	})
	mustFail(t, d, ErrInvalidDucking, Operation{
		Op: OpUpdateTrack, TrackID: "v1", TrackPatch: &TrackPatch{Ducking: valid()},
	})
	for _, config := range []*DuckingConfig{
		{SidechainTrackIDs: []string{"missing"}, Gain: 0.5},
		{SidechainTrackIDs: []string{"a1"}, Gain: 0.5},
		{SidechainTrackIDs: []string{"v1", "v1"}, Gain: 0.5},
		{SidechainTrackIDs: []string{"c1"}, Gain: 0.5},
		{SidechainTrackIDs: []string{"v1"}, Gain: -0.1},
		{SidechainTrackIDs: []string{"v1"}, Gain: math.NaN()},
		{SidechainTrackIDs: []string{"v1"}, Gain: 0.5, AttackFrames: 151},
		{SidechainTrackIDs: []string{"v1"}, Gain: 0.5, ReleaseFrames: -1},
	} {
		mustFail(t, d, ErrInvalidDucking, Operation{
			Op: OpUpdateTrack, TrackID: "a1", TrackPatch: &TrackPatch{Ducking: config},
		})
	}

	locked := d.Clone()
	locked.Tracks[1].Locked = true
	mustFail(t, locked, ErrLockedTrack, Operation{
		Op: OpUpdateTrack, TrackID: "a1", TrackPatch: &TrackPatch{Ducking: valid()},
	})
}

func TestCropRotationAndCaptionStyle(t *testing.T) {
	d := docWithClip()
	transform := &ItemTransform{
		X: 0, Y: 0, Width: 1, Height: 1,
		Crop:            &NormalizedRect{X: 0.1, Y: 0.2, Width: 0.6, Height: 0.7},
		RotationDegrees: 45,
	}
	transformed, _ := mustApply(t, d, Operation{
		Op: OpUpdateItem, ItemID: "it1", Patch: &ItemPatch{Transform: transform},
	})
	require.NotNil(t, transformed.Items[0].Transform.Crop)
	assert.Equal(t, 45.0, transformed.Items[0].Transform.RotationDegrees)
	transform.Crop.X = 0.9
	assert.Equal(t, 0.1, transformed.Items[0].Transform.Crop.X)

	for _, invalid := range []*ItemTransform{
		{Width: 1, Height: 1, Crop: &NormalizedRect{X: 0.8, Width: 0.3, Height: 1}},
		{Width: 1, Height: 1, Crop: &NormalizedRect{Width: 0, Height: 1}},
		{Width: 1, Height: 1, RotationDegrees: 181},
		{Width: 1, Height: 1, RotationDegrees: math.NaN()},
	} {
		mustFail(t, d, func() ErrorCode {
			if math.IsNaN(invalid.RotationDegrees) {
				return ErrBadRequest
			}
			return ErrOutOfRange
		}(), Operation{Op: OpUpdateItem, ItemID: "it1", Patch: &ItemPatch{Transform: invalid}})
	}

	textDoc := baseDoc()
	textDoc.Items = []Item{{
		ItemID: "caption", TrackID: "c1", Kind: ItemText, From: 0, DurationInFrames: 30, Text: "hello",
		CaptionStyle: &CaptionStyle{
			FontFamily: str("sans"), FontSize: f64(36), Bold: bptr(false),
			TextColor: str("#FFFFFF"), BackgroundColor: str("#00000080"),
			OutlineColor: str("#000000"), OutlineWidth: f64(2),
			Align: str("center"), X: f64(0.5), Y: f64(0.8),
			MaxWidth: f64(0.8), LineHeight: f64(1.2),
		},
	}}
	require.Nil(t, ValidateDoc(textDoc))
	patched, _ := mustApply(t, textDoc, Operation{
		Op: OpUpdateItem, ItemID: "caption",
		Patch: &ItemPatch{CaptionStyle: &CaptionStyle{Bold: bptr(true), TextColor: str("#FF0000")}},
	})
	style := patched.Items[0].CaptionStyle
	require.NotNil(t, style)
	assert.Equal(t, "sans", *style.FontFamily, "partial override must retain existing fields")
	assert.True(t, *style.Bold)
	assert.Equal(t, "#FF0000", *style.TextColor)
	clearedStyle, _ := mustApply(t, patched, Operation{
		Op: OpUpdateItem, ItemID: "caption",
		Patch: &ItemPatch{CaptionStyle: &CaptionStyle{}},
	})
	assert.Nil(t, clearedStyle.Items[0].CaptionStyle)

	invalidStyles := []*CaptionStyle{
		{FontFamily: str("Comic Sans")},
		{FontSize: f64(7)},
		{TextColor: str("red")},
		{BackgroundColor: str("#000000")},
		{OutlineColor: str("#00000080")},
		{OutlineWidth: f64(9)},
		{Align: str("justify")},
		{X: f64(-0.1)},
		{Y: f64(1.1)},
		{MaxWidth: f64(0.09)},
		{LineHeight: f64(3.1)},
	}
	for _, invalid := range invalidStyles {
		mustFail(t, textDoc, ErrInvalidCaptionStyle, Operation{
			Op: OpUpdateItem, ItemID: "caption", Patch: &ItemPatch{CaptionStyle: invalid},
		})
	}
	mustFail(t, d, ErrInvalidCaptionStyle, Operation{
		Op: OpUpdateItem, ItemID: "it1",
		Patch: &ItemPatch{CaptionStyle: &CaptionStyle{FontFamily: str("sans")}},
	})
}

func TestSetCaptionsStyleAndM3BDeepClone(t *testing.T) {
	d := baseDoc()
	style := &CaptionStyle{FontFamily: str("mono"), Bold: bptr(false), X: f64(0.25)}
	next, _ := mustApply(t, d, Operation{
		Op: OpSetCaptions, TrackID: "c1",
		Captions: []Caption{{From: 0, DurationInFrames: 20, Text: "styled", CaptionStyle: style}},
	})
	require.NotNil(t, next.Items[0].CaptionStyle)
	assert.Equal(t, "mono", *next.Items[0].CaptionStyle.FontFamily)
	*style.FontFamily = "serif"
	assert.Equal(t, "mono", *next.Items[0].CaptionStyle.FontFamily)

	next.Tracks[1].Ducking = &DuckingConfig{
		SidechainTrackIDs: []string{"v1"}, Gain: 0.5,
	}
	next.Items[0].Transform = nil
	clone := next.Clone()
	clone.Tracks[1].Ducking.SidechainTrackIDs[0] = "changed"
	*clone.Items[0].CaptionStyle.FontFamily = "default"
	assert.Equal(t, "v1", next.Tracks[1].Ducking.SidechainTrackIDs[0])
	assert.Equal(t, "mono", *next.Items[0].CaptionStyle.FontFamily)
}

func TestLegacyM3BJSONDefaultsRemainValid(t *testing.T) {
	const raw = `{
		"schemaVersion":1,
		"metadata":{"width":320,"height":180,"fps":30},
		"media":[{"mediaId":"m","uri":"u","kind":"video","durationInFrames":30}],
		"tracks":[{"trackId":"v","kind":"video"}],
		"items":[{"itemId":"i","trackId":"v","kind":"video","mediaId":"m","from":0,"durationInFrames":30}]
	}`
	var doc Doc
	require.NoError(t, json.Unmarshal([]byte(raw), &doc))
	require.Nil(t, ValidateDoc(doc))
	assert.Equal(t, SchemaVersion, doc.SchemaVersion)
	assert.Nil(t, doc.Tracks[0].Ducking)
	assert.Nil(t, doc.Items[0].CaptionStyle)
	assert.Nil(t, doc.Items[0].Transform)
	assert.Empty(t, doc.Transitions)
}
