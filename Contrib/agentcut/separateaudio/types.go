package separateaudio

import (
	"crypto/sha256"
	"encoding/hex"

	"agentcut/timeline"
)

const (
	WorkflowName        = "SeparateAudioWorkflow"
	ExtractActivityName = "SeparateAudio.Extract"
	CommitActivityName  = "SeparateAudio.Commit"
	CleanupActivityName = "SeparateAudio.Cleanup"
)

type Input struct {
	ProjectID      string `json:"projectId"`
	TimelineID     string `json:"timelineId,omitempty"`
	ItemID         string `json:"itemId"`
	BaseRevision   uint64 `json:"baseRevision"`
	ClientMutation string `json:"clientMutationId"`
}

type ExtractRequest struct {
	Input
}

type ExtractResult struct {
	AudioMedia  timeline.Media `json:"audioMedia"`
	AudioItemID string         `json:"audioItemId"`
	BlobKey     string         `json:"blobKey,omitempty"`
	Reused      bool           `json:"reused,omitempty"`
}

type CommitRequest struct {
	Input
	Extract ExtractResult `json:"extract"`
}

type Output struct {
	Revision     uint64       `json:"revision"`
	Doc          timeline.Doc `json:"doc"`
	AudioMediaID string       `json:"audioMediaId"`
	AudioItemID  string       `json:"audioItemId"`
	AudioTrackID string       `json:"audioTrackId"`
	Reused       bool         `json:"reused,omitempty"`
}

type CleanupRequest struct {
	ProjectID string `json:"projectId"`
	ItemID    string `json:"itemId"`
	BlobKey   string `json:"blobKey"`
}

func WorkflowID(projectID, itemID string) string {
	return "separate-audio:" + projectID + ":" + stableID("", itemID)
}

func WorkflowIDForTimeline(projectID, timelineID, itemID string) string {
	if timelineID == "" {
		return WorkflowID(projectID, itemID)
	}
	return "separate-audio:" + projectID + ":" + stableID("", timelineID+"\x00"+itemID)
}

func AudioMediaID(projectID string, source timeline.Media) string {
	return stableID("med_sep_", projectID+"\x00"+source.MediaID+"\x00"+hex.EncodeToString(uint64Bytes(source.Generation)))
}

func AudioItemID(projectID, itemID string) string {
	return stableID("item_sep_", projectID+"\x00"+itemID)
}

func AudioTrackID(projectID, itemID string) string {
	return stableID("a_sep_", projectID+"\x00"+itemID)
}

func stableID(prefix, value string) string {
	sum := sha256.Sum256([]byte(value))
	return prefix + hex.EncodeToString(sum[:12])
}

func uint64Bytes(value uint64) []byte {
	return []byte{
		byte(value >> 56), byte(value >> 48), byte(value >> 40), byte(value >> 32),
		byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value),
	}
}
