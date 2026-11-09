package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"

	"agentcut/entity"
	"agentcut/preparemedia"
	"agentcut/timeline"
)

// ---- 测试替身 ----

type fakeHandle struct {
	result any
	err    error
}

func (h *fakeHandle) WorkflowID() string { return "" }
func (h *fakeHandle) RunID() string      { return "" }
func (h *fakeHandle) UpdateID() string   { return "" }
func (h *fakeHandle) Get(ctx context.Context, valuePtr any) error {
	if h.err != nil {
		return h.err
	}
	b, _ := json.Marshal(h.result)
	return json.Unmarshal(b, valuePtr)
}

type fakeEncodedValue struct{ v any }

func (f fakeEncodedValue) HasValue() bool { return f.v != nil }
func (f fakeEncodedValue) Get(valuePtr any) error {
	b, err := json.Marshal(f.v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, valuePtr)
}

type fakeTemporal struct {
	lastUpdate *client.UpdateWorkflowOptions
	lastStart  *client.StartWorkflowOptions
	handle     *fakeHandle
	queryState entity.TimelineState
	queryErr   error
	updateErr  error
	startErr   error
}

type fakeWorkflowRun struct{}

func (*fakeWorkflowRun) GetID() string                  { return "workflow" }
func (*fakeWorkflowRun) GetRunID() string               { return "run" }
func (*fakeWorkflowRun) Get(context.Context, any) error { return nil }
func (*fakeWorkflowRun) GetWithOptions(context.Context, any, client.WorkflowRunGetOptions) error {
	return nil
}

func (f *fakeTemporal) UpdateWorkflow(ctx context.Context, options client.UpdateWorkflowOptions) (client.WorkflowUpdateHandle, error) {
	f.lastUpdate = &options
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return f.handle, nil
}

func (f *fakeTemporal) QueryWorkflow(ctx context.Context, workflowID, runID, queryType string, args ...any) (converter.EncodedValue, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return fakeEncodedValue{v: f.queryState}, nil
}

func (f *fakeTemporal) ExecuteWorkflow(
	ctx context.Context, options client.StartWorkflowOptions, workflow interface{}, args ...interface{},
) (client.WorkflowRun, error) {
	f.lastStart = &options
	if f.startErr != nil {
		return nil, f.startErr
	}
	return &fakeWorkflowRun{}, nil
}

// ---- timeline_get ----

func TestTimelineGet(t *testing.T) {
	doc := timeline.NewDoc("p", 1920, 1080, 30)
	doc.Tracks = []timeline.Track{{TrackID: "c1", Kind: timeline.TrackCaption}}
	doc.Items = []timeline.Item{{ItemID: "i1", TrackID: "c1", Kind: timeline.ItemText, From: 0, DurationInFrames: 90, Text: "hi"}}
	ft := &fakeTemporal{queryState: entity.TimelineState{ProjectID: "p1", Doc: doc, Revision: 7}}
	a := &Activities{Temporal: ft}

	out, err := a.TimelineGet(context.Background(), GetInput{ProjectID: "p1"})
	require.NoError(t, err)
	assert.Equal(t, uint64(7), out.Revision)
	assert.Equal(t, int64(90), out.DurationInFrames)
	assert.Len(t, out.Doc.Items, 1)

	_, err = a.TimelineGet(context.Background(), GetInput{})
	require.Error(t, err)

	ft.queryErr = errors.New("workflow not found")
	_, err = a.TimelineGet(context.Background(), GetInput{ProjectID: "p1"})
	require.Error(t, err)
}

// ---- 白名单 ----

func TestEditClipsWhitelist(t *testing.T) {
	ft := &fakeTemporal{handle: &fakeHandle{result: entity.ApplyOpsResult{Revision: 1}}}
	a := &Activities{Temporal: ft}

	// edit_clips 不允许 addTrack
	out, err := a.TimelineEditClips(context.Background(), EditInput{
		ProjectID: "p1", BaseRevision: 0,
		Operations: []timeline.Operation{{Op: timeline.OpAddTrack, Track: &timeline.Track{TrackID: "v9", Kind: timeline.TrackVideo}}},
	})
	require.NoError(t, err)
	assert.False(t, out.OK)
	assert.Equal(t, string(timeline.ErrUnsupported), out.Code)
	assert.Nil(t, ft.lastUpdate, "whitelist rejection must not reach the workflow")

	// edit_clips 也不允许媒体登记类 op
	out, err = a.TimelineEditClips(context.Background(), EditInput{
		ProjectID:  "p1",
		Operations: []timeline.Operation{{Op: timeline.OpAddMedia, Media: &timeline.Media{MediaID: "m", URI: "u", Kind: timeline.MediaVideo, DurationInFrames: 1}}},
	})
	require.NoError(t, err)
	assert.False(t, out.OK)
	assert.Nil(t, ft.lastUpdate)

	// edit_tracks 不允许片段类 op
	out, err = a.TimelineEditTracks(context.Background(), EditInput{
		ProjectID:  "p1",
		Operations: []timeline.Operation{{Op: timeline.OpSplit, ItemID: "i1", Frame: ptr(int64(5))}},
	})
	require.NoError(t, err)
	assert.False(t, out.OK)
	assert.Nil(t, ft.lastUpdate)
}

func ptr[T any](v T) *T { return &v }

// ---- 成功路径与请求形状 ----

func TestEditClipsSuccessShape(t *testing.T) {
	ft := &fakeTemporal{handle: &fakeHandle{result: entity.ApplyOpsResult{
		Revision: 3,
		Summary:  timeline.ChangeSummary{UpdatedItemIDs: []string{"i1"}},
	}}}
	a := &Activities{Temporal: ft}

	out, err := a.TimelineEditClips(context.Background(), EditInput{
		ProjectID: "p1", BaseRevision: 2,
		Operations: []timeline.Operation{{Op: timeline.OpMoveItem, ItemID: "i1", From: ptr(int64(100))}},
	})
	require.NoError(t, err)
	assert.True(t, out.OK)
	assert.Equal(t, uint64(3), out.Revision)
	require.NotNil(t, out.Summary)
	assert.Equal(t, []string{"i1"}, out.Summary.UpdatedItemIDs)

	// 请求发到了正确的 Workflow，source 固定 agent
	require.NotNil(t, ft.lastUpdate)
	assert.Equal(t, entity.TimelineWorkflowID("p1"), ft.lastUpdate.WorkflowID)
	assert.Equal(t, entity.UpdateApplyOps, ft.lastUpdate.UpdateName)
	assert.Equal(t, entity.TimelineMutationUpdateID("p1", "agent", ""), ft.lastUpdate.UpdateID)
	require.Len(t, ft.lastUpdate.Args, 1)
	req := ft.lastUpdate.Args[0].(entity.ApplyOpsRequest)
	assert.Equal(t, "agent", req.Source)
	assert.Equal(t, uint64(2), req.BaseRevision)
}

func TestSetCaptionsBuildsSingleOp(t *testing.T) {
	ft := &fakeTemporal{handle: &fakeHandle{result: entity.ApplyOpsResult{Revision: 5}}}
	a := &Activities{Temporal: ft}

	out, err := a.TimelineSetCaptions(context.Background(), SetCaptionsInput{
		ProjectID: "p1", BaseRevision: 4, TrackID: "c1",
		Captions: []timeline.Caption{{From: 0, DurationInFrames: 60, Text: "第一句"}},
	})
	require.NoError(t, err)
	assert.True(t, out.OK)
	req := ft.lastUpdate.Args[0].(entity.ApplyOpsRequest)
	require.Len(t, req.Operations, 1)
	assert.Equal(t, timeline.OpSetCaptions, req.Operations[0].Op)
	assert.Equal(t, "c1", req.Operations[0].TrackID)
}

func TestTimelineEditTranscriptViewDeleteAndSessionBoundary(t *testing.T) {
	doc := timeline.NewDoc("transcript", 1920, 1080, 30)
	doc.Tracks = []timeline.Track{{TrackID: "v1", Kind: timeline.TrackVideo}}
	doc.Media = []timeline.Media{{
		MediaID: "m1", Generation: 2, URI: "blob://m1", Kind: timeline.MediaVideo,
		DurationInFrames: 300,
		Transcript: &timeline.Transcript{
			Revision: 3,
			Segments: []timeline.TranscriptSegment{{
				SegmentID: "seg_one", StartMs: 0, EndMs: 1000, Text: "hello",
				Words: []timeline.TranscriptWord{{
					WordID: "word_hello", StartMs: 0, EndMs: 500, Text: "hello",
				}},
			}},
		},
	}}
	doc.Items = []timeline.Item{{
		ItemID: "it1", TrackID: "v1", Kind: timeline.ItemVideo, MediaID: "m1",
		From: 0, DurationInFrames: 60,
	}}
	ft := &fakeTemporal{queryState: entity.TimelineState{
		ProjectID: "p1", AgentKSessionID: "ses_test", Doc: doc, Revision: 7,
	}}
	activities := &Activities{
		Temporal:  ft,
		SessionID: func(context.Context) string { return "ses_test" },
	}
	view, err := activities.TimelineEditTranscript(context.Background(), TranscriptEditInput{
		ProjectID: "p1", Action: "view", Limit: 20,
	})
	require.NoError(t, err)
	require.True(t, view.OK)
	assert.Equal(t, uint64(7), view.TimelineRevision)
	require.Len(t, view.Entries, 1)

	ft.handle = &fakeHandle{result: entity.DeleteTranscriptRangesResult{
		Revision: 8,
		Doc:      doc,
		Summary: timeline.ChangeSummary{
			UpdatedItemIDs: []string{"it1"},
		},
		DeletedRanges:           []timeline.TranscriptFrameRange{{FromFrame: 0, ToFrame: 15}},
		DeletedDurationInFrames: 15,
	}}
	deleted, err := activities.TimelineEditTranscript(context.Background(), TranscriptEditInput{
		ProjectID: "p1", Action: "delete", BaseRevision: 7, Mode: "ripple",
		Ranges: []timeline.TranscriptDeleteRange{{
			FromOccurrenceID: view.Entries[0].OccurrenceID,
			ToOccurrenceID:   view.Entries[0].OccurrenceID,
		}},
	})
	require.NoError(t, err)
	require.True(t, deleted.OK)
	assert.Equal(t, uint64(8), deleted.Revision)
	assert.Equal(t, int64(15), deleted.DeletedDurationInFrames)
	require.NotNil(t, ft.lastUpdate)
	assert.Equal(t, entity.UpdateDeleteTranscript, ft.lastUpdate.UpdateName)
	assert.Equal(
		t,
		entity.TimelineMutationUpdateID("p1", "agent", ""),
		ft.lastUpdate.UpdateID,
	)
	updateReq := ft.lastUpdate.Args[0].(entity.DeleteTranscriptRangesRequest)
	assert.Equal(t, "agent", updateReq.Source)
	assert.Equal(t, "ripple", updateReq.Mode)

	activities.SessionID = func(context.Context) string { return "ses_other" }
	rejected, err := activities.TimelineEditTranscript(context.Background(), TranscriptEditInput{
		ProjectID: "p1", Action: "view",
	})
	require.NoError(t, err)
	assert.False(t, rejected.OK)
	assert.Contains(t, rejected.Error, "not bound")
}

func TestTimelineEditMediaStartsVerifiedArchiveAndRejectsUntrustedURL(t *testing.T) {
	t.Setenv("AGENTCUT_BLOB_ENDPOINT", "https://blob.example")
	t.Setenv("AGENTCUT_BLOB_BUCKET", "bucket")
	ft := &fakeTemporal{handle: &fakeHandle{result: entity.ApplyOpsResult{
		Revision: 2, AddedMediaGenerations: map[string]uint64{"m1": 1},
	}}, queryState: entity.TimelineState{AgentKSessionID: "ses_test"}}
	a := &Activities{
		Temporal: ft, TaskQueue: "agentcut-test",
		SessionID: func(context.Context) string { return "ses_test" },
	}
	media := timeline.Media{
		MediaID: "m1",
		URI:     "https://blob.example/bucket/agentk/artifacts/generated.mp4",
		Kind:    timeline.MediaVideo, DurationInFrames: 90,
	}
	out, err := a.TimelineEditMedia(context.Background(), EditInput{
		ProjectID: "p1", BaseRevision: 1,
		Operations: []timeline.Operation{{Op: timeline.OpAddMedia, Media: &media}},
	})
	require.NoError(t, err)
	assert.True(t, out.OK)
	req := ft.lastUpdate.Args[0].(entity.ApplyOpsRequest)
	added := req.Operations[0].Media
	require.NotNil(t, added)
	assert.Empty(t, added.BlobKey, "Agent URL must not become an authoritative BlobKey before archival")
	assert.Nil(t, added.Processing, "agent addMedia must be clean at the workflow boundary")
	require.NotNil(t, ft.lastStart)
	assert.Equal(t, preparemedia.WorkflowID("p1", "m1", 1), ft.lastStart.ID)
	assert.Equal(t, "agentcut-test", ft.lastStart.TaskQueue)

	ft.lastUpdate = nil
	out, err = a.TimelineEditMedia(context.Background(), EditInput{
		ProjectID: "p1", BaseRevision: 2,
		Operations: []timeline.Operation{{Op: timeline.OpAddMedia, Media: &timeline.Media{
			MediaID: "evil", URI: "http://127.0.0.1/admin",
			Kind: timeline.MediaVideo, DurationInFrames: 30,
		}}},
	})
	require.NoError(t, err)
	assert.False(t, out.OK)
	assert.Equal(t, string(timeline.ErrBadRequest), out.Code)
	assert.Nil(t, ft.lastUpdate)
}

func TestTimelineEditMediaRenamesExistingMediaWithoutOpeningSystemFields(t *testing.T) {
	ft := &fakeTemporal{
		handle:     &fakeHandle{result: entity.ApplyOpsResult{Revision: 2}},
		queryState: entity.TimelineState{AgentKSessionID: "ses_test"},
	}
	a := &Activities{
		Temporal:  ft,
		SessionID: func(context.Context) string { return "ses_test" },
	}
	name := "MacBook Neo 评测"
	out, err := a.TimelineEditMedia(context.Background(), EditInput{
		ProjectID: "p1", BaseRevision: 1,
		Operations: []timeline.Operation{{
			Op: timeline.OpUpdateMedia, MediaID: "m1",
			MediaPatch: &timeline.MediaPatch{DisplayName: &name},
		}},
	})
	require.NoError(t, err)
	assert.True(t, out.OK)
	req := ft.lastUpdate.Args[0].(entity.ApplyOpsRequest)
	require.Len(t, req.Operations, 1)
	require.NotNil(t, req.Operations[0].MediaPatch)
	assert.Equal(t, name, *req.Operations[0].MediaPatch.DisplayName)
	assert.Nil(t, ft.lastStart, "renaming must not restart media preparation")

	uri := "https://example.invalid/replacement.mp4"
	ft.lastUpdate = nil
	out, err = a.TimelineEditMedia(context.Background(), EditInput{
		ProjectID: "p1", BaseRevision: 2,
		Operations: []timeline.Operation{{
			Op: timeline.OpUpdateMedia, MediaID: "m1",
			MediaPatch: &timeline.MediaPatch{DisplayName: &name, URI: &uri},
		}},
	})
	require.NoError(t, err)
	assert.False(t, out.OK)
	assert.Equal(t, string(timeline.ErrBadRequest), out.Code)
	assert.Nil(t, ft.lastUpdate)
}

func TestTimelineEditMediaIgnoresBlobKeyAndStartFailureAfterCommit(t *testing.T) {
	t.Setenv("AGENTCUT_BLOB_ENDPOINT", "https://blob.example")
	t.Setenv("AGENTCUT_BLOB_BUCKET", "bucket")
	ft := &fakeTemporal{
		handle: &fakeHandle{result: entity.ApplyOpsResult{
			Revision: 2, AddedMediaGenerations: map[string]uint64{"m1": 3},
		}},
		queryState: entity.TimelineState{AgentKSessionID: "ses_test"},
		startErr:   errors.New("temporal start unavailable"),
	}
	a := &Activities{
		Temporal: ft, SessionID: func(context.Context) string { return "ses_test" },
	}
	out, err := a.TimelineEditMedia(context.Background(), EditInput{
		ProjectID: "p1", BaseRevision: 1,
		Operations: []timeline.Operation{{Op: timeline.OpAddMedia, Media: &timeline.Media{
			MediaID: "m1",
			URI:     "https://blob.example/bucket/agentk/artifacts/generated.mp4",
			BlobKey: "agentcut/other/source/m1/stolen.mp4",
			Kind:    timeline.MediaVideo, DurationInFrames: 30,
		}}},
	})
	require.NoError(t, err)
	assert.True(t, out.OK, "committed addMedia must remain successful when prepare start fails")
	req := ft.lastUpdate.Args[0].(entity.ApplyOpsRequest)
	assert.Empty(t, req.Operations[0].Media.BlobKey)
}

func TestTimelineEditMediaRejectsProjectOutsideCurrentSession(t *testing.T) {
	ft := &fakeTemporal{
		handle:     &fakeHandle{},
		queryState: entity.TimelineState{AgentKSessionID: "ses_other"},
	}
	a := &Activities{
		Temporal: ft, SessionID: func(context.Context) string { return "ses_current" },
	}
	out, err := a.TimelineEditMedia(context.Background(), EditInput{
		ProjectID: "p1",
		Operations: []timeline.Operation{{Op: timeline.OpAddMedia, Media: &timeline.Media{
			MediaID: "m1", URI: "https://blob.example/generated.mp3",
			Kind: timeline.MediaAudio, DurationInFrames: 30,
		}}},
	})
	require.NoError(t, err)
	assert.False(t, out.OK)
	assert.Contains(t, out.Error, "not bound")
	assert.Nil(t, ft.lastUpdate)
}

func TestTimelineEditMediaDefersArtifactOwnershipToEventProvenanceAndRejectsSetTranscript(t *testing.T) {
	t.Setenv("AGENTCUT_BLOB_ENDPOINT", "https://blob.example")
	t.Setenv("AGENTCUT_BLOB_BUCKET", "bucket")
	ft := &fakeTemporal{
		handle: &fakeHandle{}, queryState: entity.TimelineState{AgentKSessionID: "ses_test"},
	}
	a := &Activities{
		Temporal: ft, SessionID: func(context.Context) string { return "ses_test" },
	}
	out, err := a.TimelineEditMedia(context.Background(), EditInput{
		ProjectID: "p1",
		Operations: []timeline.Operation{{Op: timeline.OpAddMedia, Media: &timeline.Media{
			MediaID: "m1",
			URI:     "https://blob.example/bucket/agentcut/p2/source/m1/video.mp4",
			Kind:    timeline.MediaVideo, DurationInFrames: 30,
		}}},
	})
	require.NoError(t, err)
	assert.True(t, out.OK)
	require.NotNil(t, ft.lastUpdate)
	req := ft.lastUpdate.Args[0].(entity.ApplyOpsRequest)
	assert.Empty(t, req.Operations[0].Media.BlobKey)

	out, err = a.TimelineEditMedia(context.Background(), EditInput{
		ProjectID: "p1",
		Operations: []timeline.Operation{{
			Op: timeline.OpSetTranscript, MediaID: "m1",
			Transcript: &timeline.Transcript{},
		}},
	})
	require.NoError(t, err)
	assert.False(t, out.OK)
}

// ---- 错误映射 ----

func TestRevisionConflictMapping(t *testing.T) {
	conflictErr := temporal.NewApplicationErrorWithOptions(
		"base revision 2 does not match latest 9",
		entity.ErrTypeRevisionConflict,
		temporal.ApplicationErrorOptions{NonRetryable: true, Details: []any{entity.RevisionConflictDetails{LatestRevision: 9}}},
	)
	ft := &fakeTemporal{handle: &fakeHandle{err: conflictErr}}
	a := &Activities{Temporal: ft}

	out, err := a.TimelineEditClips(context.Background(), EditInput{
		ProjectID: "p1", BaseRevision: 2,
		Operations: []timeline.Operation{{Op: timeline.OpRemoveItems, ItemIDs: []string{"i1"}}},
	})
	require.NoError(t, err, "domain conflicts must not fail the activity")
	assert.False(t, out.OK)
	assert.Equal(t, entity.ErrTypeRevisionConflict, out.Code)
	assert.Equal(t, uint64(9), out.LatestRevision)
}

func TestOpFailedMapping(t *testing.T) {
	opErr := temporal.NewApplicationErrorWithOptions(
		"op[0] removeItems: NOT_FOUND: item \"x\" not found",
		entity.ErrTypeOpFailed,
		temporal.ApplicationErrorOptions{NonRetryable: true, Details: []any{timeline.OpError{Index: 0, Op: "removeItems", Code: timeline.ErrNotFound, Message: `item "x" not found`}}},
	)
	ft := &fakeTemporal{handle: &fakeHandle{err: opErr}}
	a := &Activities{Temporal: ft}

	out, err := a.TimelineEditClips(context.Background(), EditInput{
		ProjectID:  "p1",
		Operations: []timeline.Operation{{Op: timeline.OpRemoveItems, ItemIDs: []string{"x"}}},
	})
	require.NoError(t, err)
	assert.False(t, out.OK)
	assert.Equal(t, string(timeline.ErrNotFound), out.Code)
}

func TestUnexpectedErrorFailsActivity(t *testing.T) {
	ft := &fakeTemporal{updateErr: errors.New("connection refused")}
	a := &Activities{Temporal: ft}
	_, err := a.TimelineEditClips(context.Background(), EditInput{
		ProjectID:  "p1",
		Operations: []timeline.Operation{{Op: timeline.OpRemoveItems, ItemIDs: []string{"x"}}},
	})
	require.Error(t, err, "infra errors should fail the activity and be retried")
}

func TestUpdateIDOutsideActivity(t *testing.T) {
	assert.Equal(t, "", updateID(context.Background()))
}
