package entity

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"agentcut/timeline"
)

const DefaultSequenceName = "剪辑 1"

// TimelineSequence is one independently editable sequence inside a project.
// Media entries remain references to project blob objects; cloning a sequence
// never copies media bytes.
type TimelineSequence struct {
	TimelineID      string       `json:"timelineId"`
	Name            string       `json:"name"`
	Doc             timeline.Doc `json:"doc"`
	Revision        uint64       `json:"revision"`
	CurrentFrame    int64        `json:"currentFrame"`
	Zoom            float64      `json:"zoom"`
	CreatedAtUnixMs int64        `json:"createdAtUnixMs"`
	UpdatedAtUnixMs int64        `json:"updatedAtUnixMs"`
}

// TimelineSummary is the project-level sequence metadata exposed to clients.
// The active document itself remains in TimelineState.Doc for compatibility
// with existing render, Agent tool, and editing call sites.
type TimelineSummary struct {
	TimelineID       string  `json:"timelineId"`
	Name             string  `json:"name"`
	Revision         uint64  `json:"revision"`
	DurationInFrames int64   `json:"durationInFrames"`
	CurrentFrame     int64   `json:"currentFrame"`
	Zoom             float64 `json:"zoom"`
	CreatedAtUnixMs  int64   `json:"createdAtUnixMs"`
	UpdatedAtUnixMs  int64   `json:"updatedAtUnixMs"`
}

type CreateTimelineRequest struct {
	TimelineID       string `json:"timelineId"`
	Name             string `json:"name"`
	BaseVideoTrackID string `json:"baseVideoTrackId"`
	CreatedAtUnixMs  int64  `json:"createdAtUnixMs"`
}

type SwitchTimelineRequest struct {
	TimelineID      string `json:"timelineId"`
	UpdatedAtUnixMs int64  `json:"updatedAtUnixMs"`
}

type RenameTimelineRequest struct {
	TimelineID      string `json:"timelineId"`
	Name            string `json:"name"`
	UpdatedAtUnixMs int64  `json:"updatedAtUnixMs"`
}

type DeleteTimelineRequest struct {
	TimelineID      string `json:"timelineId"`
	UpdatedAtUnixMs int64  `json:"updatedAtUnixMs"`
}

func stableLegacyID(prefix, projectID string) string {
	sum := sha256.Sum256([]byte(projectID))
	return prefix + "_" + hex.EncodeToString(sum[:8])
}

// LegacyTimelineID gives old single-timeline projects a stable identity so a
// replay or repeated load never creates another migrated sequence.
func LegacyTimelineID(projectID string) string {
	return stableLegacyID("tl", projectID)
}

func legacyBaseVideoTrackID(projectID string) string {
	return stableLegacyID("trk", projectID)
}

func normalizeZoom(zoom float64) float64 {
	if zoom <= 0 {
		return 1
	}
	return zoom
}

func sequenceName(name string, index int) string {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		return trimmed
	}
	return fmt.Sprintf("剪辑 %d", index+1)
}

func ensureBaseVideoTrack(doc timeline.Doc, trackID string) timeline.Doc {
	for _, track := range doc.Tracks {
		if track.Kind == timeline.TrackVideo {
			return doc
		}
	}
	if trackID == "" {
		trackID = "v1"
	}
	base := timeline.Track{
		TrackID: trackID, Kind: timeline.TrackVideo, Name: "视频轨",
		CreatedBy: timeline.TrackCreatedAuto,
	}
	// Track order is compositing order. Keeping the base video track first
	// preserves the existing "later video tracks render above earlier ones"
	// contract.
	doc.Tracks = append([]timeline.Track{base}, doc.Tracks...)
	return doc
}

// newTimelineState performs the one-time v1 migration and normalizes
// Continue-As-New payloads. Existing tracks, items, captions, transitions, and
// media references are kept byte-for-byte apart from adding a missing V1.
func NewTimelineState(init TimelineInit, nowUnixMs int64) TimelineState {
	sequences := make([]TimelineSequence, 0, max(1, len(init.Timelines)))
	for index, candidate := range init.Timelines {
		sequence := candidate
		sequence.Doc = candidate.Doc.Clone()
		sequence.Name = sequenceName(sequence.Name, index)
		sequence.Zoom = normalizeZoom(sequence.Zoom)
		sequence.Doc = ensureBaseVideoTrack(sequence.Doc, legacyBaseVideoTrackID(init.ProjectID))
		if sequence.CreatedAtUnixMs == 0 {
			sequence.CreatedAtUnixMs = nowUnixMs
		}
		if sequence.UpdatedAtUnixMs == 0 {
			sequence.UpdatedAtUnixMs = sequence.CreatedAtUnixMs
		}
		if sequence.TimelineID == "" {
			sequence.TimelineID = stableLegacyID(fmt.Sprintf("tl%d", index+1), init.ProjectID)
		}
		sequences = append(sequences, sequence)
	}

	activeID := init.ActiveTimelineID
	if len(sequences) == 0 {
		if activeID == "" {
			activeID = LegacyTimelineID(init.ProjectID)
		}
		doc := ensureBaseVideoTrack(init.Doc.Clone(), legacyBaseVideoTrackID(init.ProjectID))
		sequences = append(sequences, TimelineSequence{
			TimelineID: activeID, Name: DefaultSequenceName, Doc: doc,
			Revision: init.Revision, Zoom: 1,
			CreatedAtUnixMs: nowUnixMs, UpdatedAtUnixMs: nowUnixMs,
		})
	}

	activeIndex := 0
	for index := range sequences {
		if sequences[index].TimelineID == activeID {
			activeIndex = index
			break
		}
	}
	active := sequences[activeIndex]
	return TimelineState{
		ProjectID: init.ProjectID, Doc: active.Doc.Clone(), Revision: active.Revision,
		ActiveTimelineID: active.TimelineID, Timelines: sequences,
		ProjectRevision: init.ProjectRevision,
		AgentKSessionID: init.AgentKSessionID,
	}
}

func (state *TimelineState) syncActiveSequence(nowUnixMs int64) {
	for index := range state.Timelines {
		if state.Timelines[index].TimelineID != state.ActiveTimelineID {
			continue
		}
		state.Timelines[index].Doc = state.Doc.Clone()
		state.Timelines[index].Revision = state.Revision
		state.Timelines[index].UpdatedAtUnixMs = nowUnixMs
		return
	}
}

func (state TimelineState) sequenceIndex(timelineID string) int {
	for index := range state.Timelines {
		if state.Timelines[index].TimelineID == timelineID {
			return index
		}
	}
	return -1
}

func (state *TimelineState) createTimeline(req CreateTimelineRequest) error {
	if state.sequenceIndex(req.TimelineID) >= 0 {
		return fmt.Errorf("timeline %q already exists", req.TimelineID)
	}
	state.syncActiveSequence(req.CreatedAtUnixMs)
	doc := state.Doc.Clone()
	doc.Items = []timeline.Item{}
	doc.Transitions = []timeline.Transition{}
	doc.Tracks = []timeline.Track{{
		TrackID: req.BaseVideoTrackID, Kind: timeline.TrackVideo, Name: "视频轨",
		CreatedBy: timeline.TrackCreatedAuto,
	}}
	sequence := TimelineSequence{
		TimelineID: req.TimelineID, Name: strings.TrimSpace(req.Name), Doc: doc,
		Zoom: 1, CreatedAtUnixMs: req.CreatedAtUnixMs, UpdatedAtUnixMs: req.CreatedAtUnixMs,
	}
	state.Timelines = append(state.Timelines, sequence)
	state.ActiveTimelineID = sequence.TimelineID
	state.Doc = sequence.Doc.Clone()
	state.Revision = sequence.Revision
	state.ProjectRevision++
	return nil
}

func (state *TimelineState) switchTimeline(req SwitchTimelineRequest) error {
	index := state.sequenceIndex(req.TimelineID)
	if index < 0 {
		return fmt.Errorf("timeline %q not found", req.TimelineID)
	}
	if req.TimelineID == state.ActiveTimelineID {
		return nil
	}
	state.syncActiveSequence(req.UpdatedAtUnixMs)
	target := state.Timelines[index]
	state.ActiveTimelineID = target.TimelineID
	state.Doc = target.Doc.Clone()
	state.Revision = target.Revision
	state.ProjectRevision++
	return nil
}

func (state *TimelineState) renameTimeline(req RenameTimelineRequest) error {
	index := state.sequenceIndex(req.TimelineID)
	if index < 0 {
		return fmt.Errorf("timeline %q not found", req.TimelineID)
	}
	state.Timelines[index].Name = strings.TrimSpace(req.Name)
	state.Timelines[index].UpdatedAtUnixMs = req.UpdatedAtUnixMs
	state.ProjectRevision++
	return nil
}

func (state *TimelineState) deleteTimeline(req DeleteTimelineRequest) ([]timeline.Media, error) {
	if len(state.Timelines) <= 1 {
		return nil, errors.New("the last timeline cannot be deleted")
	}
	index := state.sequenceIndex(req.TimelineID)
	if index < 0 {
		return nil, fmt.Errorf("timeline %q not found", req.TimelineID)
	}
	removedMedia := append([]timeline.Media(nil), state.Timelines[index].Doc.Media...)
	wasActive := req.TimelineID == state.ActiveTimelineID
	state.Timelines = append(state.Timelines[:index], state.Timelines[index+1:]...)
	if wasActive {
		targetIndex := index
		if targetIndex >= len(state.Timelines) {
			targetIndex = len(state.Timelines) - 1
		}
		target := state.Timelines[targetIndex]
		state.ActiveTimelineID = target.TimelineID
		state.Doc = target.Doc.Clone()
		state.Revision = target.Revision
	}
	state.ProjectRevision++
	return removedMedia, nil
}

func (state TimelineState) mediaReferenced(mediaID string) bool {
	for _, sequence := range state.Timelines {
		for _, media := range sequence.Doc.Media {
			if media.MediaID == mediaID {
				return true
			}
		}
	}
	return false
}

func (state TimelineState) allMedia() []timeline.Media {
	seen := make(map[string]bool)
	result := make([]timeline.Media, 0)
	for _, sequence := range state.Timelines {
		for _, media := range sequence.Doc.Media {
			key := fmt.Sprintf("%s\x00%d\x00%s", media.MediaID, media.Generation, media.BlobKey)
			if seen[key] {
				continue
			}
			seen[key] = true
			result = append(result, media)
		}
	}
	return result
}

func (state TimelineState) TimelineSummaries() []TimelineSummary {
	result := make([]TimelineSummary, 0, len(state.Timelines))
	for _, sequence := range state.Timelines {
		result = append(result, TimelineSummary{
			TimelineID: sequence.TimelineID, Name: sequence.Name, Revision: sequence.Revision,
			DurationInFrames: timeline.DocDurationInFrames(sequence.Doc),
			CurrentFrame:     sequence.CurrentFrame, Zoom: normalizeZoom(sequence.Zoom),
			CreatedAtUnixMs: sequence.CreatedAtUnixMs, UpdatedAtUnixMs: sequence.UpdatedAtUnixMs,
		})
	}
	return result
}
