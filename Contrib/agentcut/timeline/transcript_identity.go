package timeline

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
)

const MaxTranscriptIdentityLength = 64

func validateTranscriptIdentity(id, prefix string) error {
	if len(id) < len(prefix)+1 || len(id) > MaxTranscriptIdentityLength ||
		!strings.HasPrefix(id, prefix) {
		return fmt.Errorf("%s identity must use %s prefix and be at most %d characters",
			prefix, prefix, MaxTranscriptIdentityLength)
	}
	for _, c := range []byte(id[len(prefix):]) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-' {
			continue
		}
		return errors.New("transcript identity contains invalid characters")
	}
	return nil
}

func transcriptIdentity(prefix string, parts ...any) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = fmt.Fprintf(hash, "%T:%v\x00", part, part)
	}
	sum := hash.Sum(nil)
	return prefix + hex.EncodeToString(sum[:16])
}

func normalizeTranscriptReplacement(
	mediaID string,
	mediaGeneration uint64,
	previous *Transcript,
	incoming *Transcript,
) (*Transcript, error) {
	if incoming == nil {
		return nil, errors.New("transcript is required")
	}
	if err := validateTranscriptContents(incoming); err != nil {
		return nil, err
	}
	revision := uint64(1)
	if previous != nil {
		if previous.Revision == math.MaxUint64 {
			return nil, errors.New("transcript revision exhausted")
		}
		revision = previous.Revision + 1
	}
	out := cloneTranscript(incoming)
	out.Revision = revision
	assignTranscriptIdentities(mediaID, mediaGeneration, out, true)
	return out, nil
}

// transcriptWithStableIdentities provides deterministic IDs for legacy
// transcripts without mutating the authoritative ASR source document.
func transcriptWithStableIdentities(mediaID string, mediaGeneration uint64, transcript *Transcript) *Transcript {
	if transcript == nil {
		return nil
	}
	out := cloneTranscript(transcript)
	assignTranscriptIdentities(mediaID, mediaGeneration, out, false)
	return out
}

func assignTranscriptIdentities(
	mediaID string,
	mediaGeneration uint64,
	transcript *Transcript,
	replaceAll bool,
) {
	for segmentIndex := range transcript.Segments {
		segment := &transcript.Segments[segmentIndex]
		if replaceAll || validateTranscriptIdentity(segment.SegmentID, "seg_") != nil {
			segment.SegmentID = transcriptIdentity(
				"seg_", mediaID, mediaGeneration, segmentIndex,
				segment.StartMs, segment.EndMs, segment.Text,
			)
		}
		for wordIndex := range segment.Words {
			word := &segment.Words[wordIndex]
			if replaceAll || validateTranscriptIdentity(word.WordID, "word_") != nil {
				word.WordID = transcriptIdentity(
					"word_", mediaID, mediaGeneration, segment.SegmentID, wordIndex,
					word.StartMs, word.EndMs, word.Text,
				)
			}
		}
	}
}

func validateTranscriptContents(transcript *Transcript) error {
	for segmentIndex, segment := range transcript.Segments {
		if segment.StartMs < 0 || segment.EndMs <= segment.StartMs {
			return fmt.Errorf("segment[%d] has invalid or missing time range", segmentIndex)
		}
		if strings.TrimSpace(segment.Text) == "" {
			return fmt.Errorf("segment[%d] text must be non-empty", segmentIndex)
		}
		for wordIndex, word := range segment.Words {
			if word.StartMs < segment.StartMs || word.EndMs > segment.EndMs ||
				word.EndMs <= word.StartMs {
				return fmt.Errorf(
					"segment[%d].word[%d] has invalid or missing time range",
					segmentIndex, wordIndex,
				)
			}
			if strings.TrimSpace(word.Text) == "" {
				return fmt.Errorf("segment[%d].word[%d] text must be non-empty", segmentIndex, wordIndex)
			}
		}
	}
	return nil
}
