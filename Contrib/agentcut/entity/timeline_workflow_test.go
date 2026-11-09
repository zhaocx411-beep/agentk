package entity

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"agentcut/timeline"
)

func i64(v int64) *int64 { return &v }

func baseInit() TimelineInit {
	d := timeline.NewDoc("测试项目", 1920, 1080, 30)
	d.Media = []timeline.Media{{MediaID: "m1", URI: "blob://m1", Kind: timeline.MediaVideo, DurationInFrames: 300}}
	d.Tracks = []timeline.Track{{TrackID: "v1", Kind: timeline.TrackVideo}}
	return TimelineInit{ProjectID: "p1", Doc: d, Revision: 0, DisableSnapshots: true}
}

func addItemReq(base uint64, itemID string, from int64) ApplyOpsRequest {
	return ApplyOpsRequest{
		BaseRevision: base,
		Source:       "user",
		Operations: []timeline.Operation{{
			Op: timeline.OpAddItem,
			Item: &timeline.Item{
				ItemID: itemID, TrackID: "v1", Kind: timeline.ItemVideo, MediaID: "m1",
				From: from, DurationInFrames: 50,
			},
		}},
	}
}

// updateResult 收集一次 Update 的结果。
type updateResult struct {
	res ApplyOpsResult
	err error
}

type transcriptUpdateResult struct {
	res DeleteTranscriptRangesResult
	err error
}

// decodeUpdateResult 兼容测试环境两种结果形态：EncodedValue 或原始返回值。
func decodeUpdateResult(result any, out any) error {
	if result == nil {
		return nil
	}
	if ev, ok := result.(converter.EncodedValue); ok {
		return ev.Get(out)
	}
	b, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func sendUpdate(t *testing.T, env *testsuite.TestWorkflowEnvironment, delay time.Duration, updateID string, req ApplyOpsRequest, out *updateResult) {
	t.Helper()
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(UpdateApplyOps, updateID, &testsuite.TestUpdateCallback{
			OnAccept: func() {},
			OnReject: func(err error) { out.err = err },
			OnComplete: func(result any, err error) {
				if err != nil {
					out.err = err
					return
				}
				out.err = decodeUpdateResult(result, &out.res)
			},
		}, req)
	}, delay)
}

func sendTranscriptUpdate(
	t *testing.T,
	env *testsuite.TestWorkflowEnvironment,
	delay time.Duration,
	updateID string,
	req DeleteTranscriptRangesRequest,
	out *transcriptUpdateResult,
) {
	t.Helper()
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(UpdateDeleteTranscript, updateID, &testsuite.TestUpdateCallback{
			OnAccept: func() {},
			OnReject: func(err error) { out.err = err },
			OnComplete: func(result any, err error) {
				if err != nil {
					out.err = err
					return
				}
				out.err = decodeUpdateResult(result, &out.res)
			},
		}, req)
	}, delay)
}

func transcriptInit() TimelineInit {
	doc := timeline.NewDoc("transcript", 1920, 1080, 30)
	doc.Tracks = []timeline.Track{{TrackID: "v1", Kind: timeline.TrackVideo}}
	doc.Media = []timeline.Media{{
		MediaID: "m1", Generation: 2, URI: "blob://m1", Kind: timeline.MediaVideo,
		DurationInFrames: 300,
		Transcript: &timeline.Transcript{
			Revision: 3,
			Segments: []timeline.TranscriptSegment{{
				SegmentID: "seg_one", StartMs: 0, EndMs: 1000, Text: "hello",
				Words: []timeline.TranscriptWord{
					{WordID: "word_hello", StartMs: 0, EndMs: 500, Text: "hello"},
					{WordID: "word_world", StartMs: 500, EndMs: 1000, Text: "world"},
				},
			}},
		},
	}}
	doc.Items = []timeline.Item{{
		ItemID: "it1", TrackID: "v1", Kind: timeline.ItemVideo, MediaID: "m1",
		From: 0, DurationInFrames: 60,
	}}
	return TimelineInit{
		ProjectID: "p1", Doc: doc, Revision: 5,
		DisableSnapshots: true, DisableMediaCleanup: true,
	}
}

func queryState(t *testing.T, env *testsuite.TestWorkflowEnvironment) TimelineState {
	t.Helper()
	v, err := env.QueryWorkflow(QueryGetState)
	require.NoError(t, err)
	var st TimelineState
	require.NoError(t, v.Get(&st))
	return st
}

func TestTimelineWorkflowApplyAndQuery(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(TimelineEntityWorkflow)

	var r1, r2 updateResult
	sendUpdate(t, env, time.Millisecond, "req-1", addItemReq(0, "it1", 0), &r1)
	sendUpdate(t, env, 2*time.Millisecond, "req-2", addItemReq(1, "it2", 100), &r2)
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(SignalDeleteTimeline, nil) }, 3*time.Millisecond)

	env.ExecuteWorkflow(TimelineEntityWorkflow, baseInit())
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	require.NoError(t, r1.err)
	assert.Equal(t, uint64(1), r1.res.Revision)
	assert.Equal(t, []string{"it1"}, r1.res.Summary.AddedItemIDs)
	require.NoError(t, r2.err)
	assert.Equal(t, uint64(2), r2.res.Revision)

	st := queryState(t, env)
	assert.Equal(t, uint64(2), st.Revision)
	assert.Len(t, st.Doc.Items, 2)
}

func TestTimelineWorkflowScopesMutationsToActiveSequence(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(TimelineEntityWorkflow)

	firstDoc := timeline.NewDoc("project", 1920, 1080, 30)
	firstDoc.Tracks = []timeline.Track{{TrackID: "v1", Kind: timeline.TrackVideo}}
	secondDoc := timeline.NewDoc("project", 1920, 1080, 30)
	secondDoc.Tracks = []timeline.Track{{TrackID: "v2", Kind: timeline.TrackVideo}}
	init := TimelineInit{
		ProjectID: "p1", ActiveTimelineID: "tl_first",
		Timelines: []TimelineSequence{
			{TimelineID: "tl_first", Name: "剪辑 1", Doc: firstDoc, Zoom: 1},
			{TimelineID: "tl_second", Name: "剪辑 2", Doc: secondDoc, Zoom: 1},
		},
		DisableSnapshots: true,
	}

	var inactive, active updateResult
	inactiveReq := ApplyOpsRequest{
		TimelineID: "tl_second", BaseRevision: 0, Source: "user",
		Operations: []timeline.Operation{{
			Op:    timeline.OpAddTrack,
			Track: &timeline.Track{TrackID: "a2", Kind: timeline.TrackAudio},
		}},
	}
	activeReq := inactiveReq
	activeReq.TimelineID = "tl_first"
	activeReq.Operations = []timeline.Operation{{
		Op:    timeline.OpAddTrack,
		Track: &timeline.Track{TrackID: "a1", Kind: timeline.TrackAudio},
	}}
	sendUpdate(t, env, time.Millisecond, "inactive", inactiveReq, &inactive)
	sendUpdate(t, env, 2*time.Millisecond, "active", activeReq, &active)
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(SignalDeleteTimeline, nil) }, 3*time.Millisecond)

	env.ExecuteWorkflow(TimelineEntityWorkflow, init)
	require.NoError(t, env.GetWorkflowError())
	require.Error(t, inactive.err)
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, inactive.err, &appErr)
	assert.Equal(t, ErrTypeTimelineNotActive, appErr.Type())
	require.NoError(t, active.err)

	state := queryState(t, env)
	assert.Equal(t, uint64(1), state.Revision)
	require.Len(t, state.Timelines, 2)
	assert.Len(t, state.Timelines[0].Doc.Tracks, 2)
	assert.Len(t, state.Timelines[1].Doc.Tracks, 1)
}

func TestTimelineWorkflowPersistsActiveSequenceView(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(TimelineEntityWorkflow)
	var updateErr error
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(UpdateTimelineView, "view-1", &testsuite.TestUpdateCallback{
			OnAccept: func() {},
			OnReject: func(err error) { updateErr = err },
			OnComplete: func(_ any, err error) {
				updateErr = err
			},
		}, UpdateTimelineViewRequest{
			TimelineID: "tl_first", CurrentFrame: 42, Zoom: 1.75, UpdatedAtUnixMs: 1_234,
		})
	}, time.Millisecond)
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(SignalDeleteTimeline, nil) }, 2*time.Millisecond)

	doc := timeline.NewDoc("project", 1920, 1080, 30)
	doc.Tracks = []timeline.Track{{TrackID: "v1", Kind: timeline.TrackVideo}}
	env.ExecuteWorkflow(TimelineEntityWorkflow, TimelineInit{
		ProjectID: "p1", ActiveTimelineID: "tl_first",
		Timelines: []TimelineSequence{{
			TimelineID: "tl_first", Name: "剪辑 1", Doc: doc, Zoom: 1,
		}},
		DisableSnapshots: true,
	})

	require.NoError(t, env.GetWorkflowError())
	require.NoError(t, updateErr)
	state := queryState(t, env)
	require.Len(t, state.Timelines, 1)
	assert.Equal(t, int64(42), state.Timelines[0].CurrentFrame)
	assert.Equal(t, 1.75, state.Timelines[0].Zoom)
	assert.Equal(t, int64(1_234), state.Timelines[0].UpdatedAtUnixMs)
}

func TestTimelineWorkflowRejectsNonSystemProcessingPatch(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(TimelineEntityWorkflow)
	var rejected error
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(UpdateApplyOps, "user-processing", &testsuite.TestUpdateCallback{
			OnAccept: func() {},
			OnReject: func(err error) { rejected = err },
			OnComplete: func(_ any, err error) {
				if err != nil {
					rejected = err
				}
			},
		}, ApplyOpsRequest{
			BaseRevision: 0, Source: "user",
			Operations: []timeline.Operation{{
				Op: timeline.OpUpdateMedia, MediaID: "m1",
				MediaPatch: &timeline.MediaPatch{Processing: &timeline.MediaProcessing{
					Status: "processing", ReadyForTimeline: true,
					Probe: timeline.ProcessingSucceeded, Thumbnail: timeline.ProcessingPending,
					Waveform: timeline.ProcessingPending, Transcript: timeline.ProcessingPending,
					Understanding: timeline.ProcessingPending,
				}},
			}},
		})
	}, time.Millisecond)
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(SignalDeleteTimeline, nil) }, 2*time.Millisecond)

	env.ExecuteWorkflow(TimelineEntityWorkflow, baseInit())
	require.NoError(t, env.GetWorkflowError())
	require.Error(t, rejected)
	assert.Contains(t, rejected.Error(), "system workflows")
}

func TestTimelineWorkflowBindsAgentKSessionOnce(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(TimelineEntityWorkflow)
	var firstErr, conflictingErr error
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(UpdateBindAgentKSession, "bind-1", &testsuite.TestUpdateCallback{
			OnAccept: func() {},
			OnReject: func(err error) { firstErr = err },
			OnComplete: func(_ any, err error) {
				firstErr = err
			},
		}, BindAgentKSessionRequest{SessionID: "ses_1"})
	}, time.Millisecond)
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(UpdateBindAgentKSession, "bind-2", &testsuite.TestUpdateCallback{
			OnAccept: func() {},
			OnReject: func(err error) { conflictingErr = err },
			OnComplete: func(_ any, err error) {
				conflictingErr = err
			},
		}, BindAgentKSessionRequest{SessionID: "ses_2"})
	}, 2*time.Millisecond)
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(SignalDeleteTimeline, nil) }, 3*time.Millisecond)

	env.ExecuteWorkflow(TimelineEntityWorkflow, baseInit())
	require.NoError(t, env.GetWorkflowError())
	require.NoError(t, firstErr)
	require.Error(t, conflictingErr)
	assert.Equal(t, "ses_1", queryState(t, env).AgentKSessionID)
}

func TestTimelineWorkflowShareGrantLifecycle(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(TimelineEntityWorkflow)

	var createErr, revokeErr error
	var valid, expired, revoked SharedTimelineResult
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(UpdateCreateShare, "share-create", &testsuite.TestUpdateCallback{
			OnAccept: func() {},
			OnReject: func(err error) { createErr = err },
			OnComplete: func(_ any, err error) {
				createErr = err
			},
		}, CreateShareRequest{TokenHash: "hash-1", NowUnixMs: 1_000, ExpiresAtUnixMs: 10_000})
	}, time.Millisecond)
	env.RegisterDelayedCallback(func() {
		value, err := env.QueryWorkflow(QuerySharedTimeline,
			SharedTimelineRequest{TokenHash: "hash-1", NowUnixMs: 9_999})
		require.NoError(t, err)
		require.NoError(t, value.Get(&valid))

		value, err = env.QueryWorkflow(QuerySharedTimeline,
			SharedTimelineRequest{TokenHash: "hash-1", NowUnixMs: 10_000})
		require.NoError(t, err)
		require.NoError(t, value.Get(&expired))
	}, 2*time.Millisecond)
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(UpdateRevokeShare, "share-revoke", &testsuite.TestUpdateCallback{
			OnAccept: func() {},
			OnReject: func(err error) { revokeErr = err },
			OnComplete: func(_ any, err error) {
				revokeErr = err
			},
		}, RevokeShareRequest{TokenHash: "hash-1"})
	}, 3*time.Millisecond)
	env.RegisterDelayedCallback(func() {
		value, err := env.QueryWorkflow(QuerySharedTimeline,
			SharedTimelineRequest{TokenHash: "hash-1", NowUnixMs: 5_000})
		require.NoError(t, err)
		require.NoError(t, value.Get(&revoked))
		env.SignalWorkflow(SignalDeleteTimeline, nil)
	}, 4*time.Millisecond)

	env.ExecuteWorkflow(TimelineEntityWorkflow, baseInit())
	require.NoError(t, env.GetWorkflowError())
	require.NoError(t, createErr)
	require.NoError(t, revokeErr)
	assert.Equal(t, "valid", valid.Status)
	assert.Equal(t, "p1", valid.State.ProjectID)
	assert.Equal(t, "expired", expired.Status)
	assert.Equal(t, "not_found", revoked.Status)
}

func TestTimelineWorkflowRevisionConflict(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(TimelineEntityWorkflow)

	var ok1, stale updateResult
	sendUpdate(t, env, time.Millisecond, "req-1", addItemReq(0, "it1", 0), &ok1)
	// 第二个请求仍然带 baseRevision=0（模拟并发编辑里晚到的一方）
	sendUpdate(t, env, 2*time.Millisecond, "req-2", addItemReq(0, "it2", 100), &stale)
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(SignalDeleteTimeline, nil) }, 3*time.Millisecond)

	env.ExecuteWorkflow(TimelineEntityWorkflow, baseInit())
	require.NoError(t, env.GetWorkflowError())

	require.NoError(t, ok1.err)
	require.Error(t, stale.err)
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, stale.err, &appErr)
	assert.Equal(t, ErrTypeRevisionConflict, appErr.Type())
	var details RevisionConflictDetails
	require.NoError(t, appErr.Details(&details))
	assert.Equal(t, uint64(1), details.LatestRevision)

	// 冲突方不生效：最终只有 it1
	st := queryState(t, env)
	assert.Equal(t, uint64(1), st.Revision)
	require.Len(t, st.Doc.Items, 1)
	assert.Equal(t, "it1", st.Doc.Items[0].ItemID)
}

func TestTimelineWorkflowDuplicateUpdateIDAppliesOnce(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(TimelineEntityWorkflow)

	var first, replay updateResult
	req := addItemReq(0, "it1", 0)
	sendUpdate(t, env, time.Millisecond, "same-mutation", req, &first)
	sendUpdate(t, env, 2*time.Millisecond, "same-mutation", req, &replay)
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(SignalDeleteTimeline, nil) }, 3*time.Millisecond)

	env.ExecuteWorkflow(TimelineEntityWorkflow, baseInit())
	require.NoError(t, env.GetWorkflowError())
	require.NoError(t, first.err)
	require.NoError(t, replay.err)
	assert.Equal(t, uint64(1), first.res.Revision)
	assert.Equal(t, uint64(1), replay.res.Revision)
	state := queryState(t, env)
	assert.Equal(t, uint64(1), state.Revision)
	require.Len(t, state.Doc.Items, 1)
}

func TestTimelineWorkflowTranscriptDeleteIsAtomicAndIdempotent(t *testing.T) {
	init := transcriptInit()
	view := timeline.BuildTranscriptView(init.ProjectID, init.Revision, init.Doc)
	require.Len(t, view.Entries, 2)
	req := DeleteTranscriptRangesRequest{
		BaseRevision: init.Revision,
		Source:       "user",
		Mode:         "ripple",
		Ranges: []timeline.TranscriptDeleteRange{{
			FromOccurrenceID: view.Entries[0].OccurrenceID,
			ToOccurrenceID:   view.Entries[0].OccurrenceID,
		}},
	}

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(TimelineEntityWorkflow)
	var first, replay transcriptUpdateResult
	sendTranscriptUpdate(t, env, time.Millisecond, "same-transcript-delete", req, &first)
	sendTranscriptUpdate(t, env, 2*time.Millisecond, "same-transcript-delete", req, &replay)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalDeleteTimeline, nil)
	}, 3*time.Millisecond)

	env.ExecuteWorkflow(TimelineEntityWorkflow, init)
	require.NoError(t, env.GetWorkflowError())
	require.NoError(t, first.err)
	require.NoError(t, replay.err)
	assert.Equal(t, uint64(6), first.res.Revision)
	assert.Equal(t, uint64(6), replay.res.Revision)
	assert.Equal(t, []timeline.TranscriptFrameRange{{FromFrame: 0, ToFrame: 15}}, first.res.DeletedRanges)
	assert.Equal(t, int64(15), first.res.DeletedDurationInFrames)
	state := queryState(t, env)
	assert.Equal(t, uint64(6), state.Revision)
	require.Len(t, state.Doc.Items, 1)
	assert.Equal(t, int64(45), state.Doc.Items[0].DurationInFrames)
	assert.Equal(t, int64(15), state.Doc.Items[0].TrimStart)
}

func TestTimelineWorkflowTranscriptDeleteRejectsLockedTrackWithoutMutation(t *testing.T) {
	init := transcriptInit()
	init.Doc.Tracks = append(init.Doc.Tracks, timeline.Track{
		TrackID: "locked", Kind: timeline.TrackAudio, Locked: true,
	})
	init.Doc.Media = append(init.Doc.Media, timeline.Media{
		MediaID: "a1", URI: "blob://a1", Kind: timeline.MediaAudio, DurationInFrames: 300,
	})
	init.Doc.Items = append(init.Doc.Items, timeline.Item{
		ItemID: "locked-item", TrackID: "locked", Kind: timeline.ItemAudio, MediaID: "a1",
		From: 100, DurationInFrames: 20,
	})
	before := init.Doc.Clone()
	view := timeline.BuildTranscriptView(init.ProjectID, init.Revision, init.Doc)
	var result transcriptUpdateResult

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(TimelineEntityWorkflow)
	sendTranscriptUpdate(t, env, time.Millisecond, "locked-delete", DeleteTranscriptRangesRequest{
		BaseRevision: init.Revision,
		Source:       "user",
		Mode:         "ripple",
		Ranges: []timeline.TranscriptDeleteRange{{
			FromOccurrenceID: view.Entries[0].OccurrenceID,
			ToOccurrenceID:   view.Entries[0].OccurrenceID,
		}},
	}, &result)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalDeleteTimeline, nil)
	}, 2*time.Millisecond)

	env.ExecuteWorkflow(TimelineEntityWorkflow, init)
	require.NoError(t, env.GetWorkflowError())
	require.Error(t, result.err)
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, result.err, &appErr)
	assert.Equal(t, ErrTypeTranscriptEdit, appErr.Type())
	var details timeline.TranscriptEditError
	require.NoError(t, appErr.Details(&details))
	assert.Equal(t, timeline.ErrLockedTrackConflict, details.Code)
	state := queryState(t, env)
	assert.Equal(t, init.Revision, state.Revision)
	assert.Equal(t, before, state.Doc)
}

func TestTimelineWorkflowUserAndAgentConcurrentEditsConflict(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(TimelineEntityWorkflow)

	var userResult, agentResult updateResult
	sendUpdate(t, env, time.Millisecond, "user-edit", addItemReq(0, "user-item", 0), &userResult)
	agentRequest := addItemReq(0, "agent-item", 100)
	agentRequest.Source = "agent"
	sendUpdate(t, env, 2*time.Millisecond, "agent-edit", agentRequest, &agentResult)
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(SignalDeleteTimeline, nil) }, 3*time.Millisecond)

	env.ExecuteWorkflow(TimelineEntityWorkflow, baseInit())
	require.NoError(t, env.GetWorkflowError())
	require.NoError(t, userResult.err)
	require.Error(t, agentResult.err)
	var conflict *temporal.ApplicationError
	require.ErrorAs(t, agentResult.err, &conflict)
	assert.Equal(t, ErrTypeRevisionConflict, conflict.Type())
	state := queryState(t, env)
	assert.Equal(t, uint64(1), state.Revision)
	require.Len(t, state.Doc.Items, 1)
	assert.Equal(t, "user-item", state.Doc.Items[0].ItemID)
}

func TestTimelineWorkflowOpFailure(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(TimelineEntityWorkflow)

	var bad updateResult
	// 引用不存在的素材 → TIMELINE_OP_FAILED，revision 不涨
	req := ApplyOpsRequest{BaseRevision: 0, Source: "agent", Operations: []timeline.Operation{{
		Op:   timeline.OpAddItem,
		Item: &timeline.Item{ItemID: "x", TrackID: "v1", Kind: timeline.ItemVideo, MediaID: "nope", From: 0, DurationInFrames: 10},
	}}}
	sendUpdate(t, env, time.Millisecond, "req-bad", req, &bad)
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(SignalDeleteTimeline, nil) }, 2*time.Millisecond)

	env.ExecuteWorkflow(TimelineEntityWorkflow, baseInit())
	require.NoError(t, env.GetWorkflowError())

	require.Error(t, bad.err)
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, bad.err, &appErr)
	assert.Equal(t, ErrTypeOpFailed, appErr.Type())
	var oe timeline.OpError
	require.NoError(t, appErr.Details(&oe))
	assert.Equal(t, timeline.ErrNotFound, oe.Code)

	assert.Equal(t, uint64(0), queryState(t, env).Revision)
}

func TestTimelineWorkflowValidatorRejectsEmptyOps(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(TimelineEntityWorkflow)

	var rejected updateResult
	sendUpdate(t, env, time.Millisecond, "req-empty", ApplyOpsRequest{BaseRevision: 0, Source: "user"}, &rejected)
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(SignalDeleteTimeline, nil) }, 2*time.Millisecond)

	env.ExecuteWorkflow(TimelineEntityWorkflow, baseInit())
	require.NoError(t, env.GetWorkflowError())
	require.Error(t, rejected.err)
	assert.Contains(t, rejected.err.Error(), "empty")
}

func TestTimelineWorkflowSnapshot(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(TimelineEntityWorkflow)

	var mu sync.Mutex
	var snaps []SnapshotRequest
	env.RegisterActivityWithOptions(func(ctx context.Context, req SnapshotRequest) error {
		mu.Lock()
		defer mu.Unlock()
		snaps = append(snaps, req)
		return nil
	}, activity.RegisterOptions{Name: SnapshotActivityName})

	init := baseInit()
	init.DisableSnapshots = false

	var r1 updateResult
	sendUpdate(t, env, time.Millisecond, "req-1", addItemReq(0, "it1", 0), &r1)
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(SignalDeleteTimeline, nil) }, 10*time.Millisecond)

	env.ExecuteWorkflow(TimelineEntityWorkflow, init)
	require.NoError(t, env.GetWorkflowError())
	require.NoError(t, r1.err)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, snaps)
	last := snaps[len(snaps)-1]
	assert.Equal(t, "p1", last.ProjectID)
	assert.Equal(t, uint64(1), last.Revision)
	assert.Len(t, last.Doc.Items, 1)
}

func TestTimelineWorkflowSnapshotFailureDoesNotBreak(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(TimelineEntityWorkflow)
	env.RegisterActivityWithOptions(func(ctx context.Context, req SnapshotRequest) error {
		return errors.New("blobstore down")
	}, activity.RegisterOptions{Name: SnapshotActivityName})

	init := baseInit()
	init.DisableSnapshots = false

	var r1 updateResult
	sendUpdate(t, env, time.Millisecond, "req-1", addItemReq(0, "it1", 0), &r1)
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(SignalDeleteTimeline, nil) }, 10*time.Millisecond)

	env.ExecuteWorkflow(TimelineEntityWorkflow, init)
	require.NoError(t, env.GetWorkflowError())
	require.NoError(t, r1.err)
	assert.Equal(t, uint64(1), queryState(t, env).Revision)
}

func TestTimelineWorkflowSchedulesMediaCleanup(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(TimelineEntityWorkflow)

	var cleaned []DeleteMediaBlobRequest
	env.RegisterActivityWithOptions(func(ctx context.Context, req DeleteMediaBlobRequest) error {
		cleaned = append(cleaned, req)
		return nil
	}, activity.RegisterOptions{Name: DeleteMediaBlobActivityName})

	init := baseInit()
	init.Doc.Media[0].BlobKey = "agentcut/p1/source/m1/video.mp4"
	init.DisableMediaCleanup = false
	var removed updateResult
	sendUpdate(t, env, time.Millisecond, "remove-media", ApplyOpsRequest{
		BaseRevision: 0,
		Source:       "user",
		Operations:   []timeline.Operation{{Op: timeline.OpRemoveMedia, MediaID: "m1"}},
	}, &removed)
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(SignalDeleteTimeline, nil) }, 10*time.Millisecond)

	env.ExecuteWorkflow(TimelineEntityWorkflow, init)
	require.NoError(t, env.GetWorkflowError())
	require.NoError(t, removed.err)
	require.Len(t, cleaned, 1)
	assert.Equal(t, "m1", cleaned[0].MediaID)
	assert.Equal(t, "agentcut/p1/source/m1/video.mp4", cleaned[0].BlobKey)
}

func TestTimelineWorkflowCleansMediaOnProjectDelete(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(TimelineEntityWorkflow)
	var cleaned []DeleteMediaBlobRequest
	env.RegisterActivityWithOptions(func(ctx context.Context, req DeleteMediaBlobRequest) error {
		cleaned = append(cleaned, req)
		return nil
	}, activity.RegisterOptions{Name: DeleteMediaBlobActivityName})

	init := baseInit()
	init.Doc.Media[0].BlobKey = "agentcut/p1/source/m1/video.mp4"
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(SignalDeleteTimeline, nil) }, time.Millisecond)
	env.ExecuteWorkflow(TimelineEntityWorkflow, init)

	require.NoError(t, env.GetWorkflowError())
	require.Len(t, cleaned, 1)
	assert.Equal(t, "m1", cleaned[0].MediaID)
}

func TestTimelineWorkflowContinueAsNew(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(TimelineEntityWorkflow)

	init := baseInit()
	init.MaxUpdatesPerRun = 2

	var r1, r2 updateResult
	sendUpdate(t, env, time.Millisecond, "req-1", addItemReq(0, "it1", 0), &r1)
	sendUpdate(t, env, 2*time.Millisecond, "req-2", addItemReq(1, "it2", 100), &r2)

	env.ExecuteWorkflow(TimelineEntityWorkflow, init)
	require.True(t, env.IsWorkflowCompleted())
	werr := env.GetWorkflowError()
	require.Error(t, werr)
	var canErr *workflow.ContinueAsNewError
	assert.True(t, errors.As(werr, &canErr), "expected ContinueAsNewError, got %v", werr)
	require.NoError(t, r1.err)
	require.NoError(t, r2.err)
}

func TestNonSystemMediaACLRejectsSystemFieldsAndTranscript(t *testing.T) {
	processing := &timeline.MediaProcessing{Status: "processing"}
	require.Error(t, validateNonSystemMediaACL("agent", []timeline.Operation{{
		Op: timeline.OpAddMedia, Media: &timeline.Media{
			MediaID: "m2", URI: "managed", Kind: timeline.MediaImage,
			Processing: processing,
		},
	}}))
	require.Error(t, validateNonSystemMediaACL("agent", []timeline.Operation{{
		Op: timeline.OpSetTranscript, MediaID: "m1",
		Transcript: &timeline.Transcript{},
	}}))
	require.NoError(t, validateNonSystemMediaACL("agent", []timeline.Operation{{
		Op: timeline.OpAddMedia, Media: &timeline.Media{
			MediaID: "m2", URI: "managed", Kind: timeline.MediaImage,
		},
	}, {
		Op: timeline.OpRemoveMedia, MediaID: "m1",
	}}))
	name := "Agent 重命名"
	require.NoError(t, validateNonSystemMediaACL("agent", []timeline.Operation{{
		Op: timeline.OpUpdateMedia, MediaID: "m1",
		MediaPatch: &timeline.MediaPatch{DisplayName: &name},
	}}))
	require.Error(t, validateNonSystemMediaACL("user", []timeline.Operation{{
		Op: timeline.OpAddMedia, Media: &timeline.Media{
			MediaID: "m2", URI: "managed", Kind: timeline.MediaImage,
		},
	}}))
}

func TestTimelineWorkflowAssignsGenerationAndRejectsStalePrepare(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(TimelineEntityWorkflow)

	var removed, added, stale updateResult
	sendUpdate(t, env, time.Millisecond, "remove", ApplyOpsRequest{
		BaseRevision: 0, Source: "agent",
		Operations: []timeline.Operation{{Op: timeline.OpRemoveMedia, MediaID: "m1"}},
	}, &removed)
	sendUpdate(t, env, 2*time.Millisecond, "readd", ApplyOpsRequest{
		BaseRevision: 1, Source: "agent",
		Operations: []timeline.Operation{{Op: timeline.OpAddMedia, Media: &timeline.Media{
			MediaID: "m1", URI: "managed", Kind: timeline.MediaVideo, DurationInFrames: 30,
		}}},
	}, &added)
	legacy := uint64(0)
	name := "stale"
	sendUpdate(t, env, 3*time.Millisecond, "stale-prepare", ApplyOpsRequest{
		BaseRevision: 2, Source: "system",
		Operations: []timeline.Operation{{
			Op: timeline.OpUpdateMedia, MediaID: "m1", MediaGeneration: &legacy,
			MediaPatch: &timeline.MediaPatch{DisplayName: &name},
		}},
	}, &stale)
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(SignalDeleteTimeline, nil) }, 4*time.Millisecond)
	env.ExecuteWorkflow(TimelineEntityWorkflow, baseInit())

	require.NoError(t, env.GetWorkflowError())
	require.NoError(t, removed.err)
	require.NoError(t, added.err)
	assert.Equal(t, uint64(1), added.res.AddedMediaGenerations["m1"])
	require.Error(t, stale.err)
	state := queryState(t, env)
	require.Len(t, state.Doc.Media, 1)
	assert.Equal(t, uint64(1), state.Doc.Media[0].Generation)
	assert.Empty(t, state.Doc.Media[0].DisplayName)
}

func TestTimelineWorkflowRetriesFailedCleanupWithoutDroppingIt(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(TimelineEntityWorkflow)
	calls := 0
	env.RegisterActivityWithOptions(func(context.Context, DeleteMediaBlobRequest) error {
		calls++
		if calls <= 3 {
			return errors.New("blob unavailable")
		}
		return nil
	}, activity.RegisterOptions{Name: DeleteMediaBlobActivityName})

	init := baseInit()
	init.Doc.Media[0].BlobKey = "agentcut/p1/source/m1/video.mp4"
	var removed updateResult
	sendUpdate(t, env, time.Millisecond, "remove", ApplyOpsRequest{
		BaseRevision: 0, Source: "user",
		Operations: []timeline.Operation{{Op: timeline.OpRemoveMedia, MediaID: "m1"}},
	}, &removed)
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(SignalDeleteTimeline, nil) }, 2*time.Millisecond)
	env.ExecuteWorkflow(TimelineEntityWorkflow, init)

	require.NoError(t, env.GetWorkflowError())
	require.NoError(t, removed.err)
	assert.Equal(t, 4, calls, "failed request must remain queued after activity retries")
}

func TestTimelineWorkflowCarriesFailedCleanupAcrossContinueAsNew(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(TimelineEntityWorkflow)
	env.RegisterActivityWithOptions(func(context.Context, DeleteMediaBlobRequest) error {
		return errors.New("still unavailable")
	}, activity.RegisterOptions{Name: DeleteMediaBlobActivityName})

	init := baseInit()
	init.MaxUpdatesPerRun = 1
	init.PendingDeletes = []DeleteMediaBlobRequest{{
		ProjectID: "p1", MediaID: "old", BlobKey: "agentcut/p1/source/old/a.mp4", Generation: 2,
	}}
	var updated updateResult
	sendUpdate(t, env, time.Millisecond, "update", addItemReq(0, "it1", 0), &updated)
	env.ExecuteWorkflow(TimelineEntityWorkflow, init)

	var canErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &canErr)
	var next TimelineInit
	require.NoError(t, converter.GetDefaultDataConverter().FromPayloads(canErr.Input, &next))
	require.Len(t, next.PendingDeletes, 1)
	assert.Equal(t, uint64(2), next.PendingDeletes[0].Generation)
}
