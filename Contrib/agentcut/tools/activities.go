package tools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"

	"agentcut/entity"
	"agentcut/platform/artifactarchive"
	"agentcut/preparemedia"
	"agentcut/timeline"
)

// TemporalClient 是 Activities 依赖的最小 Temporal 客户端切面（便于单测替身）。
// *client.Client 天然满足。
type TemporalClient interface {
	UpdateWorkflow(ctx context.Context, options client.UpdateWorkflowOptions) (client.WorkflowUpdateHandle, error)
	QueryWorkflow(ctx context.Context, workflowID string, runID string, queryType string, args ...any) (converter.EncodedValue, error)
	ExecuteWorkflow(ctx context.Context, options client.StartWorkflowOptions, workflow interface{}, args ...interface{}) (client.WorkflowRun, error)
}

// Activities 承载全部 contrib.agentcut.* 工具的 Activity 实现。
type Activities struct {
	Temporal  TemporalClient
	TaskQueue string
	SessionID func(context.Context) string
}

// TimelineGet 实现 contrib.agentcut.timeline_get：Query 对应实体 Workflow。
func (a *Activities) TimelineGet(ctx context.Context, in GetInput) (GetOutput, error) {
	if in.ProjectID == "" {
		return GetOutput{}, temporal.NewNonRetryableApplicationError("projectId is required", "BAD_REQUEST", nil)
	}
	ev, err := a.Temporal.QueryWorkflow(ctx, entity.TimelineWorkflowID(in.ProjectID), "", entity.QueryGetState)
	if err != nil {
		return GetOutput{}, fmt.Errorf("query timeline for project %s: %w", in.ProjectID, err)
	}
	var st entity.TimelineState
	if err := ev.Get(&st); err != nil {
		return GetOutput{}, err
	}
	return GetOutput{
		Revision:         st.Revision,
		DurationInFrames: timeline.DocDurationInFrames(st.Doc),
		Doc:              st.Doc,
	}, nil
}

// TimelineEditClips 实现 contrib.agentcut.timeline_edit_clips。
func (a *Activities) TimelineEditClips(ctx context.Context, in EditInput) (EditOutput, error) {
	return a.applyEdit(ctx, in, editClipsAllowed, "timeline_edit_clips")
}

// TimelineEditTracks 实现 contrib.agentcut.timeline_edit_tracks。
func (a *Activities) TimelineEditTracks(ctx context.Context, in EditInput) (EditOutput, error) {
	return a.applyEdit(ctx, in, editTracksAllowed, "timeline_edit_tracks")
}

// TimelineEditMedia 实现 contrib.agentcut.timeline_edit_media。
func (a *Activities) TimelineEditMedia(ctx context.Context, in EditInput) (EditOutput, error) {
	sessionID := agentKSessionID(ctx)
	if a.SessionID != nil {
		sessionID = a.SessionID(ctx)
	}
	if sessionID == "" {
		return EditOutput{
			OK: false, Code: string(timeline.ErrBadRequest),
			Error: "timeline_edit_media must run inside a project-bound Agent-K session",
		}, nil
	}
	value, err := a.Temporal.QueryWorkflow(
		ctx, entity.TimelineWorkflowID(in.ProjectID), "", entity.QueryGetState)
	if err != nil {
		return EditOutput{}, fmt.Errorf("verify project Agent-K session: %w", err)
	}
	var state entity.TimelineState
	if err := value.Get(&state); err != nil {
		return EditOutput{}, fmt.Errorf("decode project Agent-K session: %w", err)
	}
	if state.AgentKSessionID == "" || state.AgentKSessionID != sessionID {
		return EditOutput{
			OK: false, Code: string(timeline.ErrBadRequest),
			Error: "projectId is not bound to the current Agent-K session",
		}, nil
	}

	normalized := in
	normalized.Operations = append([]timeline.Operation(nil), in.Operations...)
	var addedMediaIDs []string
	for index := range normalized.Operations {
		op := &normalized.Operations[index]
		if op.Op == timeline.OpUpdateMedia {
			patch := op.MediaPatch
			if patch == nil || patch.DisplayName == nil {
				return EditOutput{
					OK: false, Code: string(timeline.ErrBadRequest),
					Error: "timeline_edit_media updateMedia only supports mediaPatch.displayName",
				}, nil
			}
			if patch.URI != nil || patch.BlobKey != nil ||
				patch.DurationInFrames != nil || patch.Probe != nil ||
				patch.ThumbnailKey != nil || patch.ThumbnailURI != nil ||
				patch.WaveformPeaks != nil || patch.Transcript != nil ||
				patch.Processing != nil || patch.UnderstandingText != nil {
				return EditOutput{
					OK: false, Code: string(timeline.ErrBadRequest),
					Error: "timeline_edit_media updateMedia may only change mediaPatch.displayName",
				}, nil
			}
		}
		if op.Op != timeline.OpAddMedia || op.Media == nil {
			continue
		}
		media := *op.Media
		if err := artifactarchive.ValidateURL(media.URI); err != nil {
			return EditOutput{OK: false, Code: string(timeline.ErrBadRequest), Error: err.Error()}, nil
		}
		// Agent-supplied storage metadata is never authoritative. The
		// preparation Workflow verifies this exact URI against the
		// project-bound Agent-K event log, streams it into the AgentCut
		// project namespace, then writes BlobKey/URI as source=system.
		media.BlobKey = ""
		// Generated media must not smuggle system-owned preprocessing results.
		// The asynchronous workflow derives these fields from the managed bytes.
		media.Probe = nil
		media.ThumbnailKey = ""
		media.ThumbnailURI = ""
		media.WaveformPeaks = nil
		media.Transcript = nil
		media.UnderstandingText = ""
		media.Processing = nil
		media.Generation = 0
		op.Media = &media
		addedMediaIDs = append(addedMediaIDs, media.MediaID)
	}
	out, err := a.applyEdit(ctx, normalized, editMediaAllowed, "timeline_edit_media")
	if err != nil || !out.OK {
		return out, err
	}
	for _, mediaID := range addedMediaIDs {
		generation := out.addedMediaGenerations[mediaID]
		prepareInput := preparemedia.InputFromEnv(in.ProjectID, mediaID, generation)
		prepareInput.AgentKSessionID = sessionID
		if _, err := preparemedia.Start(ctx, a.Temporal, a.TaskQueue, prepareInput); err != nil {
			// The addMedia update is already durably committed. Returning an
			// Activity error would retry the whole tool and turn success into an
			// ID conflict, so preparation start is best effort here.
			log.Printf("ERROR: start media preparation failed after committed addMedia projectId=%s mediaId=%s generation=%d error=%v",
				in.ProjectID, mediaID, generation, err)
		}
	}
	return out, nil
}

// TimelineSetCaptions 实现 contrib.agentcut.timeline_set_captions。
func (a *Activities) TimelineSetCaptions(ctx context.Context, in SetCaptionsInput) (EditOutput, error) {
	return a.applyEdit(ctx, EditInput{
		ProjectID:    in.ProjectID,
		BaseRevision: in.BaseRevision,
		Operations: []timeline.Operation{{
			Op:       timeline.OpSetCaptions,
			TrackID:  in.TrackID,
			Captions: in.Captions,
		}},
	}, setCaptionsAllowed, "timeline_set_captions")
}

func (a *Activities) TimelineEditTranscript(
	ctx context.Context,
	in TranscriptEditInput,
) (TranscriptEditOutput, error) {
	if in.ProjectID == "" {
		return TranscriptEditOutput{}, temporal.NewNonRetryableApplicationError(
			"projectId is required", "BAD_REQUEST", nil,
		)
	}
	sessionID := agentKSessionID(ctx)
	if a.SessionID != nil {
		sessionID = a.SessionID(ctx)
	}
	if sessionID == "" {
		return TranscriptEditOutput{
			OK: false, Action: in.Action, Code: string(timeline.ErrBadRequest),
			Error: "timeline_edit_transcript must run inside a project-bound Agent-K session",
		}, nil
	}
	value, err := a.Temporal.QueryWorkflow(
		ctx, entity.TimelineWorkflowID(in.ProjectID), "", entity.QueryGetState,
	)
	if err != nil {
		return TranscriptEditOutput{}, fmt.Errorf("verify project Agent-K session: %w", err)
	}
	var state entity.TimelineState
	if err := value.Get(&state); err != nil {
		return TranscriptEditOutput{}, fmt.Errorf("decode project Agent-K session: %w", err)
	}
	if state.AgentKSessionID == "" || state.AgentKSessionID != sessionID {
		return TranscriptEditOutput{
			OK: false, Action: in.Action, Code: string(timeline.ErrBadRequest),
			Error: "projectId is not bound to the current Agent-K session",
		}, nil
	}

	switch in.Action {
	case "view":
		if in.BaseRevision != 0 || len(in.Ranges) != 0 || in.Mode != "" {
			return TranscriptEditOutput{
				OK: false, Action: in.Action, Code: string(timeline.ErrBadRequest),
				Error: "view does not accept baseRevision, ranges, or mode",
			}, nil
		}
		limit := in.Limit
		if limit == 0 {
			limit = 200
		}
		if limit < 1 || limit > 500 {
			return TranscriptEditOutput{
				OK: false, Action: in.Action, Code: string(timeline.ErrBadRequest),
				Error: "limit must be between 1 and 500",
			}, nil
		}
		view, total, truncated, selectErr := timeline.SelectTranscriptView(
			in.ProjectID,
			state.Revision,
			state.Doc,
			in.FromOccurrenceID,
			in.ToOccurrenceID,
			limit,
		)
		if selectErr != nil {
			return TranscriptEditOutput{
				OK: false, Action: in.Action, Code: selectErr.Code, Error: selectErr.Message,
			}, nil
		}
		return TranscriptEditOutput{
			OK:               true,
			Action:           in.Action,
			TimelineRevision: view.TimelineRevision,
			Entries:          view.Entries,
			Total:            total,
			Truncated:        truncated,
			Warnings:         view.Warnings,
		}, nil
	case "delete":
		if in.FromOccurrenceID != "" || in.ToOccurrenceID != "" || in.Limit != 0 {
			return TranscriptEditOutput{
				OK: false, Action: in.Action, Code: string(timeline.ErrBadRequest),
				Error: "delete accepts selection endpoints only through ranges",
			}, nil
		}
		request := entity.DeleteTranscriptRangesRequest{
			BaseRevision: in.BaseRevision,
			Source:       "agent",
			Mode:         in.Mode,
			Ranges:       in.Ranges,
		}
		if err := entity.ValidateDeleteTranscriptRangesRequest(request); err != nil {
			return TranscriptEditOutput{
				OK: false, Action: in.Action, Code: string(timeline.ErrBadRequest), Error: err.Error(),
			}, nil
		}
		handle, err := a.Temporal.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
			WorkflowID: entity.TimelineWorkflowID(in.ProjectID),
			UpdateName: entity.UpdateDeleteTranscript,
			UpdateID: entity.TimelineMutationUpdateID(
				in.ProjectID, "agent", updateID(ctx),
			),
			WaitForStage: client.WorkflowUpdateStageCompleted,
			Args:         []any{request},
		})
		if err != nil {
			return TranscriptEditOutput{}, fmt.Errorf(
				"update transcript edit for project %s: %w", in.ProjectID, err,
			)
		}
		var result entity.DeleteTranscriptRangesResult
		if err := handle.Get(ctx, &result); err != nil {
			return mapTranscriptUpdateError(in.Action, err)
		}
		doc := result.Doc
		return TranscriptEditOutput{
			OK:                      true,
			Action:                  in.Action,
			Revision:                result.Revision,
			TimelineRevision:        result.Revision,
			Doc:                     &doc,
			Summary:                 &result.Summary,
			DeletedRanges:           result.DeletedRanges,
			DeletedDurationInFrames: result.DeletedDurationInFrames,
		}, nil
	default:
		return TranscriptEditOutput{
			OK: false, Action: in.Action, Code: string(timeline.ErrBadRequest),
			Error: "action must be view or delete",
		}, nil
	}
}

// applyEdit 是修改类工具的公共路径：白名单校验 → Update 转发 → 错误映射。
func (a *Activities) applyEdit(ctx context.Context, in EditInput, allowed map[string]bool, toolName string) (EditOutput, error) {
	if in.ProjectID == "" {
		return EditOutput{}, temporal.NewNonRetryableApplicationError("projectId is required", "BAD_REQUEST", nil)
	}
	if len(in.Operations) == 0 {
		return EditOutput{OK: false, Code: string(timeline.ErrBadRequest), Error: "operations must not be empty"}, nil
	}
	// op 白名单：模型不能借当前工具偷传其它类别的 op。
	for _, op := range in.Operations {
		if !allowed[op.Op] {
			return EditOutput{
				OK:    false,
				Code:  string(timeline.ErrUnsupported),
				Error: fmt.Sprintf("op %q is not allowed by tool %s", op.Op, toolName),
			}, nil
		}
	}

	request := entity.ApplyOpsRequest{
		BaseRevision: in.BaseRevision,
		Source:       "agent",
		Operations:   in.Operations,
	}
	if err := entity.ValidateApplyOpsRequest(request); err != nil {
		return EditOutput{OK: false, Code: string(timeline.ErrBadRequest), Error: err.Error()}, nil
	}
	handle, err := a.Temporal.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID: entity.TimelineWorkflowID(in.ProjectID),
		UpdateName: entity.UpdateApplyOps,
		// UpdateID 在 Activity 重试间稳定，Temporal 原生按它去重。
		UpdateID: entity.TimelineMutationUpdateID(
			in.ProjectID, "agent", updateID(ctx),
		),
		WaitForStage: client.WorkflowUpdateStageCompleted,
		Args:         []any{request},
	})
	if err != nil {
		return EditOutput{}, fmt.Errorf("update timeline for project %s: %w", in.ProjectID, err)
	}
	var res entity.ApplyOpsResult
	if err := handle.Get(ctx, &res); err != nil {
		return mapUpdateError(err)
	}
	return EditOutput{
		OK: true, Revision: res.Revision, Summary: &res.Summary,
		addedMediaGenerations: res.AddedMediaGenerations,
	}, nil
}

func mapTranscriptUpdateError(action string, err error) (TranscriptEditOutput, error) {
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) {
		return TranscriptEditOutput{}, err
	}
	switch appErr.Type() {
	case entity.ErrTypeRevisionConflict:
		var details entity.RevisionConflictDetails
		_ = appErr.Details(&details)
		return TranscriptEditOutput{
			OK: false, Action: action, Code: entity.ErrTypeRevisionConflict,
			Error:          "baseRevision 已过期，请重新 view 后使用新的工具调用重试",
			LatestRevision: details.LatestRevision,
		}, nil
	case entity.ErrTypeTranscriptEdit:
		var domainErr timeline.TranscriptEditError
		if detailErr := appErr.Details(&domainErr); detailErr == nil {
			return TranscriptEditOutput{
				OK: false, Action: action, Code: domainErr.Code, Error: domainErr.Message,
			}, nil
		}
		return TranscriptEditOutput{
			OK: false, Action: action, Code: entity.ErrTypeTranscriptEdit, Error: appErr.Message(),
		}, nil
	case entity.ErrTypeOpFailed:
		var opErr timeline.OpError
		if detailErr := appErr.Details(&opErr); detailErr == nil {
			return TranscriptEditOutput{
				OK: false, Action: action, Code: string(opErr.Code), Error: opErr.Error(),
			}, nil
		}
	}
	return TranscriptEditOutput{}, err
}

// mapUpdateError 把实体 Workflow 的领域错误映射成模型可自行纠正的结构化输出；
// 其余错误按 Activity 失败上抛（网络/找不到 Workflow 等值得重试的情况）。
func mapUpdateError(err error) (EditOutput, error) {
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) {
		return EditOutput{}, err
	}
	switch appErr.Type() {
	case entity.ErrTypeRevisionConflict:
		var details entity.RevisionConflictDetails
		_ = appErr.Details(&details)
		return EditOutput{
			OK:             false,
			Code:           entity.ErrTypeRevisionConflict,
			Error:          "baseRevision 已过期，请用 latestRevision 重试（必要时先 timeline_get 查看最新文档）",
			LatestRevision: details.LatestRevision,
		}, nil
	case entity.ErrTypeOpFailed:
		var oe timeline.OpError
		if derr := appErr.Details(&oe); derr == nil {
			return EditOutput{OK: false, Code: string(oe.Code), Error: oe.Error()}, nil
		}
		return EditOutput{OK: false, Code: entity.ErrTypeOpFailed, Error: appErr.Message()}, nil
	default:
		return EditOutput{}, err
	}
}

// updateID 生成在 Activity 重试间稳定的 Update ID：同一次工具调用（同一个
// Agent Loop 内的同一个 Activity）无论重试多少次都是同一个 ID。
func updateID(ctx context.Context) string {
	if !activity.IsActivity(ctx) {
		return ""
	}
	info := activity.GetInfo(ctx)
	return fmt.Sprintf("tool:%s:%s", info.WorkflowExecution.ID, info.ActivityID)
}

func agentKSessionID(ctx context.Context) string {
	if !activity.IsActivity(ctx) {
		return ""
	}
	workflowID := activity.GetInfo(ctx).WorkflowExecution.ID
	sessionID := strings.TrimPrefix(workflowID, "session/")
	if sessionID == workflowID || !strings.HasPrefix(sessionID, "ses_") {
		return ""
	}
	return sessionID
}
