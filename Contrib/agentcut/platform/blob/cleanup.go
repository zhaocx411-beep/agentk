package blob

import (
	"context"
	"fmt"
	"strings"

	"agentcut/entity"
	"agentcut/timeline"
)

type CleanupActivities struct {
	Store Storage
}

func (a *CleanupActivities) DeleteMediaBlob(ctx context.Context, req entity.DeleteMediaBlobRequest) error {
	if a.Store == nil {
		return fmt.Errorf("media cleanup storage is not configured")
	}
	if req.ProjectID == "" {
		return fmt.Errorf("projectId and mediaId are required")
	}
	if err := timeline.ValidateMediaID(req.MediaID); err != nil {
		return err
	}
	prefix := fmt.Sprintf("agentcut/%s/source/%s/", req.ProjectID, req.MediaID)
	keys := []string{
		ThumbnailKeyForGeneration(req.ProjectID, req.MediaID, req.Generation),
		ASRAudioKeyForGeneration(req.ProjectID, req.MediaID, req.Generation),
		AudioPreviewKeyForGeneration(req.ProjectID, req.MediaID, req.Generation),
	}
	// Agent-K generated source artifacts may live outside AgentCut's owned source
	// prefix. Never delete those shared objects; only reclaim our derivatives.
	if strings.HasPrefix(req.BlobKey, prefix) {
		keys = append([]string{req.BlobKey}, keys...)
	}
	for _, key := range keys {
		if err := a.Store.Delete(ctx, key); err != nil {
			return fmt.Errorf("delete media blob %q: %w", key, err)
		}
	}
	return nil
}
