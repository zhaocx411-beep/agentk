package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"

	"agentcut/entity"
	"agentcut/platform/blob"
	"agentcut/separateaudio"
	"agentcut/timeline"
)

type separateAudioRequest struct {
	ClientMutationID string `json:"clientMutationId"`
	TimelineID       string `json:"timelineId,omitempty"`
	BaseRevision     uint64 `json:"baseRevision"`
}

func (s *Server) handleStartSeparateAudio(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return
	}
	if s.Blob == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"code": "BLOB_NOT_CONFIGURED", "error": "blob storage is not configured",
			"missing": blob.MissingConfiguration(),
		})
		return
	}
	itemID := strings.TrimSpace(r.PathValue("itemID"))
	if itemID == "" || len(itemID) > 128 {
		writeStructuredError(w, http.StatusBadRequest, "BAD_REQUEST", "valid itemId is required")
		return
	}
	var req separateAudioRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeStructuredError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}
	if err := entity.ValidateClientMutationID(req.ClientMutationID); err != nil {
		writeStructuredError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	state, err := s.getTimelineState(r.Context(), ref.ProjectID)
	if err != nil {
		writeStructuredError(w, http.StatusBadGateway, "TIMELINE_UNAVAILABLE", "failed to load timeline")
		return
	}
	if state.Revision != req.BaseRevision {
		writeJSON(w, http.StatusConflict, map[string]any{
			"code": entity.ErrTypeRevisionConflict, "latestRevision": state.Revision,
			"error": "baseRevision does not match latest revision",
		})
		return
	}
	if req.TimelineID != "" && req.TimelineID != state.ActiveTimelineID {
		writeStructuredError(
			w, http.StatusConflict, entity.ErrTypeTimelineNotActive,
			"timeline is no longer active",
		)
		return
	}
	item, media, track := separationSource(state.Doc, itemID)
	switch {
	case item == nil || media == nil || item.Kind != timeline.ItemVideo || media.Kind != timeline.MediaVideo:
		writeStructuredError(w, http.StatusNotFound, "ITEM_NOT_FOUND", "video item not found")
		return
	case media.Probe == nil || !media.Probe.HasAudio:
		writeStructuredError(w, http.StatusUnprocessableEntity, "NO_AUDIO", "video has no audio stream")
		return
	case track == nil || track.Locked:
		writeStructuredError(w, http.StatusUnprocessableEntity, "LOCKED_TRACK", "source video track is locked")
		return
	}
	workflowID := separateaudio.WorkflowIDForTimeline(ref.ProjectID, req.TimelineID, itemID)
	_, err = s.Temporal.ExecuteWorkflow(r.Context(), client.StartWorkflowOptions{
		ID:                    workflowID,
		TaskQueue:             s.TaskQueue,
		WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
	}, separateaudio.WorkflowName, separateaudio.Input{
		ProjectID: ref.ProjectID, TimelineID: req.TimelineID,
		ItemID: itemID, BaseRevision: req.BaseRevision,
		ClientMutation: req.ClientMutationID,
	})
	if err != nil {
		var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
		if !errors.As(err, &alreadyStarted) {
			writeStructuredError(w, http.StatusBadGateway, "AUDIO_SEPARATION_START_FAILED", "failed to start audio separation")
			return
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"workflowId": workflowID, "status": "running",
	})
}

func (s *Server) handleGetSeparateAudio(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return
	}
	itemID := strings.TrimSpace(r.PathValue("itemID"))
	timelineID := strings.TrimSpace(r.URL.Query().Get("timelineId"))
	workflowID := separateaudio.WorkflowIDForTimeline(ref.ProjectID, timelineID, itemID)
	description, err := s.Temporal.DescribeWorkflowExecution(r.Context(), workflowID, "")
	if err != nil {
		if isNotFound(err) {
			writeStructuredError(w, http.StatusNotFound, "AUDIO_SEPARATION_NOT_FOUND", "audio separation not found")
			return
		}
		writeStructuredError(w, http.StatusBadGateway, "TEMPORAL_UNAVAILABLE", "failed to inspect audio separation")
		return
	}
	status := description.WorkflowExecutionInfo.GetStatus()
	if status == enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING {
		writeJSON(w, http.StatusOK, map[string]any{"workflowId": workflowID, "status": "running"})
		return
	}
	if status == enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED {
		var output separateaudio.Output
		if err := s.Temporal.GetWorkflow(r.Context(), workflowID, "").Get(r.Context(), &output); err != nil {
			writeStructuredError(w, http.StatusBadGateway, "AUDIO_SEPARATION_RESULT_FAILED", "failed to load audio separation result")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"workflowId": workflowID, "status": "completed", "result": output,
		})
		return
	}
	runErr := s.Temporal.GetWorkflow(r.Context(), workflowID, "").Get(r.Context(), nil)
	code := "AUDIO_SEPARATION_FAILED"
	message := "audio separation failed"
	var appErr *temporal.ApplicationError
	if errors.As(runErr, &appErr) {
		if appErr.Type() != "" {
			code = appErr.Type()
		}
		message = appErr.Message()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"workflowId": workflowID, "status": "failed", "code": code,
		"error": message, "workflowStatus": status.String(),
	})
}

func separationSource(doc timeline.Doc, itemID string) (*timeline.Item, *timeline.Media, *timeline.Track) {
	var item *timeline.Item
	for index := range doc.Items {
		if doc.Items[index].ItemID == itemID {
			item = &doc.Items[index]
			break
		}
	}
	if item == nil {
		return nil, nil, nil
	}
	var media *timeline.Media
	for index := range doc.Media {
		if doc.Media[index].MediaID == item.MediaID {
			media = &doc.Media[index]
			break
		}
	}
	var track *timeline.Track
	for index := range doc.Tracks {
		if doc.Tracks[index].TrackID == item.TrackID {
			track = &doc.Tracks[index]
			break
		}
	}
	return item, media, track
}
