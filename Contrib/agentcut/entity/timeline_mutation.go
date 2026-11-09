package entity

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
)

const MaxClientMutationIDLength = 128

var clientMutationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidateClientMutationID limits public idempotency keys to a small, printable
// token. Callers must still derive the Temporal UpdateID instead of using this
// value directly as an identifier.
func ValidateClientMutationID(id string) error {
	if id == "" {
		return errors.New("clientMutationId is required")
	}
	if len(id) > MaxClientMutationIDLength {
		return errors.New("clientMutationId is too long")
	}
	if !clientMutationIDPattern.MatchString(id) {
		return errors.New("clientMutationId has invalid format")
	}
	return nil
}

// TimelineMutationUpdateID produces a bounded UpdateID isolated by project and
// mutation source. Hashing prevents public or upstream identifiers from being
// interpreted as arbitrary Temporal identifiers while preserving stable retry
// semantics.
func TimelineMutationUpdateID(projectID, source, clientMutationID string) string {
	sum := sha256.Sum256([]byte(projectID + "\x00" + source + "\x00" + clientMutationID))
	return "timeline-mutation:" + source + ":" + hex.EncodeToString(sum[:])
}

func TimelineSequenceMutationUpdateID(projectID, timelineID, source, clientMutationID string) string {
	if timelineID == "" {
		return TimelineMutationUpdateID(projectID, source, clientMutationID)
	}
	sum := sha256.Sum256([]byte(projectID + "\x00" + timelineID + "\x00" + source + "\x00" + clientMutationID))
	return "timeline-mutation:" + source + ":" + hex.EncodeToString(sum[:])
}
