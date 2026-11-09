package entity

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"agentcut/timeline"
)

func TestNewTimelineStateMigratesLegacyDocOnce(t *testing.T) {
	doc := timeline.NewDoc("旧项目", 1920, 1080, 30)
	doc.Media = []timeline.Media{{
		MediaID: "media_1", URI: "blob://media_1", Kind: timeline.MediaVideo,
		DurationInFrames: 300,
	}}
	doc.Tracks = []timeline.Track{
		{TrackID: "captions", Kind: timeline.TrackCaption, Name: "字幕"},
		{TrackID: "video_existing", Kind: timeline.TrackVideo, Name: "主视频"},
	}
	doc.Items = []timeline.Item{
		{
			ItemID: "video_item", TrackID: "video_existing", Kind: timeline.ItemVideo,
			MediaID: "media_1", From: 15, DurationInFrames: 90,
		},
		{
			ItemID: "caption_item", TrackID: "captions", Kind: timeline.ItemText,
			From: 20, DurationInFrames: 30, Text: "保留字幕",
		},
	}

	first := NewTimelineState(TimelineInit{
		ProjectID: "project_legacy", Doc: doc, Revision: 7,
	}, 1_000)
	second := NewTimelineState(TimelineInit{
		ProjectID: "project_legacy", Doc: first.Doc, Revision: first.Revision,
		ActiveTimelineID: first.ActiveTimelineID, Timelines: first.Timelines,
	}, 2_000)

	require.Len(t, first.Timelines, 1)
	assert.Equal(t, DefaultSequenceName, first.Timelines[0].Name)
	assert.Equal(t, uint64(7), first.Timelines[0].Revision)
	assert.Equal(t, doc, first.Timelines[0].Doc)
	assert.Equal(t, first.ActiveTimelineID, second.ActiveTimelineID)
	assert.Equal(t, first.Timelines, second.Timelines)
}

func TestNewTimelineStateAddsRealBaseVideoTrackToEmptyLegacyDoc(t *testing.T) {
	doc := timeline.NewDoc("空项目", 1920, 1080, 30)

	state := NewTimelineState(TimelineInit{
		ProjectID: "project_empty", Doc: doc,
	}, 1_000)

	require.Len(t, state.Doc.Tracks, 1)
	assert.Equal(t, timeline.TrackVideo, state.Doc.Tracks[0].Kind)
	assert.Equal(t, timeline.TrackCreatedAuto, state.Doc.Tracks[0].CreatedBy)
	assert.Equal(t, state.Doc.Tracks, state.Timelines[0].Doc.Tracks)
}

func TestNewTimelineStateRestoresSelectedSequenceIndependently(t *testing.T) {
	firstDoc := timeline.NewDoc("项目", 1920, 1080, 30)
	firstDoc.Tracks = []timeline.Track{{TrackID: "v1", Kind: timeline.TrackVideo}}
	firstDoc.Items = []timeline.Item{{
		ItemID: "first", TrackID: "v1", Kind: timeline.ItemImage,
		MediaID: "image", From: 0, DurationInFrames: 30,
	}}
	firstDoc.Media = []timeline.Media{{
		MediaID: "image", URI: "blob://image", Kind: timeline.MediaImage,
	}}
	secondDoc := timeline.NewDoc("项目", 1920, 1080, 30)
	secondDoc.Tracks = []timeline.Track{{TrackID: "v2", Kind: timeline.TrackVideo}}

	state := NewTimelineState(TimelineInit{
		ProjectID: "project_multi", ActiveTimelineID: "tl_second",
		Timelines: []TimelineSequence{
			{TimelineID: "tl_first", Name: "剪辑 1", Doc: firstDoc, Revision: 3, Zoom: 2},
			{TimelineID: "tl_second", Name: "剪辑 2", Doc: secondDoc, Revision: 5, CurrentFrame: 12, Zoom: .5},
		},
	}, 1_000)

	assert.Equal(t, "tl_second", state.ActiveTimelineID)
	assert.Equal(t, uint64(5), state.Revision)
	assert.Empty(t, state.Doc.Items)
	require.Len(t, state.Timelines[0].Doc.Items, 1)
	assert.Equal(t, int64(12), state.Timelines[1].CurrentFrame)
	assert.Equal(t, .5, state.Timelines[1].Zoom)
}

func TestTimelineSequenceLifecyclePreservesIndependentDocuments(t *testing.T) {
	doc := timeline.NewDoc("项目", 1920, 1080, 30)
	doc.Media = []timeline.Media{{
		MediaID: "image", URI: "blob://image", Kind: timeline.MediaImage,
	}}
	doc.Tracks = []timeline.Track{{TrackID: "v1", Kind: timeline.TrackVideo}}
	doc.Items = []timeline.Item{{
		ItemID: "first-item", TrackID: "v1", Kind: timeline.ItemImage,
		MediaID: "image", From: 0, DurationInFrames: 30,
	}}
	state := NewTimelineState(TimelineInit{
		ProjectID: "project", ActiveTimelineID: "tl_1",
		Timelines: []TimelineSequence{{
			TimelineID: "tl_1", Name: "剪辑 1", Doc: doc, Revision: 2,
			CurrentFrame: 18, Zoom: 1.5,
		}},
	}, 1)

	require.NoError(t, state.createTimeline(CreateTimelineRequest{
		TimelineID: "tl_2", Name: "剪辑 2", BaseVideoTrackID: "v2",
		CreatedAtUnixMs: 2,
	}))
	assert.Equal(t, "tl_2", state.ActiveTimelineID)
	assert.Empty(t, state.Doc.Items)
	require.Len(t, state.Doc.Media, 1, "media references remain project-available")
	require.Len(t, state.Timelines[0].Doc.Items, 1, "creating a sequence must not clear the first")

	state.Doc.Items = []timeline.Item{{
		ItemID: "second-item", TrackID: "v2", Kind: timeline.ItemImage,
		MediaID: "image", From: 60, DurationInFrames: 30,
	}}
	state.Revision = 1
	state.syncActiveSequence(3)

	require.NoError(t, state.switchTimeline(SwitchTimelineRequest{
		TimelineID: "tl_1", UpdatedAtUnixMs: 4,
	}))
	assert.Equal(t, uint64(2), state.Revision)
	require.Len(t, state.Doc.Items, 1)
	assert.Equal(t, "first-item", state.Doc.Items[0].ItemID)

	require.NoError(t, state.renameTimeline(RenameTimelineRequest{
		TimelineID: "tl_2", Name: "成片版", UpdatedAtUnixMs: 5,
	}))
	assert.Equal(t, "成片版", state.Timelines[1].Name)

	_, err := state.deleteTimeline(DeleteTimelineRequest{
		TimelineID: "tl_1", UpdatedAtUnixMs: 6,
	})
	require.NoError(t, err)
	assert.Equal(t, "tl_2", state.ActiveTimelineID)
	require.Len(t, state.Doc.Items, 1)
	assert.Equal(t, "second-item", state.Doc.Items[0].ItemID)

	_, err = state.deleteTimeline(DeleteTimelineRequest{
		TimelineID: "tl_2", UpdatedAtUnixMs: 7,
	})
	require.ErrorContains(t, err, "last timeline")
}
