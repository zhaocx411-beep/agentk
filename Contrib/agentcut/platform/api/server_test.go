package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	apiv1 "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1/apiv1connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"google.golang.org/protobuf/types/known/timestamppb"

	"agentcut/entity"
	"agentcut/platform/agentkclient"
	"agentcut/platform/blob"
	"agentcut/preparemedia"
	"agentcut/render"
	"agentcut/separateaudio"
	"agentcut/timeline"
)

// ---- 假 Temporal：在内存里模拟两个实体 Workflow 的行为 ----

type fakeRun struct {
	id     string
	result any
}

func (r *fakeRun) GetID() string    { return r.id }
func (r *fakeRun) GetRunID() string { return "run" }
func (r *fakeRun) Get(ctx context.Context, valuePtr any) error {
	if valuePtr == nil || r.result == nil {
		return nil
	}
	b, _ := json.Marshal(r.result)
	return json.Unmarshal(b, valuePtr)
}
func (r *fakeRun) GetWithOptions(ctx context.Context, valuePtr any, options client.WorkflowRunGetOptions) error {
	return r.Get(ctx, valuePtr)
}

type fakeUpdateHandle struct {
	result any
	err    error
}

func (h *fakeUpdateHandle) WorkflowID() string { return "" }
func (h *fakeUpdateHandle) RunID() string      { return "" }
func (h *fakeUpdateHandle) UpdateID() string   { return "" }
func (h *fakeUpdateHandle) Get(ctx context.Context, valuePtr any) error {
	if h.err != nil {
		return h.err
	}
	if valuePtr == nil || h.result == nil {
		return nil
	}
	b, _ := json.Marshal(h.result)
	return json.Unmarshal(b, valuePtr)
}

type fakeEncoded struct{ v any }

func (f fakeEncoded) HasValue() bool { return true }
func (f fakeEncoded) Get(valuePtr any) error {
	b, err := json.Marshal(f.v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, valuePtr)
}

// fakeRender 模拟一次渲染 Workflow 执行。
type fakeRender struct {
	input  render.Input
	status enums.WorkflowExecutionStatus
	output render.Output
	starts int
}

type fakeSeparation struct {
	input  separateaudio.Input
	status enums.WorkflowExecutionStatus
	output separateaudio.Output
	err    error
	starts int
}

type fakeTemporal struct {
	timelines    map[string]*entity.TimelineState // key: workflow ID
	projects     map[string][]entity.ProjectRef   // key: workflow ID
	renders      map[string]*fakeRender           // key: workflow ID
	separations  map[string]*fakeSeparation       // key: workflow ID
	shares       map[string]map[string]int64      // workflow ID → token hash → expiry
	prepares     map[string]int                   // workflow ID → start count
	seenIDs      map[string]bool                  // Update 幂等
	applyResults map[string]*fakeUpdateHandle     // workflow ID + UpdateID → first result
	generations  map[string]map[string]uint64

	failAddProject       error // 注入 addProject 失败，测试补偿路径
	applyConflicts       int   // 注入指定次数的 applyOps revision conflict
	failTimelineQueries  bool  // 注入 Timeline Query 失败
	failApplyAfterCommit bool  // 注入提交成功但客户端收到失败
}

func newFakeTemporal() *fakeTemporal {
	return &fakeTemporal{
		timelines:    map[string]*entity.TimelineState{},
		projects:     map[string][]entity.ProjectRef{},
		renders:      map[string]*fakeRender{},
		separations:  map[string]*fakeSeparation{},
		shares:       map[string]map[string]int64{},
		prepares:     map[string]int{},
		seenIDs:      map[string]bool{},
		applyResults: map[string]*fakeUpdateHandle{},
		generations:  map[string]map[string]uint64{},
	}
}

func syncFakeActiveSequence(st *entity.TimelineState) {
	for index := range st.Timelines {
		if st.Timelines[index].TimelineID != st.ActiveTimelineID {
			continue
		}
		st.Timelines[index].Doc = st.Doc.Clone()
		st.Timelines[index].Revision = st.Revision
		return
	}
}

func (f *fakeTemporal) ExecuteWorkflow(ctx context.Context, options client.StartWorkflowOptions, wf any, args ...any) (client.WorkflowRun, error) {
	switch wf {
	case entity.TimelineWorkflowName:
		if _, ok := f.timelines[options.ID]; !ok {
			init := args[0].(entity.TimelineInit)
			state := entity.NewTimelineState(init, time.Now().UnixMilli())
			f.timelines[options.ID] = &state
			f.generations[options.ID] = map[string]uint64{}
			f.shares[options.ID] = map[string]int64{}
		}
	case entity.UserProjectsWorkflowName:
		if _, ok := f.projects[options.ID]; !ok {
			f.projects[options.ID] = []entity.ProjectRef{}
		}
	case render.WorkflowName:
		if existing, ok := f.renders[options.ID]; ok {
			if existing.status == enums.WORKFLOW_EXECUTION_STATUS_FAILED &&
				options.WorkflowIDReusePolicy == enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY {
				existing.input = args[0].(render.Input)
				existing.status = enums.WORKFLOW_EXECUTION_STATUS_RUNNING
				existing.output = render.Output{}
				existing.starts++
				return &fakeRun{id: options.ID}, nil
			}
			if existing.status != enums.WORKFLOW_EXECUTION_STATUS_RUNNING {
				return nil, serviceerror.NewWorkflowExecutionAlreadyStarted("already started", "", "run")
			}
			// 运行中：默认不报错，返回现有执行
			return &fakeRun{id: options.ID}, nil
		}
		f.renders[options.ID] = &fakeRender{
			input:  args[0].(render.Input),
			status: enums.WORKFLOW_EXECUTION_STATUS_RUNNING,
			starts: 1,
		}
	case preparemedia.WorkflowName:
		f.prepares[options.ID]++
	case separateaudio.WorkflowName:
		if existing, ok := f.separations[options.ID]; ok {
			if existing.status == enums.WORKFLOW_EXECUTION_STATUS_FAILED {
				existing.input = args[0].(separateaudio.Input)
				existing.status = enums.WORKFLOW_EXECUTION_STATUS_RUNNING
				existing.starts++
				return &fakeRun{id: options.ID}, nil
			}
			return nil, serviceerror.NewWorkflowExecutionAlreadyStarted("already started", "", "run")
		}
		f.separations[options.ID] = &fakeSeparation{
			input: args[0].(separateaudio.Input), status: enums.WORKFLOW_EXECUTION_STATUS_RUNNING,
			starts: 1,
		}
	}
	return &fakeRun{id: options.ID}, nil
}

func (f *fakeTemporal) DescribeWorkflowExecution(ctx context.Context, workflowID, runID string) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
	r, ok := f.renders[workflowID]
	if ok {
		return &workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{Status: r.status},
		}, nil
	}
	if separation, exists := f.separations[workflowID]; exists {
		return &workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{Status: separation.status},
		}, nil
	}
	return nil, serviceerror.NewNotFound("workflow not found")
}

func (f *fakeTemporal) GetWorkflow(ctx context.Context, workflowID, runID string) client.WorkflowRun {
	if r, ok := f.renders[workflowID]; ok {
		return &fakeRun{id: workflowID, result: r.output}
	}
	if separation, ok := f.separations[workflowID]; ok {
		if separation.err != nil {
			return &fakeRun{id: workflowID, result: separation.err}
		}
		return &fakeRun{id: workflowID, result: separation.output}
	}
	return &fakeRun{id: workflowID}
}

func (f *fakeTemporal) UpdateWorkflow(ctx context.Context, options client.UpdateWorkflowOptions) (client.WorkflowUpdateHandle, error) {
	switch options.UpdateName {
	case entity.UpdateBindAgentKSession:
		st, ok := f.timelines[options.WorkflowID]
		if !ok {
			return nil, serviceerror.NewNotFound("workflow not found")
		}
		req := options.Args[0].(entity.BindAgentKSessionRequest)
		if st.AgentKSessionID != "" && st.AgentKSessionID != req.SessionID {
			return &fakeUpdateHandle{err: errors.New("timeline bound to another session")}, nil
		}
		st.AgentKSessionID = req.SessionID
		return &fakeUpdateHandle{}, nil
	case entity.UpdateCreateTimeline:
		st, ok := f.timelines[options.WorkflowID]
		if !ok {
			return nil, serviceerror.NewNotFound("workflow not found")
		}
		req := options.Args[0].(entity.CreateTimelineRequest)
		syncFakeActiveSequence(st)
		doc := st.Doc.Clone()
		doc.Items = []timeline.Item{}
		doc.Transitions = []timeline.Transition{}
		doc.Tracks = []timeline.Track{{
			TrackID: req.BaseVideoTrackID, Kind: timeline.TrackVideo, Name: "视频轨",
			CreatedBy: timeline.TrackCreatedAuto,
		}}
		st.Timelines = append(st.Timelines, entity.TimelineSequence{
			TimelineID: req.TimelineID, Name: req.Name, Doc: doc, Zoom: 1,
			CreatedAtUnixMs: req.CreatedAtUnixMs, UpdatedAtUnixMs: req.CreatedAtUnixMs,
		})
		st.ActiveTimelineID = req.TimelineID
		st.Doc = doc
		st.Revision = 0
		st.ProjectRevision++
		return &fakeUpdateHandle{result: *st}, nil
	case entity.UpdateSwitchTimeline:
		st, ok := f.timelines[options.WorkflowID]
		if !ok {
			return nil, serviceerror.NewNotFound("workflow not found")
		}
		req := options.Args[0].(entity.SwitchTimelineRequest)
		syncFakeActiveSequence(st)
		for _, sequence := range st.Timelines {
			if sequence.TimelineID != req.TimelineID {
				continue
			}
			st.ActiveTimelineID = sequence.TimelineID
			st.Doc = sequence.Doc.Clone()
			st.Revision = sequence.Revision
			st.ProjectRevision++
			return &fakeUpdateHandle{result: *st}, nil
		}
		return &fakeUpdateHandle{err: errors.New("timeline not found")}, nil
	case entity.UpdateRenameTimeline:
		st, ok := f.timelines[options.WorkflowID]
		if !ok {
			return nil, serviceerror.NewNotFound("workflow not found")
		}
		req := options.Args[0].(entity.RenameTimelineRequest)
		for index := range st.Timelines {
			if st.Timelines[index].TimelineID != req.TimelineID {
				continue
			}
			st.Timelines[index].Name = req.Name
			st.Timelines[index].UpdatedAtUnixMs = req.UpdatedAtUnixMs
			st.ProjectRevision++
			return &fakeUpdateHandle{result: *st}, nil
		}
		return &fakeUpdateHandle{err: errors.New("timeline not found")}, nil
	case entity.UpdateDeleteTimeline:
		st, ok := f.timelines[options.WorkflowID]
		if !ok {
			return nil, serviceerror.NewNotFound("workflow not found")
		}
		if len(st.Timelines) <= 1 {
			return &fakeUpdateHandle{err: errors.New("the last timeline cannot be deleted")}, nil
		}
		req := options.Args[0].(entity.DeleteTimelineRequest)
		for index := range st.Timelines {
			if st.Timelines[index].TimelineID != req.TimelineID {
				continue
			}
			wasActive := st.ActiveTimelineID == req.TimelineID
			st.Timelines = append(st.Timelines[:index], st.Timelines[index+1:]...)
			if wasActive {
				target := st.Timelines[min(index, len(st.Timelines)-1)]
				st.ActiveTimelineID = target.TimelineID
				st.Doc = target.Doc.Clone()
				st.Revision = target.Revision
			}
			st.ProjectRevision++
			return &fakeUpdateHandle{result: *st}, nil
		}
		return &fakeUpdateHandle{err: errors.New("timeline not found")}, nil
	case entity.UpdateTimelineView:
		st, ok := f.timelines[options.WorkflowID]
		if !ok {
			return nil, serviceerror.NewNotFound("workflow not found")
		}
		req := options.Args[0].(entity.UpdateTimelineViewRequest)
		for index := range st.Timelines {
			if st.Timelines[index].TimelineID != req.TimelineID {
				continue
			}
			st.Timelines[index].CurrentFrame = req.CurrentFrame
			st.Timelines[index].Zoom = req.Zoom
			st.Timelines[index].UpdatedAtUnixMs = req.UpdatedAtUnixMs
			st.ProjectRevision++
			return &fakeUpdateHandle{result: st.TimelineSummaries()[index]}, nil
		}
		return &fakeUpdateHandle{err: errors.New("timeline not found")}, nil
	case entity.UpdateApplyOps:
		st, ok := f.timelines[options.WorkflowID]
		if !ok {
			return nil, serviceerror.NewNotFound("workflow not found")
		}
		idempotencyKey := options.WorkflowID + "\x00" + options.UpdateID
		if previous := f.applyResults[idempotencyKey]; previous != nil {
			return previous, nil
		}
		if f.applyConflicts > 0 {
			f.applyConflicts--
			result := &fakeUpdateHandle{err: temporal.NewApplicationErrorWithOptions(
				"conflict", entity.ErrTypeRevisionConflict,
				temporal.ApplicationErrorOptions{Details: []any{entity.RevisionConflictDetails{LatestRevision: st.Revision}}},
			)}
			f.applyResults[idempotencyKey] = result
			return result, nil
		}
		req := options.Args[0].(entity.ApplyOpsRequest)
		if req.BaseRevision != st.Revision {
			result := &fakeUpdateHandle{err: temporal.NewApplicationErrorWithOptions(
				"conflict", entity.ErrTypeRevisionConflict,
				temporal.ApplicationErrorOptions{Details: []any{entity.RevisionConflictDetails{LatestRevision: st.Revision}}},
			)}
			f.applyResults[idempotencyKey] = result
			return result, nil
		}
		ops := append([]timeline.Operation(nil), req.Operations...)
		addedGenerations := map[string]uint64{}
		for i := range ops {
			if ops[i].Op == timeline.OpAddMedia && ops[i].Media != nil {
				media := *ops[i].Media
				f.generations[options.WorkflowID][media.MediaID]++
				media.Generation = f.generations[options.WorkflowID][media.MediaID]
				addedGenerations[media.MediaID] = media.Generation
				ops[i].Media = &media
			}
		}
		next, sum, err := timeline.ApplyOps(st.Doc, ops)
		if err != nil {
			oe := err.(*timeline.OpError)
			result := &fakeUpdateHandle{err: temporal.NewApplicationErrorWithOptions(
				oe.Error(), entity.ErrTypeOpFailed,
				temporal.ApplicationErrorOptions{Details: []any{*oe}},
			)}
			f.applyResults[idempotencyKey] = result
			return result, nil
		}
		next, removedAutoTracks := timeline.CleanupEmptyAutoTracks(next)
		sum.RemovedTrackIDs = append(sum.RemovedTrackIDs, removedAutoTracks...)
		st.Doc = next
		st.Revision++
		syncFakeActiveSequence(st)
		if f.failApplyAfterCommit {
			f.failApplyAfterCommit = false
			return &fakeUpdateHandle{err: errors.New("uncertain update result")}, nil
		}
		result := &fakeUpdateHandle{result: entity.ApplyOpsResult{
			Revision: st.Revision, Summary: sum, AddedMediaGenerations: addedGenerations,
		}}
		f.applyResults[idempotencyKey] = result
		return result, nil
	case entity.UpdateDeleteTranscript:
		st, ok := f.timelines[options.WorkflowID]
		if !ok {
			return nil, serviceerror.NewNotFound("workflow not found")
		}
		idempotencyKey := options.WorkflowID + "\x00" + options.UpdateID
		if previous := f.applyResults[idempotencyKey]; previous != nil {
			return previous, nil
		}
		req := options.Args[0].(entity.DeleteTranscriptRangesRequest)
		if f.applyConflicts > 0 || req.BaseRevision != st.Revision {
			if f.applyConflicts > 0 {
				f.applyConflicts--
			}
			result := &fakeUpdateHandle{err: temporal.NewApplicationErrorWithOptions(
				"conflict", entity.ErrTypeRevisionConflict,
				temporal.ApplicationErrorOptions{
					Details: []any{entity.RevisionConflictDetails{LatestRevision: st.Revision}},
				},
			)}
			f.applyResults[idempotencyKey] = result
			return result, nil
		}
		compiled, editErr := timeline.CompileTranscriptDelete(
			st.ProjectID, st.Revision, st.Doc, req.Ranges,
		)
		if editErr != nil {
			result := &fakeUpdateHandle{err: temporal.NewApplicationErrorWithOptions(
				editErr.Error(), entity.ErrTypeTranscriptEdit,
				temporal.ApplicationErrorOptions{Details: []any{*editErr}},
			)}
			f.applyResults[idempotencyKey] = result
			return result, nil
		}
		next, summary, applyErr := timeline.ApplyOps(st.Doc, compiled.Operations)
		if applyErr != nil {
			opErr := applyErr.(*timeline.OpError)
			result := &fakeUpdateHandle{err: temporal.NewApplicationErrorWithOptions(
				opErr.Error(), entity.ErrTypeOpFailed,
				temporal.ApplicationErrorOptions{Details: []any{*opErr}},
			)}
			f.applyResults[idempotencyKey] = result
			return result, nil
		}
		next, removedAutoTracks := timeline.CleanupEmptyAutoTracks(next)
		summary.RemovedTrackIDs = append(summary.RemovedTrackIDs, removedAutoTracks...)
		st.Doc = next
		st.Revision++
		syncFakeActiveSequence(st)
		result := &fakeUpdateHandle{result: entity.DeleteTranscriptRangesResult{
			Revision:                st.Revision,
			Doc:                     st.Doc,
			Summary:                 summary,
			DeletedRanges:           compiled.DeletedRanges,
			DeletedDurationInFrames: compiled.DeletedDurationInFrames,
		}}
		f.applyResults[idempotencyKey] = result
		return result, nil
	case entity.UpdateRestoreTimeline:
		st, ok := f.timelines[options.WorkflowID]
		if !ok {
			return nil, serviceerror.NewNotFound("workflow not found")
		}
		idempotencyKey := options.WorkflowID + "\x00" + options.UpdateID
		if previous := f.applyResults[idempotencyKey]; previous != nil {
			return previous, nil
		}
		req := options.Args[0].(entity.RestoreTimelineRequest)
		if f.applyConflicts > 0 || req.BaseRevision != st.Revision {
			if f.applyConflicts > 0 {
				f.applyConflicts--
			}
			result := &fakeUpdateHandle{err: temporal.NewApplicationErrorWithOptions(
				"conflict", entity.ErrTypeRevisionConflict,
				temporal.ApplicationErrorOptions{
					Details: []any{entity.RevisionConflictDetails{LatestRevision: st.Revision}},
				},
			)}
			f.applyResults[idempotencyKey] = result
			return result, nil
		}
		st.Doc = req.Doc.Clone()
		st.Revision++
		syncFakeActiveSequence(st)
		result := &fakeUpdateHandle{result: entity.RestoreTimelineResult{
			Revision: st.Revision, Doc: st.Doc,
		}}
		f.applyResults[idempotencyKey] = result
		return result, nil
	case entity.UpdateAddProject:
		if f.failAddProject != nil {
			return nil, f.failAddProject
		}
		req := options.Args[0].(entity.AddProjectRequest)
		refs := f.projects[options.WorkflowID]
		for _, p := range refs {
			if p.ProjectID == req.Ref.ProjectID {
				return &fakeUpdateHandle{result: p}, nil
			}
		}
		f.projects[options.WorkflowID] = append(refs, req.Ref)
		return &fakeUpdateHandle{result: req.Ref}, nil
	case entity.UpdateRenameProject:
		req := options.Args[0].(entity.RenameProjectRequest)
		refs := f.projects[options.WorkflowID]
		for i := range refs {
			if refs[i].ProjectID == req.ProjectID {
				refs[i].Title = req.Title
				f.projects[options.WorkflowID] = refs
				return &fakeUpdateHandle{result: refs[i]}, nil
			}
		}
		return &fakeUpdateHandle{err: errors.New("project not found")}, nil
	case entity.UpdateRemoveProject:
		req := options.Args[0].(entity.RemoveProjectRequest)
		refs := f.projects[options.WorkflowID]
		kept := refs[:0]
		for _, p := range refs {
			if p.ProjectID != req.ProjectID {
				kept = append(kept, p)
			}
		}
		f.projects[options.WorkflowID] = kept
		return &fakeUpdateHandle{}, nil
	case entity.UpdateCreateShare:
		req := options.Args[0].(entity.CreateShareRequest)
		if f.shares[options.WorkflowID] == nil {
			f.shares[options.WorkflowID] = map[string]int64{}
		}
		f.shares[options.WorkflowID][req.TokenHash] = req.ExpiresAtUnixMs
		return &fakeUpdateHandle{}, nil
	case entity.UpdateRevokeShare:
		req := options.Args[0].(entity.RevokeShareRequest)
		delete(f.shares[options.WorkflowID], req.TokenHash)
		return &fakeUpdateHandle{}, nil
	}
	return nil, serviceerror.NewNotFound("unknown update")
}

func (f *fakeTemporal) QueryWorkflow(ctx context.Context, workflowID, runID, queryType string, args ...any) (converter.EncodedValue, error) {
	switch queryType {
	case entity.QueryGetState:
		if f.failTimelineQueries {
			return nil, errors.New("timeline unavailable")
		}
		st, ok := f.timelines[workflowID]
		if !ok {
			return nil, serviceerror.NewNotFound("workflow not found")
		}
		return fakeEncoded{v: st}, nil
	case entity.QueryListProjects:
		refs, ok := f.projects[workflowID]
		if !ok {
			return nil, serviceerror.NewNotFound("workflow not found")
		}
		return fakeEncoded{v: refs}, nil
	case entity.QuerySharedTimeline:
		st, ok := f.timelines[workflowID]
		if !ok {
			return nil, serviceerror.NewNotFound("workflow not found")
		}
		req := args[0].(entity.SharedTimelineRequest)
		expiry, exists := f.shares[workflowID][req.TokenHash]
		if !exists {
			return fakeEncoded{v: entity.SharedTimelineResult{Status: "not_found"}}, nil
		}
		if req.NowUnixMs >= expiry {
			return fakeEncoded{v: entity.SharedTimelineResult{Status: "expired"}}, nil
		}
		return fakeEncoded{v: entity.SharedTimelineResult{Status: "valid", State: *st}}, nil
	case render.QueryProgress:
		if r, ok := f.renders[workflowID]; ok {
			return fakeEncoded{v: render.Progress{Done: 3, Total: len(r.input.Doc.Items), Current: "audiomix"}}, nil
		}
		return nil, serviceerror.NewNotFound("workflow not found")
	}
	return nil, serviceerror.NewNotFound("unknown query")
}

func (f *fakeTemporal) SignalWorkflow(ctx context.Context, workflowID, runID, signalName string, arg any) error {
	if signalName == entity.SignalDeleteTimeline {
		delete(f.timelines, workflowID)
	}
	return nil
}

// ---- 测试 ----

func newTestServer() (*Server, *fakeTemporal) {
	ft := newFakeTemporal()
	return &Server{Temporal: ft, TaskQueue: "test"}, ft
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func createProject(t *testing.T, h http.Handler) string {
	t.Helper()
	rec := doJSON(t, h, "POST", "/v1/projects", map[string]any{"title": "测试"})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var ref entity.ProjectRef
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &ref))
	return ref.ProjectID
}

func TestProjectLifecycle(t *testing.T) {
	s, _ := newTestServer()
	h := s.Handler()

	// 无项目时列表为空（用户索引 Workflow 不存在也不报错）
	rec := doJSON(t, h, "GET", "/v1/projects", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"projects":[]}`, rec.Body.String())

	projectID := createProject(t, h)

	rec = doJSON(t, h, "GET", "/v1/projects", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var list struct {
		Projects []entity.ProjectRef `json:"projects"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list.Projects, 1)
	assert.Equal(t, projectID, list.Projects[0].ProjectID)

	// 重命名同时更新项目索引和 Timeline metadata.name。
	rec = doJSON(t, h, "PATCH", "/v1/projects/"+projectID, map[string]any{
		"title": "新项目名", "requestId": "rename-1",
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"title":"新项目名"`)
	rec = doJSON(t, h, "GET", "/v1/projects/"+projectID+"/timeline", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"name":"新项目名"`)

	// 删除
	rec = doJSON(t, h, "DELETE", "/v1/projects/"+projectID, nil)
	require.Equal(t, http.StatusNoContent, rec.Code)
	rec = doJSON(t, h, "GET", "/v1/projects/"+projectID+"/timeline", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestTimelineSequenceLifecycle(t *testing.T) {
	s, _ := newTestServer()
	h := s.Handler()
	projectID := createProject(t, h)

	readSnapshot := func(rec *httptest.ResponseRecorder) struct {
		ActiveTimelineID string                   `json:"activeTimelineId"`
		Timelines        []entity.TimelineSummary `json:"timelines"`
		Doc              timeline.Doc             `json:"doc"`
		Revision         uint64                   `json:"revision"`
	} {
		t.Helper()
		var snapshot struct {
			ActiveTimelineID string                   `json:"activeTimelineId"`
			Timelines        []entity.TimelineSummary `json:"timelines"`
			Doc              timeline.Doc             `json:"doc"`
			Revision         uint64                   `json:"revision"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &snapshot))
		return snapshot
	}

	rec := doJSON(t, h, http.MethodGet, "/v1/projects/"+projectID+"/timeline", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	first := readSnapshot(rec)
	require.Len(t, first.Timelines, 1)
	assert.Equal(t, "剪辑 1", first.Timelines[0].Name)
	require.Len(t, first.Doc.Tracks, 1)

	rec = doJSON(t, h, http.MethodPost, "/v1/projects/"+projectID+"/timeline/ops", map[string]any{
		"clientMutationId": "add-user-track",
		"timelineId":       first.ActiveTimelineID,
		"baseRevision":     first.Revision,
		"operations": []any{map[string]any{
			"op": "addTrack",
			"track": map[string]any{
				"trackId": "manual-audio", "kind": "audio", "createdBy": "user",
			},
		}},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = doJSON(t, h, http.MethodPost, "/v1/projects/"+projectID+"/timelines", map[string]any{
		"requestId": "create-second", "name": "剪辑 2",
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	second := readSnapshot(rec)
	require.Len(t, second.Timelines, 2)
	assert.NotEqual(t, first.ActiveTimelineID, second.ActiveTimelineID)
	require.Len(t, second.Doc.Tracks, 1)
	assert.Empty(t, second.Doc.Items)

	rec = doJSON(t, h, http.MethodPatch,
		"/v1/projects/"+projectID+"/timelines/"+second.ActiveTimelineID,
		map[string]any{"requestId": "rename-second", "name": "成片版"},
	)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	renamed := readSnapshot(rec)
	assert.Equal(t, "成片版", renamed.Timelines[1].Name)

	rec = doJSON(t, h, http.MethodPatch,
		"/v1/projects/"+projectID+"/timelines/"+second.ActiveTimelineID+"/view",
		map[string]any{"requestId": "view-second", "currentFrame": 42, "zoom": 1.75},
	)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = doJSON(t, h, http.MethodPost,
		"/v1/projects/"+projectID+"/timelines/"+first.ActiveTimelineID+"/activate",
		map[string]any{"requestId": "switch-first"},
	)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	switched := readSnapshot(rec)
	assert.Equal(t, first.ActiveTimelineID, switched.ActiveTimelineID)
	require.Len(t, switched.Doc.Tracks, 2)
	assert.Equal(t, int64(42), switched.Timelines[1].CurrentFrame)
	assert.Equal(t, 1.75, switched.Timelines[1].Zoom)

	rec = doJSON(t, h, http.MethodDelete,
		"/v1/projects/"+projectID+"/timelines/"+first.ActiveTimelineID,
		map[string]any{"requestId": "delete-first"},
	)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	afterDelete := readSnapshot(rec)
	require.Len(t, afterDelete.Timelines, 1)
	assert.Equal(t, second.ActiveTimelineID, afterDelete.ActiveTimelineID)

	rec = doJSON(t, h, http.MethodDelete,
		"/v1/projects/"+projectID+"/timelines/"+second.ActiveTimelineID,
		map[string]any{"requestId": "delete-last"},
	)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestProfilesAndProjectProfileSelection(t *testing.T) {
	s, ft := newTestServer()
	s.Profiles = []ProfileDefinition{{
		ProfileID: "agentcut", Name: "剪辑 Agent", Description: "默认剪辑能力",
		ProfileJSON: []byte(`{"system":"x"}`), IsDefault: true,
	}}
	h := s.Handler()

	rec := doJSON(t, h, http.MethodGet, "/v1/profiles", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"profileId":"agentcut"`)
	assert.NotContains(t, rec.Body.String(), `"system"`, "profile JSON must not be exposed")

	rec = doJSON(t, h, http.MethodPost, "/v1/projects", map[string]any{
		"title": "指定 Profile", "profileId": "agentcut",
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var ref entity.ProjectRef
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &ref))
	assert.Equal(t, "agentcut", ref.ProfileID)

	beforeTimelines := len(ft.timelines)
	rec = doJSON(t, h, http.MethodPost, "/v1/projects", map[string]any{
		"title": "未知 Profile", "profileId": "missing",
	})
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "PROFILE_NOT_FOUND")
	assert.Len(t, ft.timelines, beforeTimelines, "invalid profile must fail before starting workflow")
}

func TestProjectShareLifecycleAndPublicRead(t *testing.T) {
	s, ft := newTestServer()
	s.Auth = AuthConfig{Mode: AuthModeTrustedHeader, UserHeader: "X-Verified-User"}
	h := s.Handler()

	doAuthed := func(method, path string, body any) *httptest.ResponseRecorder {
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Verified-User", "owner")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := doAuthed(http.MethodPost, "/v1/projects", map[string]any{"title": "可分享"})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var project entity.ProjectRef
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &project))

	rec = doAuthed(http.MethodPost, "/v1/projects/"+project.ProjectID+"/shares", map[string]any{})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var share struct {
		Token string `json:"token"`
		URL   string `json:"url"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &share))
	require.NotEmpty(t, share.Token)
	assert.Equal(t, "/v1/shared/"+share.Token+"/timeline", share.URL)

	// 公开读取不带可信用户头仍然成功，且只暴露只读快照。
	publicReq := httptest.NewRequest(http.MethodGet, share.URL, nil)
	publicRec := httptest.NewRecorder()
	h.ServeHTTP(publicRec, publicReq)
	require.Equal(t, http.StatusOK, publicRec.Code, publicRec.Body.String())
	assert.Contains(t, publicRec.Body.String(), `"readOnly":true`)

	// 公开命名空间没有写路由。
	writeReq := httptest.NewRequest(http.MethodPost,
		"/v1/shared/"+share.Token+"/timeline/ops", strings.NewReader(`{}`))
	writeRec := httptest.NewRecorder()
	h.ServeHTTP(writeRec, writeReq)
	assert.Equal(t, http.StatusNotFound, writeRec.Code)

	rec = doAuthed(http.MethodDelete,
		"/v1/projects/"+project.ProjectID+"/shares/"+share.Token, nil)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	publicRec = httptest.NewRecorder()
	h.ServeHTTP(publicRec, httptest.NewRequest(http.MethodGet, share.URL, nil))
	assert.Equal(t, http.StatusNotFound, publicRec.Code)

	// 新链接被标记过期时返回 410，而不是泄露项目内容。
	rec = doAuthed(http.MethodPost, "/v1/projects/"+project.ProjectID+"/share", nil)
	require.Equal(t, http.StatusCreated, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &share))
	ft.shares[entity.TimelineWorkflowID(project.ProjectID)][shareTokenHash(share.Token)] =
		time.Now().Add(-time.Minute).UnixMilli()
	publicRec = httptest.NewRecorder()
	h.ServeHTTP(publicRec, httptest.NewRequest(http.MethodGet, share.URL, nil))
	assert.Equal(t, http.StatusGone, publicRec.Code)
	assert.Contains(t, publicRec.Body.String(), "SHARE_EXPIRED")
}

func TestRenameProjectValidationAndOwnership(t *testing.T) {
	s, _ := newTestServer()
	h := s.Handler()
	projectID := createProject(t, h)

	for name, body := range map[string]any{
		"missing request id": map[string]any{"title": "新名字"},
		"blank title":        map[string]any{"title": "   ", "requestId": "r1"},
		"too long title":     map[string]any{"title": strings.Repeat("名", 121), "requestId": "r2"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := doJSON(t, h, "PATCH", "/v1/projects/"+projectID, body)
			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		})
	}

	req := httptest.NewRequest("PATCH", "/v1/projects/"+projectID,
		strings.NewReader(`{"title":"越权","requestId":"r3"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AgentCut-User", "other")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRenameProjectRetriesTimelineConflict(t *testing.T) {
	s, ft := newTestServer()
	h := s.Handler()
	projectID := createProject(t, h)
	ft.applyConflicts = 1

	rec := doJSON(t, h, "PATCH", "/v1/projects/"+projectID, map[string]any{
		"title": "冲突后成功", "requestId": "rename-conflict",
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "冲突后成功", ft.timelines[entity.TimelineWorkflowID(projectID)].Doc.Metadata.Name)

	// 同一个幂等键重放必须保持成功和相同最终状态。
	rec = doJSON(t, h, "PATCH", "/v1/projects/"+projectID, map[string]any{
		"title": "冲突后成功", "requestId": "rename-conflict",
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "冲突后成功", ft.timelines[entity.TimelineWorkflowID(projectID)].Doc.Metadata.Name)
}

func TestRenameProjectReportsPartialSyncFailure(t *testing.T) {
	s, ft := newTestServer()
	h := s.Handler()
	projectID := createProject(t, h)
	ft.failTimelineQueries = true

	rec := doJSON(t, h, "PATCH", "/v1/projects/"+projectID, map[string]any{
		"title": "索引已更新", "requestId": "rename-partial",
	})
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "timeline title sync failed")
	require.Len(t, ft.projects[entity.UserProjectsWorkflowID("dev")], 1)
	assert.Equal(t, "索引已更新", ft.projects[entity.UserProjectsWorkflowID("dev")][0].Title)
}

func TestTimelineOpsFlow(t *testing.T) {
	s, ft := newTestServer()
	h := s.Handler()
	projectID := createProject(t, h)

	// 初始 timeline
	rec := doJSON(t, h, "GET", "/v1/projects/"+projectID+"/timeline", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var tl struct {
		Revision uint64       `json:"revision"`
		Doc      timeline.Doc `json:"doc"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &tl))
	assert.Equal(t, uint64(0), tl.Revision)
	assert.Equal(t, 30, tl.Doc.Metadata.FPS)
	etag := rec.Header().Get("ETag")
	assert.Regexp(t, `^"timeline-tl_[a-f0-9]+-0-0"$`, etag)
	conditional := httptest.NewRequest(http.MethodGet, "/v1/projects/"+projectID+"/timeline", nil)
	conditional.Header.Set("If-None-Match", etag)
	conditionalRec := httptest.NewRecorder()
	h.ServeHTTP(conditionalRec, conditional)
	assert.Equal(t, http.StatusNotModified, conditionalRec.Code)

	// 加一条轨道
	rec = doJSON(t, h, "POST", "/v1/projects/"+projectID+"/timeline/ops", map[string]any{
		"clientMutationId": "req-1",
		"baseRevision":     0,
		"operations": []map[string]any{
			{"op": "addTrack", "track": map[string]any{"trackId": "v1", "kind": "video"}},
		},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var res entity.ApplyOpsResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	assert.Equal(t, uint64(1), res.Revision)
	assert.Equal(t, []string{"v1"}, res.Summary.AddedTrackIDs)

	// 相同 clientMutationId 重放返回首次结果，不重复应用、不增加 revision。
	replay := doJSON(t, h, "POST", "/v1/projects/"+projectID+"/timeline/ops", map[string]any{
		"clientMutationId": "req-1",
		"baseRevision":     0,
		"operations": []map[string]any{
			{"op": "addTrack", "track": map[string]any{"trackId": "v1", "kind": "video"}},
		},
	})
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	var replayResult entity.ApplyOpsResult
	require.NoError(t, json.Unmarshal(replay.Body.Bytes(), &replayResult))
	assert.Equal(t, uint64(1), replayResult.Revision)
	assert.Equal(t, uint64(1), ft.timelines[entity.TimelineWorkflowID(projectID)].Revision)

	// 旧 baseRevision → 409 + latestRevision
	rec = doJSON(t, h, "POST", "/v1/projects/"+projectID+"/timeline/ops", map[string]any{
		"requestId":    "req-2",
		"baseRevision": 0,
		"operations":   []map[string]any{{"op": "removeTrack", "trackId": "v1"}},
	})
	require.Equal(t, http.StatusConflict, rec.Code)
	var conflictBody struct {
		Code           string `json:"code"`
		LatestRevision uint64 `json:"latestRevision"`
		Message        string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &conflictBody))
	assert.Equal(t, entity.ErrTypeRevisionConflict, conflictBody.Code)
	assert.Equal(t, uint64(1), conflictBody.LatestRevision)
	assert.NotEmpty(t, conflictBody.Message)

	// 非法 op → 422 带错误码
	rec = doJSON(t, h, "POST", "/v1/projects/"+projectID+"/timeline/ops", map[string]any{
		"requestId":    "req-3",
		"baseRevision": 1,
		"operations":   []map[string]any{{"op": "removeTrack", "trackId": "nope"}},
	})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "NOT_FOUND")
	assert.Equal(t, uint64(1), ft.timelines[entity.TimelineWorkflowID(projectID)].Revision)

	// 批中第二条非法时，第一条也不得生效。
	rec = doJSON(t, h, "POST", "/v1/projects/"+projectID+"/timeline/ops", map[string]any{
		"clientMutationId": "req-atomic",
		"baseRevision":     1,
		"operations": []map[string]any{
			{"op": "addTrack", "track": map[string]any{"trackId": "a1", "kind": "audio"}},
			{"op": "removeTrack", "trackId": "missing"},
		},
	})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	state := ft.timelines[entity.TimelineWorkflowID(projectID)]
	assert.Equal(t, uint64(1), state.Revision)
	assert.NotContains(t, state.Doc.Tracks, timeline.Track{TrackID: "a1", Kind: timeline.TrackAudio})

	// 用户入口的权限/静态校验失败是 422，不得混成基础设施错误。
	rec = doJSON(t, h, "POST", "/v1/projects/"+projectID+"/timeline/ops", map[string]any{
		"clientMutationId": "req-user-acl",
		"baseRevision":     1,
		"operations": []map[string]any{{
			"op": "addMedia",
			"media": map[string]any{
				"mediaId": "m1", "uri": "https://example.invalid/video.mp4",
				"kind": "video", "durationInFrames": 30,
			},
		}},
	})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"code":"BAD_REQUEST"`)
	assert.Equal(t, uint64(1), state.Revision)

	// 缺 requestId → 400
	rec = doJSON(t, h, "POST", "/v1/projects/"+projectID+"/timeline/ops", map[string]any{
		"baseRevision": 1,
		"operations":   []map[string]any{{"op": "removeTrack", "trackId": "v1"}},
	})
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTimelineOpsRejectsUnsafeClientMutationIDs(t *testing.T) {
	s, _ := newTestServer()
	h := s.Handler()
	projectID := createProject(t, h)
	operations := []map[string]any{{
		"op": "addTrack", "track": map[string]any{"trackId": "v1", "kind": "video"},
	}}
	tests := []map[string]any{
		{"clientMutationId": "bad:value", "baseRevision": 0, "operations": operations},
		{"clientMutationId": strings.Repeat("a", entity.MaxClientMutationIDLength+1), "baseRevision": 0, "operations": operations},
		{"clientMutationId": "new-id", "requestId": "legacy-id", "baseRevision": 0, "operations": operations},
	}
	for _, body := range tests {
		rec := doJSON(t, h, http.MethodPost, "/v1/projects/"+projectID+"/timeline/ops", body)
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		var payload map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
		assert.Equal(t, "BAD_REQUEST", payload["code"])
		assert.NotEmpty(t, payload["message"])
	}
}

func TestTranscriptViewAndDeleteHTTPContract(t *testing.T) {
	server, temporalClient := newTestServer()
	handler := server.Handler()
	projectID := createProject(t, handler)
	workflowID := entity.TimelineWorkflowID(projectID)
	state := temporalClient.timelines[workflowID]
	state.Revision = 12
	state.Doc.Tracks = []timeline.Track{{TrackID: "v1", Kind: timeline.TrackVideo}}
	state.Doc.Media = []timeline.Media{{
		MediaID: "m1", Generation: 1, URI: "blob://m1", Kind: timeline.MediaVideo,
		DurationInFrames: 300,
		Transcript: &timeline.Transcript{
			Revision: 2,
			Segments: []timeline.TranscriptSegment{{
				SegmentID: "seg_one", StartMs: 0, EndMs: 1000, Text: "hello world",
				Words: []timeline.TranscriptWord{
					{WordID: "word_hello", StartMs: 0, EndMs: 500, Text: "hello"},
					{WordID: "word_world", StartMs: 500, EndMs: 1000, Text: "world"},
				},
			}},
		},
	}}
	state.Doc.Items = []timeline.Item{{
		ItemID: "it1", TrackID: "v1", Kind: timeline.ItemVideo, MediaID: "m1",
		From: 0, DurationInFrames: 60,
	}}

	get := doJSON(
		t, handler, http.MethodGet,
		"/v1/projects/"+projectID+"/transcript-view", nil,
	)
	require.Equal(t, http.StatusOK, get.Code, get.Body.String())
	var view timeline.TranscriptView
	require.NoError(t, json.Unmarshal(get.Body.Bytes(), &view))
	assert.Equal(t, uint64(12), view.TimelineRevision)
	require.Len(t, view.Entries, 2)
	assert.Equal(t, "hello", view.Entries[0].Text)

	endpoint := "/v1/projects/" + projectID + "/transcript-edits:delete"
	invalidBatch := doJSON(t, handler, http.MethodPost, endpoint, map[string]any{
		"clientMutationId": "transcript-invalid-batch",
		"baseRevision":     12,
		"mode":             "ripple",
		"ranges": []map[string]string{
			{
				"fromOccurrenceId": view.Entries[0].OccurrenceID,
				"toOccurrenceId":   view.Entries[0].OccurrenceID,
			},
			{"fromOccurrenceId": "forged", "toOccurrenceId": "forged"},
		},
	})
	require.Equal(t, http.StatusUnprocessableEntity, invalidBatch.Code, invalidBatch.Body.String())
	assert.Contains(t, invalidBatch.Body.String(), timeline.ErrInvalidOccurrenceID)
	assert.Equal(t, uint64(12), state.Revision)
	assert.Equal(t, int64(60), state.Doc.Items[0].DurationInFrames)

	deleteBody := map[string]any{
		"clientMutationId": "transcript-delete-1",
		"baseRevision":     12,
		"mode":             "ripple",
		"ranges": []map[string]string{{
			"fromOccurrenceId": view.Entries[0].OccurrenceID,
			"toOccurrenceId":   view.Entries[0].OccurrenceID,
		}},
	}
	deleted := doJSON(t, handler, http.MethodPost, endpoint, deleteBody)
	require.Equal(t, http.StatusOK, deleted.Code, deleted.Body.String())
	var result entity.DeleteTranscriptRangesResult
	require.NoError(t, json.Unmarshal(deleted.Body.Bytes(), &result))
	assert.Equal(t, uint64(13), result.Revision)
	assert.Equal(t, int64(15), result.DeletedDurationInFrames)
	assert.Equal(t, []timeline.TranscriptFrameRange{{FromFrame: 0, ToFrame: 15}}, result.DeletedRanges)

	replay := doJSON(t, handler, http.MethodPost, endpoint, deleteBody)
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	var replayResult entity.DeleteTranscriptRangesResult
	require.NoError(t, json.Unmarshal(replay.Body.Bytes(), &replayResult))
	assert.Equal(t, uint64(13), replayResult.Revision)
	assert.Equal(t, uint64(13), state.Revision)

	conflict := doJSON(t, handler, http.MethodPost, endpoint, map[string]any{
		"clientMutationId": "transcript-delete-stale",
		"baseRevision":     12,
		"mode":             "ripple",
		"ranges": []map[string]string{{
			"fromOccurrenceId": view.Entries[1].OccurrenceID,
			"toOccurrenceId":   view.Entries[1].OccurrenceID,
		}},
	})
	require.Equal(t, http.StatusConflict, conflict.Code, conflict.Body.String())
	assert.Contains(t, conflict.Body.String(), entity.ErrTypeRevisionConflict)
	assert.Contains(t, conflict.Body.String(), `"latestRevision":13`)

	otherProjectID := createProject(t, handler)
	otherState := temporalClient.timelines[entity.TimelineWorkflowID(otherProjectID)]
	otherState.Revision = 12
	otherState.Doc = state.Doc.Clone()
	crossProject := doJSON(
		t, handler, http.MethodPost,
		"/v1/projects/"+otherProjectID+"/transcript-edits:delete",
		map[string]any{
			"clientMutationId": "cross-project-selection",
			"baseRevision":     12,
			"mode":             "ripple",
			"ranges": []map[string]string{{
				"fromOccurrenceId": view.Entries[1].OccurrenceID,
				"toOccurrenceId":   view.Entries[1].OccurrenceID,
			}},
		},
	)
	require.Equal(t, http.StatusUnprocessableEntity, crossProject.Code, crossProject.Body.String())
	assert.Contains(t, crossProject.Body.String(), timeline.ErrCrossProjectOccurrence)
}

func TestOwnershipIsolation(t *testing.T) {
	s, _ := newTestServer()
	h := s.Handler()
	projectID := createProject(t, h)

	// 另一个用户访问 → 404
	req := httptest.NewRequest("GET", "/v1/projects/"+projectID+"/timeline", nil)
	req.Header.Set("X-AgentCut-User", "别人")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestUnavailableDependencies(t *testing.T) {
	s, _ := newTestServer()
	h := s.Handler()
	projectID := createProject(t, h)

	// Blob 未配置 → 上传 503
	req := httptest.NewRequest("POST", "/v1/projects/"+projectID+"/uploads", bytes.NewReader(nil))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	// agent-k 未配置 → 对话 503
	rec = doJSON(t, h, "POST", "/v1/projects/"+projectID+"/messages", map[string]any{"text": "hi"})
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	// 空时间线不可渲染 → 422（编译期拦截，不启动 Workflow）
	rec = doJSON(t, h, "POST", "/v1/projects/"+projectID+"/renders", map[string]any{})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestRenderLifecycle(t *testing.T) {
	s, ft := newTestServer()
	store, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	s.Blob = store
	h := s.Handler()
	projectID := createProject(t, h)

	// 先通过同一 Timeline Workflow 入口做出一条可渲染的时间线。
	_, setupErr := s.applyOps(context.Background(), projectID, "render-setup", "system", 0, []timeline.Operation{
		{Op: timeline.OpAddTrack, Track: &timeline.Track{TrackID: "v1", Kind: timeline.TrackVideo}},
		{Op: timeline.OpAddMedia, Media: &timeline.Media{
			MediaID: "m1", URI: "https://blob/a.mp4", Kind: timeline.MediaVideo, DurationInFrames: 300,
		}},
		{Op: timeline.OpAddItem, Item: &timeline.Item{
			ItemID: "i1", TrackID: "v1", MediaID: "m1", Kind: timeline.ItemVideo,
			From: 0, DurationInFrames: 300,
		}},
	})
	require.Nil(t, setupErr)

	// 触发导出
	rec := doJSON(t, h, "POST", "/v1/projects/"+projectID+"/renders", map[string]any{})
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	var accepted struct {
		RenderKey string `json:"renderKey"`
		Revision  uint64 `json:"revision"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &accepted))
	require.NotEmpty(t, accepted.RenderKey)
	assert.Equal(t, uint64(1), accepted.Revision)

	workflowID := render.WorkflowID(projectID, accepted.RenderKey)
	require.NotNil(t, ft.renders[workflowID])
	assert.Equal(t, render.ExecutorLocal, ft.renders[workflowID].input.Executor)
	assert.Equal(t, render.DefaultLocalProfile, ft.renders[workflowID].input.Profile)

	// 幂等：重复触发返回同一个 renderKey，不重复启动
	rec = doJSON(t, h, "POST", "/v1/projects/"+projectID+"/renders", map[string]any{})
	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), accepted.RenderKey)
	assert.Equal(t, 1, ft.renders[workflowID].starts)

	// 进行中 → running + progress
	rec = doJSON(t, h, "GET", "/v1/projects/"+projectID+"/renders/"+accepted.RenderKey, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"running"`)
	assert.Contains(t, rec.Body.String(), `"progress"`)

	// 完成 → completed + outputUri
	ft.renders[workflowID].status = enums.WORKFLOW_EXECUTION_STATUS_COMPLETED
	ft.renders[workflowID].output = render.Output{
		OutputURI: render.ContentURI(projectID, accepted.RenderKey),
		OutputKey: "agentcut/" + projectID + "/output/" + accepted.RenderKey + ".mp4",
		Executor:  render.ExecutorLocal,
		RenderKey: accepted.RenderKey, Revision: 1,
	}
	rec = doJSON(t, h, "GET", "/v1/projects/"+projectID+"/renders/"+accepted.RenderKey, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"completed"`)
	assert.Contains(t, rec.Body.String(), render.ContentURI(projectID, accepted.RenderKey))

	// 已完成后再次 POST：RejectDuplicate → 仍然 202、不重新渲染
	rec = doJSON(t, h, "POST", "/v1/projects/"+projectID+"/renders", map[string]any{})
	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.Equal(t, 1, ft.renders[workflowID].starts)

	// 未知 renderKey → 404
	rec = doJSON(t, h, "GET", "/v1/projects/"+projectID+"/renders/nope", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// 失败态 → failed
	ft.renders[workflowID].status = enums.WORKFLOW_EXECUTION_STATUS_FAILED
	rec = doJSON(t, h, "GET", "/v1/projects/"+projectID+"/renders/"+accepted.RenderKey, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"failed"`)

	// 同一个 key 失败后允许重新执行。
	rec = doJSON(t, h, "POST", "/v1/projects/"+projectID+"/renders", map[string]any{})
	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.Equal(t, 2, ft.renders[workflowID].starts)
	assert.Equal(t, enums.WORKFLOW_EXECUTION_STATUS_RUNNING, ft.renders[workflowID].status)
}

func TestRenderM3BSelectsLocalOrFailsClosed(t *testing.T) {
	setup := func(t *testing.T, s *Server, projectID string) {
		t.Helper()
		_, updateErr := s.applyOps(context.Background(), projectID, "m3b-setup", "system", 0, []timeline.Operation{
			{Op: timeline.OpAddTrack, Track: &timeline.Track{TrackID: "v1", Kind: timeline.TrackVideo}},
			{Op: timeline.OpAddMedia, Media: &timeline.Media{
				MediaID: "m1", URI: "https://blob/a.mp4", Kind: timeline.MediaVideo, DurationInFrames: 300,
			}},
			{Op: timeline.OpAddItem, Item: &timeline.Item{
				ItemID: "i1", TrackID: "v1", MediaID: "m1", Kind: timeline.ItemVideo,
				From: 0, DurationInFrames: 120,
				Transform: &timeline.ItemTransform{
					X: 0, Y: 0, Width: 1, Height: 1, Fit: timeline.ItemFitContain,
					RotationDegrees: 45,
				},
			}},
		})
		require.Nil(t, updateErr)
	}

	s, ft := newTestServer()
	s.RenderSelection = render.Selection{
		Executor: render.ExecutorAgentK,
		Profile:  render.DefaultAgentKProfile,
	}
	store, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	s.Blob = store
	h := s.Handler()
	projectID := createProject(t, h)
	setup(t, s, projectID)

	rec := doJSON(t, h, http.MethodPost, "/v1/projects/"+projectID+"/renders", map[string]any{})
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	var accepted struct {
		RenderKey string `json:"renderKey"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &accepted))
	run := ft.renders[render.WorkflowID(projectID, accepted.RenderKey)]
	require.NotNil(t, run)
	assert.Equal(t, render.ExecutorLocal, run.input.Executor)
	assert.Equal(t, render.DefaultLocalProfile, run.input.Profile)

	s2, ft2 := newTestServer()
	s2.RenderSelection = s.RenderSelection
	h2 := s2.Handler()
	projectID2 := createProject(t, h2)
	setup(t, s2, projectID2)
	rec = doJSON(t, h2, http.MethodPost, "/v1/projects/"+projectID2+"/renders", map[string]any{})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"UNSUPPORTED_RENDER_FEATURE"`)
	assert.Contains(t, rec.Body.String(), "requires local executor")
	assert.Empty(t, ft2.renders)
}

func TestRenderContentAuthScopeInlineAndDownload(t *testing.T) {
	s, ft := newTestServer()
	store, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	s.Blob = store
	h := s.Handler()
	projectID := createProject(t, h)
	renderKey := "0123456789abcdef"
	workflowID := render.WorkflowID(projectID, renderKey)
	outputKey := blob.OutputKey(projectID, renderKey)
	payload := []byte("real-mp4-bytes")
	_, err = store.Put(context.Background(), outputKey, bytes.NewReader(payload), int64(len(payload)), "video/mp4")
	require.NoError(t, err)
	ft.renders[workflowID] = &fakeRender{
		status: enums.WORKFLOW_EXECUTION_STATUS_COMPLETED,
		output: render.Output{
			RenderKey: renderKey, Executor: render.ExecutorLocal,
			OutputKey: outputKey, OutputURI: render.ContentURI(projectID, renderKey),
		},
	}
	contentPath := render.ContentURI(projectID, renderKey)

	rec := doJSON(t, h, http.MethodGet, contentPath, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, payload, rec.Body.Bytes())
	assert.Equal(t, "video/mp4", rec.Header().Get("Content-Type"))
	assert.Equal(t, `inline; filename="`+renderKey+`.mp4"`, rec.Header().Get("Content-Disposition"))
	assert.Equal(t, "bytes", rec.Header().Get("Accept-Ranges"))

	rec = doJSON(t, h, http.MethodGet, contentPath+"?download=1", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, `attachment; filename="`+renderKey+`.mp4"`, rec.Header().Get("Content-Disposition"))

	rangeReq := httptest.NewRequest(http.MethodGet, contentPath, nil)
	rangeReq.Header.Set("Range", "bytes=5-8")
	rangeRec := httptest.NewRecorder()
	h.ServeHTTP(rangeRec, rangeReq)
	require.Equal(t, http.StatusPartialContent, rangeRec.Code, rangeRec.Body.String())
	assert.Equal(t, payload[5:9], rangeRec.Body.Bytes())
	assert.Equal(t, "bytes 5-8/14", rangeRec.Header().Get("Content-Range"))
	assert.Equal(t, "bytes", rangeRec.Header().Get("Accept-Ranges"))

	crossProject := httptest.NewRequest(http.MethodGet, contentPath, nil)
	crossProject.Header.Set("X-AgentCut-User", "other-user")
	crossRec := httptest.NewRecorder()
	h.ServeHTTP(crossRec, crossProject)
	assert.Equal(t, http.StatusNotFound, crossRec.Code)

	rec = doJSON(t, h, http.MethodGet,
		"/v1/projects/"+projectID+"/renders/not-a-render-key/content", nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateProjectCompensation(t *testing.T) {
	// 登记用户索引失败 → 已启动的 Timeline 实体 Workflow 必须被补偿删除
	s, ft := newTestServer()
	ft.failAddProject = errors.New("temporal unavailable")
	h := s.Handler()

	rec := doJSON(t, h, "POST", "/v1/projects", map[string]any{"title": "会失败"})
	require.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Empty(t, ft.timelines, "failed creation must not leak the timeline entity workflow")
}

func TestCreateProjectCompensationOnSessionFailure(t *testing.T) {
	// agent-k 建会话失败 → 同样补偿删除 Timeline Workflow
	agentk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
	}))
	defer agentk.Close()

	s, ft := newTestServer()
	client, err := agentkclient.New(agentk.URL, "key")
	require.NoError(t, err)
	s.AgentK = client
	s.Profile = []byte(`{"system":"x"}`)
	h := s.Handler()

	rec := doJSON(t, h, "POST", "/v1/projects", map[string]any{"title": "会失败"})
	require.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Empty(t, ft.timelines, "failed session creation must not leak the timeline entity workflow")
}

func TestBuildSessionProfile(t *testing.T) {
	template := []byte(`{
		"system": "x",
		"tools": [{"capability": "contrib.agentcut.timeline_get", "name": "timeline_get"}],
		"toolDefaults": {"understand_media": {"model": "m"}}
	}`)
	profile, err := BuildSessionProfile(template, "prj_1")
	require.NoError(t, err)

	// 原有默认值保留
	assert.Equal(t, "m", profile.GetToolDefaults()["understand_media"].AsMap()["model"])
	// 只给模板实际声明的 timeline 工具注入，避免 Agent-K 拒绝未知工具默认值。
	assert.Equal(t, "prj_1", profile.GetToolDefaults()["timeline_get"].AsMap()["projectId"])
	for _, name := range timelineToolNames[1:] {
		assert.NotContains(t, profile.GetToolDefaults(), name)
	}

	// proto 未定义的字段（旧模板的 ref 写法）必须被拒绝，防契约漂移。
	_, err = BuildSessionProfile([]byte(`{"tools":[{"ref":"tool:agentk.x","name":"x"}]}`), "prj_1")
	assert.Error(t, err)
}

// ---- 假 agent-k：进程内 Connect 服务 ----

type fakeAgentK struct {
	apiv1connect.UnimplementedAgentKServiceHandler

	lastCreate *apiv1.CreateSessionRequest
	lastStart  *apiv1.StartExecutionRequest
	running    bool // ListExecutions 是否返回一个 RUNNING Execution
	cancelled  int
}

func (f *fakeAgentK) CreateSession(
	_ context.Context, req *connect.Request[apiv1.CreateSessionRequest],
) (*connect.Response[apiv1.CreateSessionResponse], error) {
	f.lastCreate = req.Msg
	if req.Msg.GetProfile() == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("profile required"))
	}
	return connect.NewResponse(&apiv1.CreateSessionResponse{
		Session: &apiv1.Session{Id: "ses_test", Title: req.Msg.GetTitle()},
	}), nil
}

func (f *fakeAgentK) StartExecution(
	_ context.Context, req *connect.Request[apiv1.StartExecutionRequest],
) (*connect.Response[apiv1.StartExecutionResponse], error) {
	f.lastStart = req.Msg
	return connect.NewResponse(&apiv1.StartExecutionResponse{
		Execution: &apiv1.Execution{Id: "exe_1", SessionId: req.Msg.GetSessionId(),
			Status: apiv1.ExecutionStatus_EXECUTION_STATUS_RUNNING},
	}), nil
}

func (f *fakeAgentK) ListExecutions(
	_ context.Context, req *connect.Request[apiv1.ListExecutionsRequest],
) (*connect.Response[apiv1.ListExecutionsResponse], error) {
	resp := &apiv1.ListExecutionsResponse{}
	if f.running {
		resp.Executions = []*apiv1.Execution{{Id: "exe_1", SessionId: req.Msg.GetSessionId(),
			Status: apiv1.ExecutionStatus_EXECUTION_STATUS_RUNNING}}
	}
	return connect.NewResponse(resp), nil
}

func (f *fakeAgentK) CancelExecution(
	_ context.Context, _ *connect.Request[apiv1.CancelExecutionRequest],
) (*connect.Response[apiv1.CancelExecutionResponse], error) {
	f.cancelled++
	return connect.NewResponse(&apiv1.CancelExecutionResponse{}), nil
}

func (f *fakeAgentK) ListEvents(
	_ context.Context, req *connect.Request[apiv1.ListEventsRequest],
) (*connect.Response[apiv1.ListEventsResponse], error) {
	return connect.NewResponse(&apiv1.ListEventsResponse{
		Events:        []*apiv1.Event{fakeTextEvent("evt_2", "你好")},
		NextPageToken: "tok_next",
	}), nil
}

func (f *fakeAgentK) WatchEvents(
	_ context.Context, req *connect.Request[apiv1.WatchEventsRequest],
	stream *connect.ServerStream[apiv1.WatchEventsResponse],
) error {
	_ = stream.Send(&apiv1.WatchEventsResponse{Frame: &apiv1.WatchEventsResponse_Delta{
		Delta: &apiv1.Delta{ExecutionId: req.Msg.GetExecutionId(), Kind: apiv1.DeltaBlockKind_DELTA_BLOCK_KIND_TEXT, Text: "你"},
	}})
	_ = stream.Send(&apiv1.WatchEventsResponse{Frame: &apiv1.WatchEventsResponse_CommittedEvent{
		CommittedEvent: fakeTextEvent("evt_3", "你好"),
	}})
	_ = stream.Send(&apiv1.WatchEventsResponse{Frame: &apiv1.WatchEventsResponse_StreamEnd{
		StreamEnd: &apiv1.StreamEnd{ExecutionId: req.Msg.GetExecutionId()},
	}})
	return nil
}

func fakeTextEvent(id, text string) *apiv1.Event {
	return &apiv1.Event{
		EventId:     id,
		ExecutionId: "exe_1",
		RoundId:     "rnd_1",
		CreatedAt:   timestamppb.New(time.Unix(1700000000, 0)),
		Payload:     &apiv1.Event_AssistantText{AssistantText: &apiv1.AssistantText{Text: text}},
	}
}

func newAgentTestServer(t *testing.T) (http.Handler, *fakeAgentK, string) {
	t.Helper()
	fake := &fakeAgentK{running: true}
	path, handler := apiv1connect.NewAgentKServiceHandler(fake)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	upstream := httptest.NewServer(mux)
	t.Cleanup(upstream.Close)

	s, _ := newTestServer()
	akClient, err := agentkclient.New(upstream.URL, "key")
	require.NoError(t, err)
	s.AgentK = akClient
	s.Profile = []byte(`{"system":"x"}`)
	h := s.Handler()
	projectID := createProject(t, h)
	return h, fake, projectID
}

func TestCreateProjectUsesSelectedProfile(t *testing.T) {
	fake := &fakeAgentK{}
	path, handler := apiv1connect.NewAgentKServiceHandler(fake)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	upstream := httptest.NewServer(mux)
	defer upstream.Close()

	s, _ := newTestServer()
	akClient, err := agentkclient.New(upstream.URL, "key")
	require.NoError(t, err)
	s.AgentK = akClient
	s.Profiles = []ProfileDefinition{{
		ProfileID: "custom", Name: "Custom",
		ProfileJSON: []byte(`{"system":"selected profile"}`), IsDefault: true,
	}}

	rec := doJSON(t, s.Handler(), http.MethodPost, "/v1/projects",
		map[string]any{"title": "profile project", "profileId": "custom"})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.NotNil(t, fake.lastCreate)
	assert.Equal(t, "selected profile", fake.lastCreate.GetProfile().GetSystem())
	var ref entity.ProjectRef
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &ref))
	assert.Equal(t, "custom", ref.ProfileID)
}

func TestAgentBridge(t *testing.T) {
	h, fake, projectID := newAgentTestServer(t)

	// 发消息 → StartExecution(message input)，202 + executionId
	rec := doJSON(t, h, "POST", "/v1/projects/"+projectID+"/messages", map[string]any{"text": "hi"})
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "exe_1")
	require.NotNil(t, fake.lastStart)
	assert.Equal(t, "ses_test", fake.lastStart.GetSessionId())
	assert.Equal(t, "hi", fake.lastStart.GetMessage().GetText())

	// 结构化编辑器上下文通过显式 system/context envelope 传给仅支持 text 的 SDK；
	// 既有 profileId/mediaIds 字段仍可同时发送。
	rec = doJSON(t, h, "POST", "/v1/projects/"+projectID+"/messages", map[string]any{
		"text":      "trim selection",
		"profileId": "agentcut-default",
		"mediaIds":  []string{"media_1"},
		"editorContext": map[string]any{
			"timelineRevision":      7,
			"selectedItemIds":       []string{"clip_a", "clip_b"},
			"playheadFrame":         42,
			"selectedMediaId":       "media_1",
			"selectedCaptionItemId": "caption_1",
		},
	})
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	message := fake.lastStart.GetMessage().GetText()
	assert.Contains(t, message, editorContextStart)
	assert.Contains(t, message, `"timelineRevision":7`)
	assert.Contains(t, message, `"selectedItemIds":["clip_a","clip_b"]`)
	assert.Contains(t, message, `"playheadFrame":42`)
	assert.Contains(t, message, userMessageStart+"\ntrim selection\n"+userMessageEnd)

	rec = doJSON(t, h, "POST", "/v1/projects/"+projectID+"/messages", map[string]any{
		"text": "bad context",
		"editorContext": map[string]any{
			"timelineRevision": 1,
			"selectedItemIds":  []string{},
			"playheadFrame":    -1,
		},
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// 空消息 → 400
	rec = doJSON(t, h, "POST", "/v1/projects/"+projectID+"/messages", map[string]any{})
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// answers → StartExecution(answers input)
	rec = doJSON(t, h, "POST", "/v1/projects/"+projectID+"/answers", map[string]any{
		"toolCallId": "call_1",
		"answers":    []map[string]any{{"questionId": "q1", "selected": []string{"a"}}},
	})
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.NotNil(t, fake.lastStart.GetAnswers())
	assert.Equal(t, "call_1", fake.lastStart.GetAnswers().GetToolCallId())
	assert.Equal(t, "q1", fake.lastStart.GetAnswers().GetAnswers()[0].GetQuestionId())

	// events → envelope 翻译
	rec = doJSON(t, h, "GET", "/v1/projects/"+projectID+"/events?limit=10", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var page struct {
		Events []struct {
			EventID     string          `json:"eventId"`
			ExecutionID string          `json:"executionId"`
			Kind        string          `json:"kind"`
			Data        json.RawMessage `json:"data"`
			CreatedAt   string          `json:"createdAt"`
		} `json:"events"`
		NextPageToken string `json:"nextPageToken"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
	require.Len(t, page.Events, 1)
	assert.Equal(t, "evt_2", page.Events[0].EventID)
	assert.Equal(t, "exe_1", page.Events[0].ExecutionID)
	assert.Equal(t, "assistant.text", page.Events[0].Kind)
	assert.Contains(t, string(page.Events[0].Data), "你好")
	assert.Equal(t, "tok_next", page.NextPageToken)
	assert.NotEmpty(t, page.Events[0].CreatedAt)

	// stream → SSE：delta、提交事件、stream.end
	rec = doJSON(t, h, "GET", "/v1/projects/"+projectID+"/stream", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/event-stream")
	assert.True(t, strings.Contains(body, "event: delta"), body)
	assert.True(t, strings.Contains(body, "event: assistant.text"), body)
	assert.True(t, strings.Contains(body, `"eventId":"evt_3"`), body)
	assert.True(t, strings.Contains(body, "event: stream.end"), body)

	// 没有进行中的 Execution → 204
	fake.running = false
	rec = doJSON(t, h, "GET", "/v1/projects/"+projectID+"/stream", nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestDeleteProjectCancelsAgentExecution(t *testing.T) {
	h, fake, projectID := newAgentTestServer(t)
	rec := doJSON(t, h, http.MethodDelete, "/v1/projects/"+projectID, nil)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, 1, fake.cancelled)
}

func TestCreateProjectFailureCancelsAgentExecution(t *testing.T) {
	fake := &fakeAgentK{}
	path, handler := apiv1connect.NewAgentKServiceHandler(fake)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	upstream := httptest.NewServer(mux)
	defer upstream.Close()

	s, ft := newTestServer()
	ft.failAddProject = errors.New("index unavailable")
	akClient, err := agentkclient.New(upstream.URL, "key")
	require.NoError(t, err)
	s.AgentK = akClient
	s.Profile = []byte(`{"system":"x"}`)

	rec := doJSON(t, s.Handler(), http.MethodPost, "/v1/projects", map[string]any{"title": "失败"})
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	assert.Equal(t, 1, fake.cancelled)
	assert.Empty(t, ft.timelines)
}
