package timeline

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func transcriptEditingDoc(t *testing.T) Doc {
	t.Helper()
	doc := NewDoc("transcript", 1920, 1080, 30)
	doc.Tracks = []Track{{TrackID: "v1", Kind: TrackVideo}}
	doc.Media = []Media{{
		MediaID: "m1", Generation: 7, URI: "blob://m1", Kind: MediaVideo,
		DurationInFrames: 300,
		Transcript: &Transcript{
			Revision: 4,
			Language: "zh",
			Segments: []TranscriptSegment{{
				SegmentID: "seg_server",
				StartMs:   1000,
				EndMs:     3000,
				Text:      "甲乙丙",
				Words: []TranscriptWord{
					{WordID: "word_a", StartMs: 1000, EndMs: 1500, Text: "甲"},
					{WordID: "word_b", StartMs: 1500, EndMs: 2500, Text: "乙"},
					{WordID: "word_c", StartMs: 2500, EndMs: 3000, Text: "丙"},
				},
			}},
		},
	}}
	doc.Items = []Item{
		{
			ItemID: "it1", TrackID: "v1", Kind: ItemVideo, MediaID: "m1",
			From: 100, DurationInFrames: 30, TrimStart: 30, Speed: 2,
		},
		{
			ItemID: "it2", TrackID: "v1", Kind: ItemVideo, MediaID: "m1",
			From: 200, DurationInFrames: 30, TrimStart: 30, Speed: 2,
		},
	}
	return doc
}

func TestTranscriptIdentityNormalizationAndLegacyCompatibility(t *testing.T) {
	doc := NewDoc("identity", 1920, 1080, 30)
	doc.Media = []Media{{
		MediaID: "m1", Generation: 3, URI: "blob://m1", Kind: MediaVideo,
		DurationInFrames: 300,
	}}
	incoming := &Transcript{
		Revision: 99,
		Segments: []TranscriptSegment{{
			SegmentID: "seg_external",
			StartMs:   0,
			EndMs:     1000,
			Text:      "hello",
			Words: []TranscriptWord{{
				WordID: "word_external", StartMs: 0, EndMs: 1000, Text: "hello",
			}},
		}},
	}
	first, _ := mustApply(t, doc, Operation{
		Op: OpSetTranscript, MediaID: "m1", Transcript: incoming,
	})
	got := first.Media[0].Transcript
	require.NotNil(t, got)
	assert.Equal(t, uint64(1), got.Revision)
	assert.NotEqual(t, "seg_external", got.Segments[0].SegmentID)
	assert.NotEqual(t, "word_external", got.Segments[0].Words[0].WordID)
	assert.LessOrEqual(t, len(got.Segments[0].SegmentID), MaxTranscriptIdentityLength)
	assert.LessOrEqual(t, len(got.Segments[0].Words[0].WordID), MaxTranscriptIdentityLength)

	second, _ := mustApply(t, first, Operation{
		Op: OpSetTranscript, MediaID: "m1", Transcript: incoming,
	})
	assert.Equal(t, uint64(2), second.Media[0].Transcript.Revision)
	assert.Equal(t, got.Segments[0].SegmentID, second.Media[0].Transcript.Segments[0].SegmentID)
	assert.Equal(t, got.Segments[0].Words[0].WordID, second.Media[0].Transcript.Segments[0].Words[0].WordID)
	third, _ := mustApply(t, second, Operation{
		Op: OpUpdateMedia, MediaID: "m1",
		MediaPatch: &MediaPatch{Transcript: incoming},
	})
	assert.Equal(t, uint64(3), third.Media[0].Transcript.Revision)
	assert.Equal(t, got.Segments[0].SegmentID, third.Media[0].Transcript.Segments[0].SegmentID)

	empty := NewDoc("add", 1920, 1080, 30)
	added, _ := mustApply(t, empty, Operation{
		Op: OpAddMedia,
		Media: &Media{
			MediaID: "added", Generation: 4, URI: "blob://added",
			Kind: MediaVideo, DurationInFrames: 60, Transcript: incoming,
		},
	})
	assert.Equal(t, uint64(1), added.Media[0].Transcript.Revision)
	assert.NotEqual(t, "seg_external", added.Media[0].Transcript.Segments[0].SegmentID)

	var legacy Doc
	require.NoError(t, json.Unmarshal([]byte(`{
		"schemaVersion":1,
		"metadata":{"name":"legacy","width":1920,"height":1080,"fps":30},
		"media":[{"mediaId":"m1","generation":1,"uri":"blob://m1","kind":"video","durationInFrames":60,
			"transcript":{"segments":[{"startMs":0,"endMs":1000,"text":"old"}]}}],
		"tracks":[{"trackId":"v1","kind":"video"}],
		"items":[{"itemId":"it1","trackId":"v1","kind":"video","mediaId":"m1","from":0,"durationInFrames":30,"trimStart":0}],
		"transitions":[],"captions":[]
	}`), &legacy))
	view := BuildTranscriptView("project", 1, legacy)
	require.Len(t, view.Entries, 1)
	assert.NotEmpty(t, view.Entries[0].SegmentID)
	assert.Empty(t, legacy.Media[0].Transcript.Segments[0].SegmentID)
	assert.Zero(t, legacy.Media[0].Transcript.Revision)
}

func TestBuildTranscriptViewMapsTrimSpeedAndRepeatedMedia(t *testing.T) {
	doc := transcriptEditingDoc(t)
	view := BuildTranscriptView("p1", 9, doc)
	require.Len(t, view.Entries, 6)

	assert.Equal(t, int64(100), view.Entries[0].FromFrame)
	assert.Equal(t, int64(108), view.Entries[0].ToFrame)
	assert.Equal(t, int64(1000), view.Entries[0].SourceStartMs)
	assert.Equal(t, int64(1500), view.Entries[0].SourceEndMs)
	assert.Equal(t, TranscriptGranularityWord, view.Entries[0].Granularity)
	assert.Equal(t, int64(200), view.Entries[3].FromFrame)
	assert.NotEqual(t, view.Entries[0].OccurrenceID, view.Entries[3].OccurrenceID)
	assert.Equal(t, "it1", view.Entries[0].ItemID)
	assert.Equal(t, "it2", view.Entries[3].ItemID)
}

func TestTranscriptViewFallsBackToSegmentGranularity(t *testing.T) {
	doc := transcriptEditingDoc(t)
	doc.Items = doc.Items[:1]
	doc.Media[0].Transcript.Segments[0].Words = nil
	view := BuildTranscriptView("p1", 9, doc)
	require.Len(t, view.Entries, 1)
	assert.Equal(t, TranscriptGranularitySegment, view.Entries[0].Granularity)
	assert.Empty(t, view.Entries[0].WordID)
	assert.Equal(t, int64(100), view.Entries[0].FromFrame)
	assert.Equal(t, int64(130), view.Entries[0].ToFrame)
}

func TestCompileTranscriptDeleteMergesAndOrdersBackToFront(t *testing.T) {
	doc := transcriptEditingDoc(t)
	view := BuildTranscriptView("p1", 9, doc)
	compiled, editErr := CompileTranscriptDelete("p1", 9, doc, []TranscriptDeleteRange{
		{
			FromOccurrenceID: view.Entries[0].OccurrenceID,
			ToOccurrenceID:   view.Entries[1].OccurrenceID,
		},
		{
			FromOccurrenceID: view.Entries[3].OccurrenceID,
			ToOccurrenceID:   view.Entries[4].OccurrenceID,
		},
	})
	require.Nil(t, editErr)
	require.Equal(t, []TranscriptFrameRange{
		{FromFrame: 100, ToFrame: 123},
		{FromFrame: 200, ToFrame: 223},
	}, compiled.DeletedRanges)
	require.Len(t, compiled.Operations, 2)
	assert.Equal(t, int64(200), *compiled.Operations[0].FromFrame)
	assert.Equal(t, int64(100), *compiled.Operations[1].FromFrame)
	assert.Equal(t, int64(46), compiled.DeletedDurationInFrames)
}

func TestCompileTranscriptDeleteRejectsTokensAndLockedTracks(t *testing.T) {
	doc := transcriptEditingDoc(t)
	view := BuildTranscriptView("p1", 9, doc)
	selected := []TranscriptDeleteRange{{
		FromOccurrenceID: view.Entries[0].OccurrenceID,
		ToOccurrenceID:   view.Entries[0].OccurrenceID,
	}}

	for _, testCase := range []struct {
		name         string
		projectID    string
		revision     uint64
		occurrenceID string
		expected     string
	}{
		{"forged", "p1", 9, view.Entries[0].OccurrenceID + "x", ErrInvalidOccurrenceID},
		{"cross project", "p2", 9, view.Entries[0].OccurrenceID, ErrCrossProjectOccurrence},
		{"expired", "p1", 10, view.Entries[0].OccurrenceID, ErrStaleTranscriptSelection},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ranges := append([]TranscriptDeleteRange(nil), selected...)
			ranges[0].FromOccurrenceID = testCase.occurrenceID
			ranges[0].ToOccurrenceID = testCase.occurrenceID
			_, editErr := CompileTranscriptDelete(
				testCase.projectID, testCase.revision, doc, ranges,
			)
			require.NotNil(t, editErr)
			assert.Equal(t, testCase.expected, editErr.Code)
		})
	}

	lockedDoc := doc.Clone()
	lockedDoc.Tracks = append(lockedDoc.Tracks, Track{
		TrackID: "locked", Kind: TrackAudio, Locked: true,
	})
	lockedDoc.Media = append(lockedDoc.Media, Media{
		MediaID: "audio", URI: "blob://audio", Kind: MediaAudio, DurationInFrames: 300,
	})
	lockedDoc.Items = append(lockedDoc.Items, Item{
		ItemID: "locked-item", TrackID: "locked", Kind: ItemAudio, MediaID: "audio",
		From: 500, DurationInFrames: 30,
	})
	_, editErr := CompileTranscriptDelete("p1", 9, lockedDoc, selected)
	require.NotNil(t, editErr)
	assert.Equal(t, ErrLockedTrackConflict, editErr.Code)
}

func TestCompileTranscriptDeleteBatchIsAllOrNothing(t *testing.T) {
	doc := transcriptEditingDoc(t)
	view := BuildTranscriptView("p1", 9, doc)
	before := doc.Clone()
	_, editErr := CompileTranscriptDelete("p1", 9, doc, []TranscriptDeleteRange{
		{FromOccurrenceID: view.Entries[0].OccurrenceID, ToOccurrenceID: view.Entries[0].OccurrenceID},
		{FromOccurrenceID: "forged", ToOccurrenceID: view.Entries[1].OccurrenceID},
	})
	require.NotNil(t, editErr)
	assert.Equal(t, before, doc)
}

func TestCompileTranscriptDeleteClassifiesMissingWordAndTimestamp(t *testing.T) {
	doc := transcriptEditingDoc(t)
	view := BuildTranscriptView("p1", 9, doc)
	token, parseErr := parseOccurrenceID(view.Entries[0].OccurrenceID)
	require.NoError(t, parseErr)
	token.WordFingerprint = tokenFingerprint("word_missing")
	missingWord := makeOccurrenceID(token)
	_, editErr := CompileTranscriptDelete("p1", 9, doc, []TranscriptDeleteRange{{
		FromOccurrenceID: missingWord,
		ToOccurrenceID:   missingWord,
	}})
	require.NotNil(t, editErr)
	assert.Equal(t, ErrTranscriptWordNotFound, editErr.Code)

	legacy := transcriptEditingDoc(t)
	legacy.Media[0].Transcript.Revision = 0
	legacy.Media[0].Transcript.Segments[0].StartMs = 1000
	legacy.Media[0].Transcript.Segments[0].EndMs = 1000
	legacy.Media[0].Transcript.Segments[0].Words = nil
	stable := transcriptWithStableIdentities(
		legacy.Media[0].MediaID,
		legacy.Media[0].Generation,
		legacy.Media[0].Transcript,
	)
	unavailable := makeOccurrenceID(occurrenceToken{
		Version:            1,
		ProjectFingerprint: tokenFingerprint("p1"),
		TimelineRevision:   9,
		ItemFingerprint:    tokenFingerprint("it1"),
		MediaFingerprint:   tokenFingerprint("m1"),
		MediaGeneration:    legacy.Media[0].Generation,
		TranscriptRevision: 0,
		SegmentFingerprint: tokenFingerprint(stable.Segments[0].SegmentID),
		Granularity:        TranscriptGranularitySegment,
	})
	_, editErr = CompileTranscriptDelete("p1", 9, legacy, []TranscriptDeleteRange{{
		FromOccurrenceID: unavailable,
		ToOccurrenceID:   unavailable,
	}})
	require.NotNil(t, editErr)
	assert.Equal(t, ErrTranscriptTimestamp, editErr.Code)
}
