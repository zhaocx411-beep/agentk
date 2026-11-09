package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.temporal.io/sdk/client"

	"agentcut/entity"
	"agentcut/timeline"
)

const shareTTL = 7 * 24 * time.Hour

func newShareToken(projectID string) (string, error) {
	var secret [24]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", err
	}
	projectPart := base64.RawURLEncoding.EncodeToString([]byte(projectID))
	return "sh_" + projectPart + "." + hex.EncodeToString(secret[:]), nil
}

func parseShareToken(token string) (string, bool) {
	if !strings.HasPrefix(token, "sh_") {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(token, "sh_"), ".")
	if len(parts) != 2 || len(parts[1]) != 48 {
		return "", false
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(raw) == 0 {
		return "", false
	}
	return string(raw), true
}

func shareTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Server) handleCreateShare(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return
	}
	token, err := newShareToken(ref.ProjectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate share token")
		return
	}
	now := time.Now().UTC()
	expiresAt := now.Add(shareTTL)
	handle, err := s.Temporal.UpdateWorkflow(r.Context(), client.UpdateWorkflowOptions{
		WorkflowID:   entity.TimelineWorkflowID(ref.ProjectID),
		UpdateName:   entity.UpdateCreateShare,
		UpdateID:     "share:create:" + shareTokenHash(token),
		WaitForStage: client.WorkflowUpdateStageCompleted,
		Args: []any{entity.CreateShareRequest{
			TokenHash: shareTokenHash(token), NowUnixMs: now.UnixMilli(),
			ExpiresAtUnixMs: expiresAt.UnixMilli(),
		}},
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to create project share")
		return
	}
	if err := handle.Get(r.Context(), nil); err != nil {
		writeError(w, http.StatusBadGateway, "failed to create project share")
		return
	}
	url := fmt.Sprintf("/v1/shared/%s/timeline", token)
	writeJSON(w, http.StatusCreated, map[string]any{
		"shareId":   token,
		"token":     token,
		"url":       url,
		"expiresAt": expiresAt.Format(time.RFC3339),
	})
}

func (s *Server) handleRevokeShare(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return
	}
	token := r.PathValue("token")
	tokenProjectID, valid := parseShareToken(token)
	if !valid || tokenProjectID != ref.ProjectID {
		writeError(w, http.StatusNotFound, "share not found")
		return
	}
	handle, err := s.Temporal.UpdateWorkflow(r.Context(), client.UpdateWorkflowOptions{
		WorkflowID:   entity.TimelineWorkflowID(ref.ProjectID),
		UpdateName:   entity.UpdateRevokeShare,
		UpdateID:     "share:revoke:" + shareTokenHash(token),
		WaitForStage: client.WorkflowUpdateStageCompleted,
		Args:         []any{entity.RevokeShareRequest{TokenHash: shareTokenHash(token)}},
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to revoke project share")
		return
	}
	if err := handle.Get(r.Context(), nil); err != nil {
		writeError(w, http.StatusBadGateway, "failed to revoke project share")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetSharedTimeline(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	projectID, ok := parseShareToken(token)
	if !ok {
		writeError(w, http.StatusNotFound, "share not found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	ev, err := s.Temporal.QueryWorkflow(ctx, entity.TimelineWorkflowID(projectID), "",
		entity.QuerySharedTimeline, entity.SharedTimelineRequest{
			TokenHash: shareTokenHash(token), NowUnixMs: time.Now().UnixMilli(),
		})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "share not found")
		} else {
			writeError(w, http.StatusBadGateway, "failed to load shared timeline")
		}
		return
	}
	var result entity.SharedTimelineResult
	if err := ev.Get(&result); err != nil {
		writeError(w, http.StatusBadGateway, "failed to decode shared timeline")
		return
	}
	switch result.Status {
	case "valid":
		writeJSON(w, http.StatusOK, map[string]any{
			"revision":         result.State.Revision,
			"doc":              result.State.Doc,
			"durationInFrames": timeline.DocDurationInFrames(result.State.Doc),
			"readOnly":         true,
		})
	case "expired":
		writeJSON(w, http.StatusGone, map[string]any{
			"error": "share link has expired", "code": "SHARE_EXPIRED",
		})
	default:
		writeError(w, http.StatusNotFound, "share not found")
	}
}
