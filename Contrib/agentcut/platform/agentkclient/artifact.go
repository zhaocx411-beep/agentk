package agentkclient

import (
	"context"
	"fmt"
	"strings"

	"agentcut/platform/artifactarchive"
	"agentcut/timeline"
)

const (
	artifactEventPageSize = int32(100)
	maxArtifactEventPages = 20
)

// VerifyArtifact proves provenance from Agent-K's durable event log instead of
// trusting a model-supplied URL or a global Blob prefix.
func (c *Client) VerifyArtifact(
	ctx context.Context,
	sessionID, uri string,
	kind timeline.MediaKind,
) (artifactarchive.Provenance, error) {
	if c == nil {
		return artifactarchive.Provenance{}, fmt.Errorf("Agent-K client is not configured")
	}
	if !strings.HasPrefix(sessionID, "ses_") || strings.TrimSpace(uri) == "" {
		return artifactarchive.Provenance{}, fmt.Errorf("session and artifact URI are required")
	}
	pageToken := ""
	for page := 0; page < maxArtifactEventPages; page++ {
		response, err := c.ListEvents(ctx, sessionID, artifactEventPageSize, pageToken)
		if err != nil {
			return artifactarchive.Provenance{}, fmt.Errorf("list Agent-K artifact events: %w", err)
		}
		for _, event := range response.GetEvents() {
			succeeded := event.GetToolExecSucceeded()
			if succeeded == nil {
				continue
			}
			for _, artifact := range succeeded.GetArtifacts() {
				if artifact.GetUri() != uri || !artifactTypeMatches(kind, artifact.GetType()) {
					continue
				}
				if strings.TrimSpace(event.GetExecutionId()) == "" {
					return artifactarchive.Provenance{}, fmt.Errorf("artifact event has no executionId")
				}
				return artifactarchive.Provenance{
					ExecutionID: event.GetExecutionId(),
					ToolCallID:  succeeded.GetToolCallId(),
					Type:        artifact.GetType(),
				}, nil
			}
		}
		pageToken = response.GetNextPageToken()
		if pageToken == "" {
			break
		}
	}
	return artifactarchive.Provenance{}, fmt.Errorf(
		"artifact was not emitted by a successful tool call in the project Agent-K session")
}

func artifactTypeMatches(kind timeline.MediaKind, artifactType string) bool {
	value := strings.ToLower(strings.TrimSpace(artifactType))
	switch kind {
	case timeline.MediaAudio:
		return value == "audio"
	case timeline.MediaVideo:
		return value == "video"
	case timeline.MediaImage:
		return value == "image"
	default:
		return false
	}
}
