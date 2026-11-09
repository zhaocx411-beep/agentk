package timeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanupEmptyAutoTracksPreservesBaseUserLockedAndOccupiedTracks(t *testing.T) {
	doc := NewDoc("project", 1920, 1080, 30)
	doc.Tracks = []Track{
		{TrackID: "v1", Kind: TrackVideo, CreatedBy: TrackCreatedAuto},
		{TrackID: "v2", Kind: TrackVideo, CreatedBy: TrackCreatedAuto},
		{TrackID: "v3", Kind: TrackVideo, CreatedBy: TrackCreatedAuto, Locked: true},
		{TrackID: "a1", Kind: TrackAudio, CreatedBy: TrackCreatedAuto},
		{TrackID: "a2", Kind: TrackAudio, CreatedBy: TrackCreatedAuto},
		{TrackID: "manual", Kind: TrackAudio, CreatedBy: TrackCreatedUser},
		{TrackID: "legacy", Kind: TrackAudio},
	}
	doc.Items = []Item{{
		ItemID: "audio", TrackID: "a1", Kind: ItemAudio, MediaID: "audio",
		From: 0, DurationInFrames: 30,
	}}

	next, removed := CleanupEmptyAutoTracks(doc)

	assert.ElementsMatch(t, []string{"v2", "a2"}, removed)
	require.Len(t, next.Tracks, 5)
	assert.Equal(t, []string{"v1", "v3", "a1", "manual", "legacy"}, []string{
		next.Tracks[0].TrackID,
		next.Tracks[1].TrackID,
		next.Tracks[2].TrackID,
		next.Tracks[3].TrackID,
		next.Tracks[4].TrackID,
	})
	assert.Len(t, doc.Tracks, 7, "cleanup must not mutate its input")
}

func TestCleanupEmptyAutoTracksRemovesLastAudioTrackAfterItsItemIsDeleted(t *testing.T) {
	doc := NewDoc("project", 1920, 1080, 30)
	doc.Tracks = []Track{
		{TrackID: "v1", Kind: TrackVideo, CreatedBy: TrackCreatedAuto},
		{TrackID: "a1", Kind: TrackAudio, CreatedBy: TrackCreatedAuto},
	}

	next, removed := CleanupEmptyAutoTracks(doc)

	assert.Equal(t, []string{"a1"}, removed)
	require.Len(t, next.Tracks, 1)
	assert.Equal(t, "v1", next.Tracks[0].TrackID)
}
