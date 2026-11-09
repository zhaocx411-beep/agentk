package api

import (
	"context"
	"fmt"
	"net/http"

	"agentcut/platform/blob"
	"agentcut/preparemedia"
	"agentcut/timeline"
)

func (s *Server) startMediaPrepare(
	ctx context.Context,
	projectID, mediaID, agentKSessionID string,
) (bool, error) {
	state, err := s.getTimelineState(ctx, projectID)
	if err != nil {
		return false, err
	}
	for _, media := range state.Doc.Media {
		if media.MediaID == mediaID {
			input := preparemedia.InputFromEnv(projectID, mediaID, media.Generation)
			input.AgentKSessionID = agentKSessionID
			return preparemedia.Start(ctx, s.Temporal, s.TaskQueue, input)
		}
	}
	return false, fmt.Errorf("media %q not found", mediaID)
}

func (s *Server) handlePrepareMedia(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return
	}
	mediaID := r.PathValue("mediaID")
	if err := timeline.ValidateMediaID(mediaID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	state, err := s.getTimelineState(r.Context(), ref.ProjectID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to load timeline")
		return
	}
	found := false
	var generation uint64
	for _, media := range state.Doc.Media {
		if media.MediaID != mediaID {
			continue
		}
		found = true
		generation = media.Generation
		if !blob.IsProjectMediaSourceKey(ref.ProjectID, mediaID, media.BlobKey) && ref.SessionID == "" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"error": "external media cannot be archived because this project has no Agent-K session",
				"code":  "ARTIFACT_SESSION_REQUIRED",
			})
			return
		}
		break
	}
	if !found {
		writeError(w, http.StatusNotFound, "media not found")
		return
	}
	started, err := s.startMediaPrepare(r.Context(), ref.ProjectID, mediaID, ref.SessionID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to start media preparation: "+err.Error())
		return
	}
	status := "already_started"
	if started {
		status = "started"
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"workflowId": preparemedia.WorkflowID(ref.ProjectID, mediaID, generation),
		"status":     status,
	})
}
