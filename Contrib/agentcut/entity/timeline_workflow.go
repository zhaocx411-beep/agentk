// Package entity 实现 AgentCut 的实体 Workflow（Entity Workflow 模式）：
// Timeline 与项目归属的权威状态保存在长驱动 Temporal Workflow 内，由
// Temporal Server 自身持久化，不依赖任何自建数据库。
package entity

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"agentcut/timeline"
)

// Workflow / handler / activity 名称常量。cut-apiserver 与 Agent Tool
// Activity 都通过这些名字与实体 Workflow 交互。
const (
	TimelineWorkflowName    = "TimelineEntityWorkflow"
	UpdateApplyOps          = "applyOps"
	UpdateDeleteTranscript  = "deleteTranscriptRanges"
	UpdateRestoreTimeline   = "restoreTimeline"
	UpdateTimelineView      = "updateTimelineView"
	UpdateCreateTimeline    = "createTimeline"
	UpdateSwitchTimeline    = "switchTimeline"
	UpdateRenameTimeline    = "renameTimeline"
	UpdateDeleteTimeline    = "deleteTimeline"
	UpdateBindAgentKSession = "bindAgentKSession"
	UpdateCreateShare       = "createShare"
	UpdateRevokeShare       = "revokeShare"
	QueryGetState           = "getState"
	QuerySharedTimeline     = "sharedTimeline"
	SignalDeleteTimeline    = "delete"

	// SnapshotActivityName 把 {doc, revision} PUT 进 BlobStore 做只读备份。
	// 实现由 worker 注册（platform/blob），快照不是权威状态。
	SnapshotActivityName = "SnapshotTimeline"
	// DeleteMediaBlobActivityName 在 removeMedia 状态提交后幂等回收源对象。
	DeleteMediaBlobActivityName = "DeleteMediaBlob"
)

// 应用错误类型（temporal.ApplicationError 的 Type 字段），客户端据此分支。
const (
	ErrTypeRevisionConflict  = "REVISION_CONFLICT"
	ErrTypeOpFailed          = "TIMELINE_OP_FAILED"
	ErrTypeTranscriptEdit    = "TRANSCRIPT_EDIT_FAILED"
	ErrTypeTimelineNotActive = "TIMELINE_NOT_ACTIVE"
)

// maxUpdatesPerRun 是单次 Workflow Run 处理的 Update 上限，超过后
// ContinueAsNew，防止 History 无限增长拖慢 Replay。
const maxUpdatesPerRun = 500
const maxShareGrantsPerProject = 100

// TimelineWorkflowID 返回项目对应的确定性 Workflow ID。
func TimelineWorkflowID(projectID string) string { return "timeline:" + projectID }

// TimelineInit 是 Workflow 启动/ContinueAsNew 的入参。
type TimelineInit struct {
	ProjectID        string             `json:"projectId"`
	Doc              timeline.Doc       `json:"doc"`
	Revision         uint64             `json:"revision"`
	ActiveTimelineID string             `json:"activeTimelineId,omitempty"`
	Timelines        []TimelineSequence `json:"timelines,omitempty"`
	ProjectRevision  uint64             `json:"projectRevision,omitempty"`
	AgentKSessionID  string             `json:"agentkSessionId,omitempty"`
	// DisableSnapshots 供测试关闭快照 Activity。
	DisableSnapshots bool `json:"disableSnapshots,omitempty"`
	// DisableMediaCleanup 供纯 Workflow 测试关闭 Blob 清理 Activity。
	DisableMediaCleanup bool `json:"disableMediaCleanup,omitempty"`
	// ShareGrants 保存 token hash → 过期 Unix 毫秒；原始 token 不进 Workflow。
	ShareGrants map[string]int64 `json:"shareGrants,omitempty"`
	// MaxUpdatesPerRun 覆盖 ContinueAsNew 阈值（<=0 用默认值），供测试用。
	MaxUpdatesPerRun int `json:"maxUpdatesPerRun,omitempty"`
	// MediaGenerations retains the highest generation even after deletion.
	MediaGenerations map[string]uint64 `json:"mediaGenerations,omitempty"`
	// PendingDeletes survives Continue-As-New. Failed cleanup remains queued.
	PendingDeletes []DeleteMediaBlobRequest `json:"pendingDeletes,omitempty"`
}

// TimelineState 是 Query 返回的权威状态。
type TimelineState struct {
	ProjectID        string             `json:"projectId"`
	Doc              timeline.Doc       `json:"doc"`
	Revision         uint64             `json:"revision"`
	ActiveTimelineID string             `json:"activeTimelineId"`
	Timelines        []TimelineSequence `json:"timelines"`
	ProjectRevision  uint64             `json:"projectRevision"`
	AgentKSessionID  string             `json:"agentkSessionId,omitempty"`
}

// ApplyOpsRequest 是一次修改请求。幂等去重靠入口从项目、来源和 mutation ID
// 派生出的 Temporal UpdateID，这里不重复携带不可信的外部标识。
type ApplyOpsRequest struct {
	TimelineID string `json:"timelineId,omitempty"`
	// BaseRevision 必须等于当前 revision，否则返回 REVISION_CONFLICT。
	BaseRevision uint64 `json:"baseRevision"`
	// Source 由入口固定：HTTP 前端 = user，Agent Tool = agent，上传链路 = system。
	Source     string               `json:"source"`
	Operations []timeline.Operation `json:"operations"`
}

// ApplyOpsResult 是修改成功的返回。
type ApplyOpsResult struct {
	Revision              uint64                 `json:"revision"`
	Summary               timeline.ChangeSummary `json:"summary"`
	AddedMediaGenerations map[string]uint64      `json:"addedMediaGenerations,omitempty"`
}

type DeleteTranscriptRangesRequest struct {
	TimelineID   string                           `json:"timelineId,omitempty"`
	BaseRevision uint64                           `json:"baseRevision"`
	Source       string                           `json:"source"`
	Mode         string                           `json:"mode"`
	Ranges       []timeline.TranscriptDeleteRange `json:"ranges"`
}

type DeleteTranscriptRangesResult struct {
	Revision                uint64                          `json:"revision"`
	Doc                     timeline.Doc                    `json:"doc"`
	Summary                 timeline.ChangeSummary          `json:"summary"`
	DeletedRanges           []timeline.TranscriptFrameRange `json:"deletedRanges"`
	DeletedDurationInFrames int64                           `json:"deletedDurationInFrames"`
}

type RestoreTimelineRequest struct {
	TimelineID   string       `json:"timelineId,omitempty"`
	BaseRevision uint64       `json:"baseRevision"`
	Source       string       `json:"source"`
	Doc          timeline.Doc `json:"doc"`
}

type RestoreTimelineResult struct {
	Revision uint64       `json:"revision"`
	Doc      timeline.Doc `json:"doc"`
}

type UpdateTimelineViewRequest struct {
	TimelineID      string  `json:"timelineId"`
	CurrentFrame    int64   `json:"currentFrame"`
	Zoom            float64 `json:"zoom"`
	UpdatedAtUnixMs int64   `json:"updatedAtUnixMs"`
}

// RevisionConflictDetails 附在 REVISION_CONFLICT 错误 Details 里。
type RevisionConflictDetails struct {
	LatestRevision uint64 `json:"latestRevision"`
}

type BindAgentKSessionRequest struct {
	SessionID string `json:"sessionId"`
}

// SnapshotRequest 是快照 Activity 的入参。
type SnapshotRequest struct {
	ProjectID string       `json:"projectId"`
	Revision  uint64       `json:"revision"`
	Doc       timeline.Doc `json:"doc"`
}

// DeleteMediaBlobRequest 是媒体源对象回收 Activity 的入参。
type DeleteMediaBlobRequest struct {
	ProjectID  string `json:"projectId"`
	MediaID    string `json:"mediaId"`
	BlobKey    string `json:"blobKey"`
	Generation uint64 `json:"generation,omitempty"`
}

type CreateShareRequest struct {
	TokenHash       string `json:"tokenHash"`
	NowUnixMs       int64  `json:"nowUnixMs"`
	ExpiresAtUnixMs int64  `json:"expiresAtUnixMs"`
}

type RevokeShareRequest struct {
	TokenHash string `json:"tokenHash"`
}

type SharedTimelineRequest struct {
	TokenHash string `json:"tokenHash"`
	NowUnixMs int64  `json:"nowUnixMs"`
}

type SharedTimelineResult struct {
	Status string        `json:"status"` // valid | expired | not_found
	State  TimelineState `json:"state,omitempty"`
}

func requireActiveTimeline(state TimelineState, timelineID string) error {
	if timelineID == "" || timelineID == state.ActiveTimelineID {
		return nil
	}
	return temporal.NewApplicationErrorWithOptions(
		fmt.Sprintf("timeline %q is not active; active timeline is %q", timelineID, state.ActiveTimelineID),
		ErrTypeTimelineNotActive,
		temporal.ApplicationErrorOptions{NonRetryable: true},
	)
}

// TimelineEntityWorkflow 持有一个项目的 Timeline 权威状态。
//
//   - 修改 = Update（UpdateID 幂等去重，处理严格串行，无需乐观锁实现）；
//   - 读取 = Query（只读通道，不进 History）；
//   - 删除项目 = Signal，Workflow 结束；
//   - History 体量靠 ContinueAsNew 控制；
//   - 每次状态变化后异步快照进 BlobStore（只读备份，失败不影响主流程）。
func TimelineEntityWorkflow(ctx workflow.Context, init TimelineInit) error {
	state := NewTimelineState(init, workflow.Now(ctx).UnixMilli())
	updatesInRun := 0
	deleted := false
	pendingDeletes := append([]DeleteMediaBlobRequest(nil), init.PendingDeletes...)
	mediaGenerations := make(map[string]uint64, len(init.MediaGenerations)+len(state.Doc.Media))
	for mediaID, generation := range init.MediaGenerations {
		mediaGenerations[mediaID] = generation
	}
	for _, sequence := range state.Timelines {
		for _, media := range sequence.Doc.Media {
			if media.Generation > mediaGenerations[media.MediaID] {
				mediaGenerations[media.MediaID] = media.Generation
			}
		}
	}
	shareGrants := make(map[string]int64, len(init.ShareGrants))
	for tokenHash, expiry := range init.ShareGrants {
		shareGrants[tokenHash] = expiry
	}
	lastSnapshotRev := init.Revision
	threshold := init.MaxUpdatesPerRun
	if threshold <= 0 {
		threshold = maxUpdatesPerRun
	}

	if err := workflow.SetQueryHandler(ctx, QueryGetState, func() (TimelineState, error) {
		return state, nil
	}); err != nil {
		return err
	}
	if err := workflow.SetQueryHandler(ctx, QuerySharedTimeline,
		func(req SharedTimelineRequest) (SharedTimelineResult, error) {
			expiry, ok := shareGrants[req.TokenHash]
			if !ok {
				return SharedTimelineResult{Status: "not_found"}, nil
			}
			if req.NowUnixMs >= expiry {
				return SharedTimelineResult{Status: "expired"}, nil
			}
			return SharedTimelineResult{Status: "valid", State: state}, nil
		}); err != nil {
		return err
	}
	if err := workflow.SetUpdateHandler(ctx, UpdateBindAgentKSession,
		func(ctx workflow.Context, req BindAgentKSessionRequest) error {
			sessionID := strings.TrimSpace(req.SessionID)
			if !strings.HasPrefix(sessionID, "ses_") {
				return errors.New("valid Agent-K sessionId is required")
			}
			if state.AgentKSessionID != "" && state.AgentKSessionID != sessionID {
				return errors.New("timeline is already bound to a different Agent-K session")
			}
			if state.AgentKSessionID == "" {
				state.AgentKSessionID = sessionID
				updatesInRun++
			}
			return nil
		}); err != nil {
		return err
	}
	if err := workflow.SetUpdateHandler(ctx, UpdateTimelineView,
		func(_ workflow.Context, req UpdateTimelineViewRequest) (TimelineSummary, error) {
			if err := requireActiveTimeline(state, req.TimelineID); err != nil {
				return TimelineSummary{}, err
			}
			if req.CurrentFrame < 0 {
				return TimelineSummary{}, errors.New("currentFrame must be non-negative")
			}
			if req.Zoom <= 0 || req.Zoom > 100 {
				return TimelineSummary{}, errors.New("zoom must be within (0, 100]")
			}
			for index := range state.Timelines {
				if state.Timelines[index].TimelineID != state.ActiveTimelineID {
					continue
				}
				state.Timelines[index].CurrentFrame = req.CurrentFrame
				state.Timelines[index].Zoom = req.Zoom
				state.Timelines[index].UpdatedAtUnixMs = req.UpdatedAtUnixMs
				state.ProjectRevision++
				updatesInRun++
				summary := state.TimelineSummaries()[index]
				return summary, nil
			}
			return TimelineSummary{}, errors.New("active timeline not found")
		}); err != nil {
		return err
	}
	if err := workflow.SetUpdateHandler(ctx, UpdateCreateTimeline,
		func(_ workflow.Context, req CreateTimelineRequest) (TimelineState, error) {
			if err := ValidateClientMutationID(req.TimelineID); err != nil {
				return TimelineState{}, fmt.Errorf("invalid timelineId: %w", err)
			}
			if err := ValidateClientMutationID(req.BaseVideoTrackID); err != nil {
				return TimelineState{}, fmt.Errorf("invalid baseVideoTrackId: %w", err)
			}
			if strings.TrimSpace(req.Name) == "" || len([]rune(strings.TrimSpace(req.Name))) > 120 {
				return TimelineState{}, errors.New("name must contain 1..120 characters")
			}
			if err := state.createTimeline(req); err != nil {
				return TimelineState{}, err
			}
			updatesInRun++
			return state, nil
		}); err != nil {
		return err
	}
	if err := workflow.SetUpdateHandler(ctx, UpdateSwitchTimeline,
		func(_ workflow.Context, req SwitchTimelineRequest) (TimelineState, error) {
			if err := ValidateClientMutationID(req.TimelineID); err != nil {
				return TimelineState{}, fmt.Errorf("invalid timelineId: %w", err)
			}
			if err := state.switchTimeline(req); err != nil {
				return TimelineState{}, err
			}
			updatesInRun++
			return state, nil
		}); err != nil {
		return err
	}
	if err := workflow.SetUpdateHandler(ctx, UpdateRenameTimeline,
		func(_ workflow.Context, req RenameTimelineRequest) (TimelineState, error) {
			if err := ValidateClientMutationID(req.TimelineID); err != nil {
				return TimelineState{}, fmt.Errorf("invalid timelineId: %w", err)
			}
			if strings.TrimSpace(req.Name) == "" || len([]rune(strings.TrimSpace(req.Name))) > 120 {
				return TimelineState{}, errors.New("name must contain 1..120 characters")
			}
			if err := state.renameTimeline(req); err != nil {
				return TimelineState{}, err
			}
			updatesInRun++
			return state, nil
		}); err != nil {
		return err
	}
	if err := workflow.SetUpdateHandler(ctx, UpdateDeleteTimeline,
		func(_ workflow.Context, req DeleteTimelineRequest) (TimelineState, error) {
			if err := ValidateClientMutationID(req.TimelineID); err != nil {
				return TimelineState{}, fmt.Errorf("invalid timelineId: %w", err)
			}
			removedMedia, err := state.deleteTimeline(req)
			if err != nil {
				return TimelineState{}, err
			}
			if !init.DisableMediaCleanup {
				for _, media := range removedMedia {
					if state.mediaReferenced(media.MediaID) || (media.BlobKey == "" && media.Processing == nil) {
						continue
					}
					pendingDeletes = appendDeleteRequests(pendingDeletes, []DeleteMediaBlobRequest{{
						ProjectID: state.ProjectID, MediaID: media.MediaID, BlobKey: media.BlobKey,
						Generation: media.Generation,
					}})
				}
			}
			updatesInRun++
			return state, nil
		}); err != nil {
		return err
	}

	err := workflow.SetUpdateHandlerWithOptions(ctx, UpdateApplyOps,
		func(ctx workflow.Context, req ApplyOpsRequest) (ApplyOpsResult, error) {
			if err := requireActiveTimeline(state, req.TimelineID); err != nil {
				return ApplyOpsResult{}, err
			}
			if req.BaseRevision != state.Revision {
				return ApplyOpsResult{}, temporal.NewApplicationErrorWithOptions(
					fmt.Sprintf("base revision %d does not match latest %d", req.BaseRevision, state.Revision),
					ErrTypeRevisionConflict,
					temporal.ApplicationErrorOptions{
						NonRetryable: true,
						Details:      []any{RevisionConflictDetails{LatestRevision: state.Revision}},
					})
			}
			operations := append([]timeline.Operation(nil), req.Operations...)
			nextGenerations := make(map[string]uint64)
			for index := range operations {
				op := &operations[index]
				if op.Op != timeline.OpAddMedia || op.Media == nil {
					continue
				}
				media := *op.Media
				baseGeneration := mediaGenerations[media.MediaID]
				if staged := nextGenerations[media.MediaID]; staged > baseGeneration {
					baseGeneration = staged
				}
				media.Generation = baseGeneration + 1
				nextGenerations[media.MediaID] = media.Generation
				op.Media = &media
			}
			var cleanupCandidates []DeleteMediaBlobRequest
			for _, op := range operations {
				if op.Op != timeline.OpRemoveMedia || op.MediaID == "" {
					continue
				}
				for _, media := range state.Doc.Media {
					if media.MediaID == op.MediaID && (media.BlobKey != "" || media.Processing != nil) {
						cleanupCandidates = append(cleanupCandidates, DeleteMediaBlobRequest{
							ProjectID: state.ProjectID, MediaID: media.MediaID, BlobKey: media.BlobKey,
							Generation: media.Generation,
						})
						break
					}
				}
			}
			next, sum, aerr := timeline.ApplyOps(state.Doc, operations)
			if aerr != nil {
				var oe *timeline.OpError
				if errors.As(aerr, &oe) {
					return ApplyOpsResult{}, temporal.NewApplicationErrorWithOptions(
						oe.Error(), ErrTypeOpFailed,
						temporal.ApplicationErrorOptions{NonRetryable: true, Details: []any{*oe}})
				}
				return ApplyOpsResult{}, aerr
			}
			next, removedAutoTracks := timeline.CleanupEmptyAutoTracks(next)
			sum.RemovedTrackIDs = append(sum.RemovedTrackIDs, removedAutoTracks...)
			state.Doc = next
			state.Revision++
			state.syncActiveSequence(workflow.Now(ctx).UnixMilli())
			for mediaID, generation := range nextGenerations {
				mediaGenerations[mediaID] = generation
			}
			updatesInRun++
			if !init.DisableMediaCleanup {
				for _, candidate := range cleanupCandidates {
					if !state.mediaReferenced(candidate.MediaID) {
						pendingDeletes = appendDeleteRequests(pendingDeletes, []DeleteMediaBlobRequest{candidate})
					}
				}
			}
			return ApplyOpsResult{
				Revision: state.Revision, Summary: sum,
				AddedMediaGenerations: nextGenerations,
			}, nil
		},
		workflow.UpdateHandlerOptions{
			// Validator 在请求进 History 之前拒绝明显非法的请求。
			Validator: func(ctx workflow.Context, req ApplyOpsRequest) error {
				return ValidateApplyOpsRequest(req)
			},
		},
	)
	if err != nil {
		return err
	}

	err = workflow.SetUpdateHandlerWithOptions(ctx, UpdateDeleteTranscript,
		func(ctx workflow.Context, req DeleteTranscriptRangesRequest) (DeleteTranscriptRangesResult, error) {
			if err := requireActiveTimeline(state, req.TimelineID); err != nil {
				return DeleteTranscriptRangesResult{}, err
			}
			if req.BaseRevision != state.Revision {
				return DeleteTranscriptRangesResult{}, temporal.NewApplicationErrorWithOptions(
					fmt.Sprintf("base revision %d does not match latest %d", req.BaseRevision, state.Revision),
					ErrTypeRevisionConflict,
					temporal.ApplicationErrorOptions{
						NonRetryable: true,
						Details:      []any{RevisionConflictDetails{LatestRevision: state.Revision}},
					},
				)
			}
			compiled, editErr := timeline.CompileTranscriptDelete(
				state.ProjectID, state.Revision, state.Doc, req.Ranges,
			)
			if editErr != nil {
				return DeleteTranscriptRangesResult{}, temporal.NewApplicationErrorWithOptions(
					editErr.Error(),
					ErrTypeTranscriptEdit,
					temporal.ApplicationErrorOptions{
						NonRetryable: true,
						Details:      []any{*editErr},
					},
				)
			}
			next, summary, applyErr := timeline.ApplyOps(state.Doc, compiled.Operations)
			if applyErr != nil {
				var opErr *timeline.OpError
				if errors.As(applyErr, &opErr) {
					return DeleteTranscriptRangesResult{}, temporal.NewApplicationErrorWithOptions(
						opErr.Error(),
						ErrTypeOpFailed,
						temporal.ApplicationErrorOptions{
							NonRetryable: true,
							Details:      []any{*opErr},
						},
					)
				}
				return DeleteTranscriptRangesResult{}, applyErr
			}
			next, removedAutoTracks := timeline.CleanupEmptyAutoTracks(next)
			summary.RemovedTrackIDs = append(summary.RemovedTrackIDs, removedAutoTracks...)
			state.Doc = next
			state.Revision++
			state.syncActiveSequence(workflow.Now(ctx).UnixMilli())
			updatesInRun++
			return DeleteTranscriptRangesResult{
				Revision:                state.Revision,
				Doc:                     state.Doc,
				Summary:                 summary,
				DeletedRanges:           compiled.DeletedRanges,
				DeletedDurationInFrames: compiled.DeletedDurationInFrames,
			}, nil
		},
		workflow.UpdateHandlerOptions{
			Validator: func(_ workflow.Context, req DeleteTranscriptRangesRequest) error {
				return ValidateDeleteTranscriptRangesRequest(req)
			},
		},
	)
	if err != nil {
		return err
	}

	err = workflow.SetUpdateHandlerWithOptions(ctx, UpdateRestoreTimeline,
		func(ctx workflow.Context, req RestoreTimelineRequest) (RestoreTimelineResult, error) {
			if err := requireActiveTimeline(state, req.TimelineID); err != nil {
				return RestoreTimelineResult{}, err
			}
			if req.BaseRevision != state.Revision {
				return RestoreTimelineResult{}, temporal.NewApplicationErrorWithOptions(
					fmt.Sprintf("base revision %d does not match latest %d", req.BaseRevision, state.Revision),
					ErrTypeRevisionConflict,
					temporal.ApplicationErrorOptions{
						NonRetryable: true,
						Details:      []any{RevisionConflictDetails{LatestRevision: state.Revision}},
					},
				)
			}
			next := req.Doc.Clone()
			currentMedia := make(map[string]timeline.Media, len(state.Doc.Media))
			for _, media := range state.Doc.Media {
				currentMedia[media.MediaID] = media
			}
			for index := range next.Media {
				media := &next.Media[index]
				current, stillPresent := currentMedia[media.MediaID]
				if !stillPresent || current.Generation != media.Generation ||
					current.BlobKey != media.BlobKey {
					mediaGenerations[media.MediaID]++
					media.Generation = mediaGenerations[media.MediaID]
				} else if media.Generation > mediaGenerations[media.MediaID] {
					mediaGenerations[media.MediaID] = media.Generation
				}
			}
			if validationErr := timeline.ValidateDoc(next); validationErr != nil {
				return RestoreTimelineResult{}, temporal.NewApplicationErrorWithOptions(
					validationErr.Error(), ErrTypeOpFailed,
					temporal.ApplicationErrorOptions{
						NonRetryable: true,
						Details:      []any{*validationErr},
					},
				)
			}
			state.Doc = next
			state.Revision++
			state.syncActiveSequence(workflow.Now(ctx).UnixMilli())
			updatesInRun++
			return RestoreTimelineResult{Revision: state.Revision, Doc: state.Doc}, nil
		},
		workflow.UpdateHandlerOptions{
			Validator: func(_ workflow.Context, req RestoreTimelineRequest) error {
				return ValidateRestoreTimelineRequest(req)
			},
		},
	)
	if err != nil {
		return err
	}

	if err := workflow.SetUpdateHandler(ctx, UpdateCreateShare,
		func(_ workflow.Context, req CreateShareRequest) error {
			if req.TokenHash == "" || req.NowUnixMs <= 0 || req.ExpiresAtUnixMs <= req.NowUnixMs {
				return errors.New("tokenHash and a future expiresAtUnixMs are required")
			}
			for tokenHash, expiry := range shareGrants {
				if expiry <= req.NowUnixMs {
					delete(shareGrants, tokenHash)
				}
			}
			if _, exists := shareGrants[req.TokenHash]; !exists && len(shareGrants) >= maxShareGrantsPerProject {
				return fmt.Errorf("active share limit %d reached", maxShareGrantsPerProject)
			}
			shareGrants[req.TokenHash] = req.ExpiresAtUnixMs
			updatesInRun++
			return nil
		}); err != nil {
		return err
	}
	if err := workflow.SetUpdateHandler(ctx, UpdateRevokeShare,
		func(_ workflow.Context, req RevokeShareRequest) error {
			if req.TokenHash == "" {
				return errors.New("tokenHash is required")
			}
			delete(shareGrants, req.TokenHash)
			updatesInRun++
			return nil
		}); err != nil {
		return err
	}

	workflow.Go(ctx, func(ctx workflow.Context) {
		workflow.GetSignalChannel(ctx, SignalDeleteTimeline).Receive(ctx, nil)
		if !init.DisableMediaCleanup {
			for _, media := range state.allMedia() {
				if media.BlobKey == "" && media.Processing == nil {
					continue
				}
				pendingDeletes = appendDeleteRequests(pendingDeletes, []DeleteMediaBlobRequest{{
					ProjectID: state.ProjectID, MediaID: media.MediaID, BlobKey: media.BlobKey,
					Generation: media.Generation,
				}})
			}
		}
		deleted = true
	})

	for {
		prevRev := state.Revision
		wait := func() bool {
			return deleted || state.Revision != prevRev || updatesInRun >= threshold
		}
		var err error
		if deleted && len(pendingDeletes) > 0 {
			_, err = workflow.AwaitWithTimeout(ctx, 30*time.Second, wait)
		} else {
			err = workflow.Await(ctx, wait)
		}
		if err != nil {
			return err
		}

		// 状态有变化就快照（多次 Update 自然合并成一次快照）。
		if !init.DisableSnapshots && state.Revision != lastSnapshotRev {
			ao := workflow.ActivityOptions{
				StartToCloseTimeout: 30 * time.Second,
				RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
			}
			actx := workflow.WithActivityOptions(ctx, ao)
			// 快照失败只记日志，不阻塞、不失败主流程。
			if serr := workflow.ExecuteActivity(actx, SnapshotActivityName, SnapshotRequest{
				ProjectID: state.ProjectID, Revision: state.Revision, Doc: state.Doc,
			}).Get(ctx, nil); serr != nil {
				workflow.GetLogger(ctx).Warn("timeline snapshot failed", "projectId", state.ProjectID, "revision", state.Revision, "error", serr)
			}
			lastSnapshotRev = state.Revision
		}

		// Removed media is retained for the lifetime of the project so a
		// revision-checked snapshot restore can never recreate a dangling blob
		// reference. Project deletion drains both retained and current media.
		if deleted && len(pendingDeletes) > 0 {
			requests := pendingDeletes
			pendingDeletes = nil
			ao := workflow.ActivityOptions{
				StartToCloseTimeout: 30 * time.Second,
				RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
			}
			actx := workflow.WithActivityOptions(ctx, ao)
			for _, req := range requests {
				executeReq := req
				for _, current := range state.allMedia() {
					if current.MediaID == req.MediaID && current.Generation != req.Generation &&
						current.BlobKey == req.BlobKey {
						// A later incarnation reused the same source object.
						// Keep the source while still deleting generation-scoped
						// derivatives from the old incarnation.
						executeReq.BlobKey = ""
						break
					}
				}
				if derr := workflow.ExecuteActivity(actx, DeleteMediaBlobActivityName, executeReq).Get(ctx, nil); derr != nil {
					pendingDeletes = appendDeleteRequests(pendingDeletes, []DeleteMediaBlobRequest{req})
					workflow.GetLogger(ctx).Warn("media blob cleanup failed",
						"projectId", req.ProjectID, "mediaId", req.MediaID, "blobKey", req.BlobKey, "error", derr)
				}
			}
		}

		if deleted && len(pendingDeletes) == 0 {
			return nil
		}
		if updatesInRun >= threshold {
			// 等所有在途 handler 收尾后翻新 Run，状态原样带过去。
			if err := workflow.Await(ctx, func() bool { return workflow.AllHandlersFinished(ctx) }); err != nil {
				return err
			}
			return workflow.NewContinueAsNewError(ctx, TimelineEntityWorkflow, TimelineInit{
				ProjectID:           state.ProjectID,
				Doc:                 state.Doc,
				Revision:            state.Revision,
				ActiveTimelineID:    state.ActiveTimelineID,
				Timelines:           state.Timelines,
				ProjectRevision:     state.ProjectRevision,
				AgentKSessionID:     state.AgentKSessionID,
				DisableSnapshots:    init.DisableSnapshots,
				DisableMediaCleanup: init.DisableMediaCleanup,
				ShareGrants:         shareGrants,
				MaxUpdatesPerRun:    init.MaxUpdatesPerRun,
				MediaGenerations:    mediaGenerations,
				PendingDeletes:      pendingDeletes,
			})
		}
	}
}

// ValidateApplyOpsRequest performs state-independent validation shared by HTTP,
// Agent Activities, and the Workflow Update validator. State-dependent checks
// remain inside timeline.ApplyOps so the batch is still applied atomically.
func ValidateApplyOpsRequest(req ApplyOpsRequest) error {
	if req.TimelineID != "" {
		if err := ValidateClientMutationID(req.TimelineID); err != nil {
			return fmt.Errorf("invalid timelineId: %w", err)
		}
	}
	if len(req.Operations) == 0 {
		return errors.New("operations must not be empty")
	}
	if len(req.Operations) > timeline.MaxOpsPerBatch {
		return fmt.Errorf("operations %d exceeds max %d", len(req.Operations), timeline.MaxOpsPerBatch)
	}
	if req.Source != "system" && req.Source != "agent" && req.Source != "user" {
		return errors.New("source must be system, agent, or user")
	}
	if req.Source != "system" {
		return validateNonSystemMediaACL(req.Source, req.Operations)
	}
	return nil
}

func ValidateDeleteTranscriptRangesRequest(req DeleteTranscriptRangesRequest) error {
	if req.TimelineID != "" {
		if err := ValidateClientMutationID(req.TimelineID); err != nil {
			return fmt.Errorf("invalid timelineId: %w", err)
		}
	}
	if req.Source != "agent" && req.Source != "user" {
		return errors.New("source must be agent or user")
	}
	if req.Mode != "ripple" {
		return errors.New("mode must be ripple")
	}
	if len(req.Ranges) == 0 || len(req.Ranges) > timeline.MaxTranscriptDeleteRanges {
		return fmt.Errorf(
			"ranges must contain 1..%d entries",
			timeline.MaxTranscriptDeleteRanges,
		)
	}
	for index, selected := range req.Ranges {
		if selected.FromOccurrenceID == "" || selected.ToOccurrenceID == "" {
			return fmt.Errorf("ranges[%d] requires fromOccurrenceId and toOccurrenceId", index)
		}
		if len(selected.FromOccurrenceID) > timeline.MaxOccurrenceIDLength ||
			len(selected.ToOccurrenceID) > timeline.MaxOccurrenceIDLength {
			return fmt.Errorf("ranges[%d] occurrenceId is too long", index)
		}
	}
	return nil
}

func ValidateRestoreTimelineRequest(req RestoreTimelineRequest) error {
	if req.TimelineID != "" {
		if err := ValidateClientMutationID(req.TimelineID); err != nil {
			return fmt.Errorf("invalid timelineId: %w", err)
		}
	}
	if req.Source != "user" {
		return errors.New("source must be user")
	}
	if validationErr := timeline.ValidateDoc(req.Doc); validationErr != nil {
		return validationErr
	}
	return nil
}

func validateNonSystemMediaACL(source string, operations []timeline.Operation) error {
	for _, op := range operations {
		if op.MediaGeneration != nil {
			return errors.New("media generation may only be supplied by system workflows")
		}
		switch op.Op {
		case timeline.OpSetTranscript:
			return errors.New("setTranscript may only be performed by system workflows")
		case timeline.OpAddMedia:
			if source != "agent" {
				return errors.New("addMedia may only be performed by agent or system workflows")
			}
			if op.Media == nil {
				continue
			}
			media := op.Media
			if media.BlobKey != "" || media.Generation != 0 || media.Probe != nil || media.ThumbnailKey != "" ||
				media.ThumbnailURI != "" || media.WaveformPeaks != nil ||
				media.Transcript != nil || media.Processing != nil ||
				media.UnderstandingText != "" {
				return errors.New("media storage and processing fields may only be supplied by system workflows")
			}
		case timeline.OpUpdateMedia:
			if op.MediaPatch == nil {
				continue
			}
			patch := op.MediaPatch
			if patch.URI != nil || patch.BlobKey != nil ||
				patch.DurationInFrames != nil || patch.Probe != nil ||
				patch.ThumbnailKey != nil || patch.ThumbnailURI != nil ||
				patch.WaveformPeaks != nil || patch.Transcript != nil ||
				patch.Processing != nil || patch.UnderstandingText != nil {
				return errors.New("media storage and processing fields may only be updated by system workflows")
			}
		}
	}
	return nil
}

func appendDeleteRequests(existing, additions []DeleteMediaBlobRequest) []DeleteMediaBlobRequest {
	seen := make(map[string]bool, len(existing)+len(additions))
	result := make([]DeleteMediaBlobRequest, 0, len(existing)+len(additions))
	for _, req := range append(append([]DeleteMediaBlobRequest(nil), existing...), additions...) {
		key := fmt.Sprintf("%s\x00%s\x00%d\x00%s", req.ProjectID, req.MediaID, req.Generation, req.BlobKey)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, req)
	}
	return result
}
