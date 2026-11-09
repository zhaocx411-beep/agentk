package timeline

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	MaxOccurrenceIDLength        = 512
	MaxTranscriptDeleteRanges    = 200
	TranscriptGranularityWord    = "word"
	TranscriptGranularitySegment = "segment"

	ErrInvalidOccurrenceID       = "INVALID_OCCURRENCE_ID"
	ErrCrossProjectOccurrence    = "CROSS_PROJECT_OCCURRENCE"
	ErrStaleTranscriptSelection  = "STALE_TRANSCRIPT_SELECTION"
	ErrTranscriptWordNotFound    = "TRANSCRIPT_WORD_NOT_FOUND"
	ErrTranscriptSegmentNotFound = "TRANSCRIPT_SEGMENT_NOT_FOUND"
	ErrTranscriptTimestamp       = "TRANSCRIPT_TIMESTAMP_UNAVAILABLE"
	ErrLockedTrackConflict       = "LOCKED_TRACK_CONFLICT"
)

type TranscriptEntry struct {
	OccurrenceID    string `json:"occurrenceId"`
	ItemID          string `json:"itemId"`
	TrackID         string `json:"trackId"`
	MediaID         string `json:"mediaId"`
	MediaGeneration uint64 `json:"mediaGeneration"`
	SegmentID       string `json:"segmentId"`
	WordID          string `json:"wordId,omitempty"`
	Text            string `json:"text"`
	SourceStartMs   int64  `json:"sourceStartMs"`
	SourceEndMs     int64  `json:"sourceEndMs"`
	FromFrame       int64  `json:"fromFrame"`
	ToFrame         int64  `json:"toFrame"`
	Granularity     string `json:"granularity"`
}

type TranscriptView struct {
	TimelineRevision uint64            `json:"timelineRevision"`
	Entries          []TranscriptEntry `json:"entries"`
	Warnings         []string          `json:"warnings"`
}

type TranscriptDeleteRange struct {
	FromOccurrenceID string `json:"fromOccurrenceId"`
	ToOccurrenceID   string `json:"toOccurrenceId"`
}

type TranscriptFrameRange struct {
	FromFrame int64 `json:"fromFrame"`
	ToFrame   int64 `json:"toFrame"`
}

type TranscriptDeleteCompilation struct {
	Operations              []Operation            `json:"-"`
	DeletedRanges           []TranscriptFrameRange `json:"deletedRanges"`
	DeletedDurationInFrames int64                  `json:"deletedDurationInFrames"`
}

type TranscriptEditError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *TranscriptEditError) Error() string { return e.Message }

func SelectTranscriptView(
	projectID string,
	timelineRevision uint64,
	doc Doc,
	fromOccurrenceID string,
	toOccurrenceID string,
	limit int,
) (TranscriptView, int, bool, *TranscriptEditError) {
	view := BuildTranscriptView(projectID, timelineRevision, doc)
	start, end := 0, len(view.Entries)
	byID := make(map[string]int, len(view.Entries))
	for index, entry := range view.Entries {
		byID[entry.OccurrenceID] = index
	}
	if fromOccurrenceID != "" {
		index, err := resolveOccurrenceIndex(
			projectID, timelineRevision, doc, view, byID, fromOccurrenceID,
		)
		if err != nil {
			return TranscriptView{}, 0, false, err
		}
		start = index
	}
	if toOccurrenceID != "" {
		index, err := resolveOccurrenceIndex(
			projectID, timelineRevision, doc, view, byID, toOccurrenceID,
		)
		if err != nil {
			return TranscriptView{}, 0, false, err
		}
		end = index + 1
	}
	if start > end {
		start, end = end-1, start+1
	}
	total := end - start
	truncated := limit > 0 && total > limit
	if truncated {
		end = start + limit
	}
	view.Entries = append([]TranscriptEntry(nil), view.Entries[start:end]...)
	return view, total, truncated, nil
}

type occurrenceToken struct {
	Version            int    `json:"v"`
	ProjectFingerprint string `json:"p"`
	TimelineRevision   uint64 `json:"r"`
	ItemFingerprint    string `json:"i"`
	MediaFingerprint   string `json:"m"`
	MediaGeneration    uint64 `json:"g"`
	TranscriptRevision uint64 `json:"t"`
	SegmentFingerprint string `json:"s"`
	WordFingerprint    string `json:"w,omitempty"`
	Granularity        string `json:"k"`
}

func BuildTranscriptView(projectID string, timelineRevision uint64, doc Doc) TranscriptView {
	view := TranscriptView{
		TimelineRevision: timelineRevision,
		Entries:          []TranscriptEntry{},
		Warnings:         []string{},
	}
	mediaByID := make(map[string]Media, len(doc.Media))
	for _, media := range doc.Media {
		mediaByID[media.MediaID] = media
	}
	trackOrder := make(map[string]int, len(doc.Tracks))
	for index, track := range doc.Tracks {
		trackOrder[track.TrackID] = index
	}
	warningSet := make(map[string]bool)
	addWarning := func(message string) {
		if !warningSet[message] {
			warningSet[message] = true
			view.Warnings = append(view.Warnings, message)
		}
	}

	for _, item := range doc.Items {
		if item.MediaID == "" || item.DurationInFrames <= 0 {
			continue
		}
		media, ok := mediaByID[item.MediaID]
		if !ok || media.Transcript == nil {
			continue
		}
		transcript := transcriptWithStableIdentities(media.MediaID, media.Generation, media.Transcript)
		for _, segment := range transcript.Segments {
			if segment.EndMs <= segment.StartMs {
				addWarning(fmt.Sprintf(
					"media %s segment %s has no usable timestamp",
					media.MediaID, segment.SegmentID,
				))
				continue
			}
			useWords := len(segment.Words) > 0
			if useWords {
				for _, word := range segment.Words {
					if word.StartMs < segment.StartMs || word.EndMs > segment.EndMs ||
						word.EndMs <= word.StartMs {
						useWords = false
						addWarning(fmt.Sprintf(
							"media %s segment %s has invalid word timestamps; using segment granularity",
							media.MediaID, segment.SegmentID,
						))
						break
					}
				}
			}
			if useWords {
				for _, word := range segment.Words {
					if entry, visible := transcriptEntryForItem(
						projectID, timelineRevision, doc.Metadata.FPS,
						item, media, transcript.Revision, segment, &word,
					); visible {
						view.Entries = append(view.Entries, entry)
					}
				}
				continue
			}
			if entry, visible := transcriptEntryForItem(
				projectID, timelineRevision, doc.Metadata.FPS,
				item, media, transcript.Revision, segment, nil,
			); visible {
				view.Entries = append(view.Entries, entry)
			}
		}
	}

	sort.SliceStable(view.Entries, func(i, j int) bool {
		left, right := view.Entries[i], view.Entries[j]
		if left.FromFrame != right.FromFrame {
			return left.FromFrame < right.FromFrame
		}
		if left.ToFrame != right.ToFrame {
			return left.ToFrame < right.ToFrame
		}
		if trackOrder[left.TrackID] != trackOrder[right.TrackID] {
			return trackOrder[left.TrackID] < trackOrder[right.TrackID]
		}
		if left.ItemID != right.ItemID {
			return left.ItemID < right.ItemID
		}
		if left.SourceStartMs != right.SourceStartMs {
			return left.SourceStartMs < right.SourceStartMs
		}
		return left.OccurrenceID < right.OccurrenceID
	})
	return view
}

func transcriptEntryForItem(
	projectID string,
	timelineRevision uint64,
	fps int,
	item Item,
	media Media,
	transcriptRevision uint64,
	segment TranscriptSegment,
	word *TranscriptWord,
) (TranscriptEntry, bool) {
	if fps <= 0 {
		fps = DefaultFPS
	}
	startMs, endMs, text := segment.StartMs, segment.EndMs, segment.Text
	wordID := ""
	granularity := TranscriptGranularitySegment
	if word != nil {
		startMs, endMs, text = word.StartMs, word.EndMs, word.Text
		wordID = word.WordID
		granularity = TranscriptGranularityWord
	}
	speed := item.EffectiveSpeed()
	sourceStart := float64(startMs) * float64(fps) / 1000
	sourceEnd := float64(endMs) * float64(fps) / 1000
	visibleStart := float64(item.TrimStart)
	visibleEnd := visibleStart + float64(item.DurationInFrames)*speed
	clippedStart := math.Max(sourceStart, visibleStart)
	clippedEnd := math.Min(sourceEnd, visibleEnd)
	if clippedEnd <= clippedStart {
		return TranscriptEntry{}, false
	}
	from := int64(math.Floor(float64(item.From) + (clippedStart-visibleStart)/speed))
	to := int64(math.Ceil(float64(item.From) + (clippedEnd-visibleStart)/speed))
	from = max(item.From, min(item.End(), from))
	to = max(item.From, min(item.End(), to))
	if to <= from {
		return TranscriptEntry{}, false
	}
	token := occurrenceToken{
		Version:            1,
		ProjectFingerprint: tokenFingerprint(projectID),
		TimelineRevision:   timelineRevision,
		ItemFingerprint:    tokenFingerprint(item.ItemID),
		MediaFingerprint:   tokenFingerprint(media.MediaID),
		MediaGeneration:    media.Generation,
		TranscriptRevision: transcriptRevision,
		SegmentFingerprint: tokenFingerprint(segment.SegmentID),
		Granularity:        granularity,
	}
	if wordID != "" {
		token.WordFingerprint = tokenFingerprint(wordID)
	}
	return TranscriptEntry{
		OccurrenceID:    makeOccurrenceID(token),
		ItemID:          item.ItemID,
		TrackID:         item.TrackID,
		MediaID:         media.MediaID,
		MediaGeneration: media.Generation,
		SegmentID:       segment.SegmentID,
		WordID:          wordID,
		Text:            text,
		SourceStartMs:   int64(math.Floor(clippedStart * 1000 / float64(fps))),
		SourceEndMs:     int64(math.Ceil(clippedEnd * 1000 / float64(fps))),
		FromFrame:       from,
		ToFrame:         to,
		Granularity:     granularity,
	}, true
}

func CompileTranscriptDelete(
	projectID string,
	timelineRevision uint64,
	doc Doc,
	ranges []TranscriptDeleteRange,
) (TranscriptDeleteCompilation, *TranscriptEditError) {
	if len(ranges) == 0 || len(ranges) > MaxTranscriptDeleteRanges {
		return TranscriptDeleteCompilation{}, transcriptEditError(
			string(ErrBadRequest),
			fmt.Sprintf("ranges must contain 1..%d entries", MaxTranscriptDeleteRanges),
		)
	}
	view := BuildTranscriptView(projectID, timelineRevision, doc)
	byID := make(map[string]int, len(view.Entries))
	for index, entry := range view.Entries {
		byID[entry.OccurrenceID] = index
	}
	var selectedRanges []TranscriptFrameRange
	for _, selected := range ranges {
		fromIndex, err := resolveOccurrenceIndex(
			projectID, timelineRevision, doc, view, byID, selected.FromOccurrenceID,
		)
		if err != nil {
			return TranscriptDeleteCompilation{}, err
		}
		toIndex, err := resolveOccurrenceIndex(
			projectID, timelineRevision, doc, view, byID, selected.ToOccurrenceID,
		)
		if err != nil {
			return TranscriptDeleteCompilation{}, err
		}
		if fromIndex > toIndex {
			fromIndex, toIndex = toIndex, fromIndex
		}
		perItem := make(map[string]TranscriptFrameRange)
		for _, entry := range view.Entries[fromIndex : toIndex+1] {
			current, exists := perItem[entry.ItemID]
			if !exists {
				perItem[entry.ItemID] = TranscriptFrameRange{
					FromFrame: entry.FromFrame,
					ToFrame:   entry.ToFrame,
				}
				continue
			}
			current.FromFrame = min(current.FromFrame, entry.FromFrame)
			current.ToFrame = max(current.ToFrame, entry.ToFrame)
			perItem[entry.ItemID] = current
		}
		for _, frameRange := range perItem {
			selectedRanges = append(selectedRanges, frameRange)
		}
	}
	merged := mergeTranscriptFrameRanges(selectedRanges)
	if len(merged) == 0 {
		return TranscriptDeleteCompilation{}, transcriptEditError(
			ErrTranscriptTimestamp, "selected transcript entries have no usable timestamps",
		)
	}
	if len(merged) > MaxOpsPerBatch {
		return TranscriptDeleteCompilation{}, transcriptEditError(
			string(ErrLimitExceeded),
			fmt.Sprintf("compiled delete requires %d operations, maximum is %d", len(merged), MaxOpsPerBatch),
		)
	}
	if err := rejectAffectedLockedTracks(doc, merged); err != nil {
		return TranscriptDeleteCompilation{}, err
	}
	var unlockedTrackIDs []string
	for _, track := range doc.Tracks {
		if !track.Locked {
			unlockedTrackIDs = append(unlockedTrackIDs, track.TrackID)
		}
	}
	compilation := TranscriptDeleteCompilation{
		DeletedRanges: append([]TranscriptFrameRange(nil), merged...),
		Operations:    make([]Operation, 0, len(merged)),
	}
	for _, frameRange := range merged {
		compilation.DeletedDurationInFrames += frameRange.ToFrame - frameRange.FromFrame
	}
	for index := len(merged) - 1; index >= 0; index-- {
		frameRange := merged[index]
		from, to := frameRange.FromFrame, frameRange.ToFrame
		compilation.Operations = append(compilation.Operations, Operation{
			Op:        OpRippleDelete,
			FromFrame: &from,
			ToFrame:   &to,
			TrackIDs:  append([]string(nil), unlockedTrackIDs...),
		})
	}
	return compilation, nil
}

func mergeTranscriptFrameRanges(ranges []TranscriptFrameRange) []TranscriptFrameRange {
	if len(ranges) == 0 {
		return nil
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].FromFrame == ranges[j].FromFrame {
			return ranges[i].ToFrame < ranges[j].ToFrame
		}
		return ranges[i].FromFrame < ranges[j].FromFrame
	})
	merged := []TranscriptFrameRange{ranges[0]}
	for _, next := range ranges[1:] {
		current := &merged[len(merged)-1]
		if next.FromFrame <= current.ToFrame {
			current.ToFrame = max(current.ToFrame, next.ToFrame)
			continue
		}
		merged = append(merged, next)
	}
	return merged
}

func rejectAffectedLockedTracks(doc Doc, ranges []TranscriptFrameRange) *TranscriptEditError {
	locked := make(map[string]bool)
	for _, track := range doc.Tracks {
		if track.Locked {
			locked[track.TrackID] = true
		}
	}
	for _, item := range doc.Items {
		if !locked[item.TrackID] {
			continue
		}
		for _, frameRange := range ranges {
			if item.End() > frameRange.FromFrame {
				return transcriptEditError(
					ErrLockedTrackConflict,
					fmt.Sprintf("locked track %s contains item %s that would be deleted or shifted", item.TrackID, item.ItemID),
				)
			}
		}
	}
	return nil
}

func resolveOccurrenceIndex(
	projectID string,
	timelineRevision uint64,
	doc Doc,
	view TranscriptView,
	index map[string]int,
	occurrenceID string,
) (int, *TranscriptEditError) {
	token, err := parseOccurrenceID(occurrenceID)
	if err != nil {
		return 0, transcriptEditError(ErrInvalidOccurrenceID, err.Error())
	}
	if token.ProjectFingerprint != tokenFingerprint(projectID) {
		return 0, transcriptEditError(ErrCrossProjectOccurrence, "occurrenceId belongs to another project")
	}
	if token.TimelineRevision != timelineRevision {
		return 0, transcriptEditError(ErrStaleTranscriptSelection, "occurrenceId was created from an older timeline revision")
	}
	if entryIndex, ok := index[occurrenceID]; ok {
		return entryIndex, nil
	}
	if occurrenceTimestampUnavailable(token, doc) {
		return 0, transcriptEditError(ErrTranscriptTimestamp, "selected transcript entry has no usable timestamp")
	}
	if occurrenceTranscriptChanged(token, doc) {
		return 0, transcriptEditError(ErrStaleTranscriptSelection, "source transcript changed after this occurrenceId was created")
	}
	if token.Granularity == TranscriptGranularityWord {
		return 0, transcriptEditError(ErrTranscriptWordNotFound, "selected transcript word no longer exists")
	}
	return 0, transcriptEditError(ErrTranscriptSegmentNotFound, "selected transcript segment no longer exists")
}

func occurrenceTranscriptChanged(token occurrenceToken, doc Doc) bool {
	for _, media := range doc.Media {
		if tokenFingerprint(media.MediaID) != token.MediaFingerprint {
			continue
		}
		if media.Generation != token.MediaGeneration || media.Transcript == nil {
			return true
		}
		return media.Transcript.Revision != token.TranscriptRevision
	}
	return true
}

func occurrenceTimestampUnavailable(token occurrenceToken, doc Doc) bool {
	for _, item := range doc.Items {
		if tokenFingerprint(item.ItemID) != token.ItemFingerprint {
			continue
		}
		for _, media := range doc.Media {
			if media.MediaID != item.MediaID || tokenFingerprint(media.MediaID) != token.MediaFingerprint ||
				media.Transcript == nil {
				continue
			}
			transcript := transcriptWithStableIdentities(media.MediaID, media.Generation, media.Transcript)
			for _, segment := range transcript.Segments {
				if tokenFingerprint(segment.SegmentID) != token.SegmentFingerprint {
					continue
				}
				if token.Granularity == TranscriptGranularitySegment {
					return segment.EndMs <= segment.StartMs
				}
				for _, word := range segment.Words {
					if tokenFingerprint(word.WordID) == token.WordFingerprint {
						return word.EndMs <= word.StartMs
					}
				}
			}
		}
	}
	return false
}

func makeOccurrenceID(token occurrenceToken) string {
	payload, _ := json.Marshal(token)
	sum := sha256.Sum256(append([]byte("agentcut-occurrence-v1\x00"), payload...))
	return "occ1." +
		base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(sum[:])
}

func parseOccurrenceID(id string) (occurrenceToken, error) {
	if len(id) == 0 || len(id) > MaxOccurrenceIDLength {
		return occurrenceToken{}, errors.New("occurrenceId has invalid length")
	}
	parts := strings.Split(id, ".")
	if len(parts) != 3 || parts[0] != "occ1" {
		return occurrenceToken{}, errors.New("occurrenceId has invalid format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return occurrenceToken{}, errors.New("occurrenceId payload is invalid")
	}
	checksum, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return occurrenceToken{}, errors.New("occurrenceId checksum is invalid")
	}
	sum := sha256.Sum256(append([]byte("agentcut-occurrence-v1\x00"), payload...))
	if len(checksum) != len(sum) || subtle.ConstantTimeCompare(checksum, sum[:]) != 1 {
		return occurrenceToken{}, errors.New("occurrenceId checksum does not match")
	}
	var token occurrenceToken
	if err := json.Unmarshal(payload, &token); err != nil || token.Version != 1 {
		return occurrenceToken{}, errors.New("occurrenceId payload is unsupported")
	}
	if token.ProjectFingerprint == "" || token.ItemFingerprint == "" ||
		token.MediaFingerprint == "" || token.SegmentFingerprint == "" ||
		(token.Granularity != TranscriptGranularityWord &&
			token.Granularity != TranscriptGranularitySegment) ||
		(token.Granularity == TranscriptGranularityWord && token.WordFingerprint == "") {
		return occurrenceToken{}, errors.New("occurrenceId payload is incomplete")
	}
	return token, nil
}

func tokenFingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:12])
}

func transcriptEditError(code, message string) *TranscriptEditError {
	return &TranscriptEditError{Code: code, Message: message}
}
