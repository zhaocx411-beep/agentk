package separateaudio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"

	"agentcut/entity"
	"agentcut/platform/blob"
	platformmedia "agentcut/platform/media"
	"agentcut/preparemedia"
	"agentcut/timeline"
)

type TemporalClient interface {
	QueryWorkflow(ctx context.Context, workflowID string, runID string, queryType string, args ...any) (converter.EncodedValue, error)
	UpdateWorkflow(ctx context.Context, options client.UpdateWorkflowOptions) (client.WorkflowUpdateHandle, error)
}

type Activities struct {
	Temporal  TemporalClient
	Store     blob.Storage
	Extractor platformmedia.EditingAudioExtractor
}

func (a *Activities) Extract(ctx context.Context, req ExtractRequest) (ExtractResult, error) {
	state, sourceItem, sourceMedia, err := a.loadSource(ctx, req.Input)
	if err != nil {
		return ExtractResult{}, err
	}
	audioMediaID := AudioMediaID(req.ProjectID, sourceMedia)
	audioItemID := AudioItemID(req.ProjectID, sourceItem.ItemID)
	for _, item := range state.Doc.Items {
		if item.ItemID == audioItemID && item.Kind == timeline.ItemAudio &&
			item.MediaID == audioMediaID {
			for _, media := range state.Doc.Media {
				if media.MediaID == audioMediaID && media.Kind == timeline.MediaAudio {
					return ExtractResult{
						AudioMedia: media, AudioItemID: audioItemID,
						BlobKey: media.BlobKey, Reused: true,
					}, nil
				}
			}
		}
	}
	for _, media := range state.Doc.Media {
		if media.MediaID == audioMediaID && media.Kind == timeline.MediaAudio {
			return ExtractResult{
				AudioMedia: media, AudioItemID: audioItemID,
				BlobKey: media.BlobKey, Reused: true,
			}, nil
		}
	}
	if a.Store == nil {
		return ExtractResult{}, nonRetryable("blob storage is required", "BLOB_NOT_CONFIGURED")
	}
	key, err := blob.ResolveMediaSourceKey(
		req.ProjectID, sourceMedia.MediaID, sourceMedia.BlobKey, sourceMedia.URI,
	)
	if err != nil {
		return ExtractResult{}, nonRetryable(err.Error(), "MEDIA_NOT_FOUND")
	}
	object, err := a.Store.Get(ctx, key)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("get video source: %w", err)
	}
	defer object.Body.Close()
	if object.Size > platformmedia.MaxFFmpegSourceBytes {
		return ExtractResult{}, nonRetryable("video source exceeds 2 GiB", "MEDIA_TOO_LARGE")
	}
	input, err := os.CreateTemp("", "agentcut-separate-*"+filepath.Ext(key))
	if err != nil {
		return ExtractResult{}, err
	}
	inputPath := input.Name()
	defer os.Remove(inputPath)
	written, copyErr := io.Copy(input, io.LimitReader(
		object.Body, platformmedia.MaxFFmpegSourceBytes+1,
	))
	closeErr := input.Close()
	if written > platformmedia.MaxFFmpegSourceBytes {
		return ExtractResult{}, nonRetryable("video source exceeds 2 GiB", "MEDIA_TOO_LARGE")
	}
	if err := errors.Join(copyErr, closeErr); err != nil {
		return ExtractResult{}, fmt.Errorf("stage video source: %w", err)
	}
	output, err := os.CreateTemp("", "agentcut-separated-*.mp3")
	if err != nil {
		return ExtractResult{}, err
	}
	outputPath := output.Name()
	_ = output.Close()
	defer os.Remove(outputPath)
	extractor := a.Extractor
	if extractor == nil {
		extractor = platformmedia.FFAudioPreviewer{Timeout: 10 * time.Minute}
	}
	if err := extractor.ExtractForEditing(ctx, inputPath, outputPath); err != nil {
		return ExtractResult{}, nonRetryable(err.Error(), "AUDIO_EXTRACTION_FAILED")
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		return ExtractResult{}, err
	}
	audio, err := os.Open(outputPath)
	if err != nil {
		return ExtractResult{}, err
	}
	defer audio.Close()
	audioKey := blob.SeparatedAudioSourceKey(req.ProjectID, audioMediaID)
	uri, err := a.Store.Put(ctx, audioKey, audio, info.Size(), "audio/mpeg")
	if err != nil {
		return ExtractResult{}, fmt.Errorf("store separated audio: %w", err)
	}
	if a.Store.Backend() == "local" {
		uri = fmt.Sprintf("/v1/projects/%s/media/%s/content", req.ProjectID, audioMediaID)
	}
	probe := &timeline.MediaProbe{HasAudio: true, Codec: "mp3"}
	if sourceMedia.Probe != nil {
		probe.DurationMs = sourceMedia.Probe.DurationMs
	}
	audioMedia := timeline.Media{
		MediaID: audioMediaID, URI: uri, BlobKey: audioKey, Kind: timeline.MediaAudio,
		DurationInFrames: sourceMedia.DurationInFrames, Probe: probe,
		DisplayName:   separatedDisplayName(sourceMedia),
		WaveformPeaks: append([]float32(nil), sourceMedia.WaveformPeaks...),
	}
	processing := preparemedia.InitialProcessing(audioMedia)
	if len(audioMedia.WaveformPeaks) == 0 {
		processing.Waveform = timeline.ProcessingSkipped
	}
	processing.Transcript = timeline.ProcessingSkipped
	preparemedia.FinalizeProcessing(&processing)
	audioMedia.Processing = &processing
	return ExtractResult{
		AudioMedia:  audioMedia,
		AudioItemID: audioItemID,
		BlobKey:     audioKey,
	}, nil
}

func (a *Activities) Commit(ctx context.Context, req CommitRequest) (Output, error) {
	state, sourceItem, _, err := a.loadSourceAllowAdvanced(ctx, req.Input)
	if err != nil {
		return Output{}, err
	}
	if existing, ok := separatedOutput(state, req.Extract.AudioMedia.MediaID, req.Extract.AudioItemID); ok {
		existing.Reused = true
		return existing, nil
	}
	if state.Revision != req.BaseRevision {
		return Output{}, revisionConflict(state.Revision)
	}
	sourceTrack := trackByID(state.Doc, sourceItem.TrackID)
	if sourceTrack == nil || sourceTrack.Locked {
		return Output{}, nonRetryable("source video track is locked", "LOCKED_TRACK")
	}
	audioTrackID := ""
	for _, track := range state.Doc.Tracks {
		if track.Kind == timeline.TrackAudio && !track.Locked &&
			trackHasRoom(state.Doc, track.TrackID, sourceItem.From, sourceItem.End()) {
			audioTrackID = track.TrackID
			break
		}
	}
	var operations []timeline.Operation
	if !mediaExists(state.Doc, req.Extract.AudioMedia.MediaID) {
		media := req.Extract.AudioMedia
		operations = append(operations, timeline.Operation{Op: timeline.OpAddMedia, Media: &media})
	}
	if audioTrackID == "" {
		audioTrackID = AudioTrackID(req.ProjectID, sourceItem.ItemID)
		track := timeline.Track{
			TrackID: audioTrackID, Kind: timeline.TrackAudio, Name: "分离音频",
			CreatedBy: timeline.TrackCreatedAuto,
		}
		operations = append(operations, timeline.Operation{Op: timeline.OpAddTrack, Track: &track})
	}
	audioItem := timeline.Item{
		ItemID: req.Extract.AudioItemID, TrackID: audioTrackID,
		Kind: timeline.ItemAudio, MediaID: req.Extract.AudioMedia.MediaID,
		From: sourceItem.From, DurationInFrames: sourceItem.DurationInFrames,
		TrimStart: sourceItem.TrimStart, Speed: sourceItem.Speed, Volume: sourceItem.Volume,
		FadeInFrames: sourceItem.FadeInFrames, FadeOutFrames: sourceItem.FadeOutFrames,
	}
	muted := true
	operations = append(operations,
		timeline.Operation{Op: timeline.OpAddItem, Item: &audioItem},
		timeline.Operation{
			Op: timeline.OpUpdateItem, ItemID: sourceItem.ItemID,
			Patch: &timeline.ItemPatch{Muted: &muted},
		},
	)
	handle, err := a.Temporal.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID: entity.TimelineWorkflowID(req.ProjectID),
		UpdateName: entity.UpdateApplyOps,
		UpdateID: entity.TimelineSequenceMutationUpdateID(
			req.ProjectID, req.TimelineID, "system", "separate-audio:"+req.ItemID,
		),
		WaitForStage: client.WorkflowUpdateStageCompleted,
		Args: []any{entity.ApplyOpsRequest{
			TimelineID: req.TimelineID, BaseRevision: req.BaseRevision,
			Source: "system", Operations: operations,
		}},
	})
	if err != nil {
		return Output{}, err
	}
	var result entity.ApplyOpsResult
	if err := handle.Get(ctx, &result); err != nil {
		return Output{}, err
	}
	committed, err := a.loadState(ctx, req.ProjectID)
	if err != nil {
		return Output{}, err
	}
	if committed.Revision < result.Revision {
		return Output{}, errors.New("timeline query returned a stale revision after audio separation")
	}
	output, ok := separatedOutput(committed, req.Extract.AudioMedia.MediaID, req.Extract.AudioItemID)
	if !ok {
		return Output{}, errors.New("separated audio commit did not produce the expected item")
	}
	return output, nil
}

func (a *Activities) Cleanup(ctx context.Context, req CleanupRequest) error {
	if a.Store == nil || strings.TrimSpace(req.BlobKey) == "" {
		return nil
	}
	state, err := a.loadState(ctx, req.ProjectID)
	if err == nil {
		for _, media := range state.Doc.Media {
			if media.BlobKey == req.BlobKey {
				return nil
			}
		}
	}
	return a.Store.Delete(ctx, req.BlobKey)
}

func (a *Activities) loadSource(ctx context.Context, in Input) (entity.TimelineState, timeline.Item, timeline.Media, error) {
	state, item, media, err := a.loadSourceAllowAdvanced(ctx, in)
	if err != nil {
		return state, item, media, err
	}
	if state.Revision != in.BaseRevision {
		return state, item, media, revisionConflict(state.Revision)
	}
	return state, item, media, nil
}

func (a *Activities) loadSourceAllowAdvanced(ctx context.Context, in Input) (entity.TimelineState, timeline.Item, timeline.Media, error) {
	state, err := a.loadState(ctx, in.ProjectID)
	if err != nil {
		return entity.TimelineState{}, timeline.Item{}, timeline.Media{}, err
	}
	if in.TimelineID != "" && state.ActiveTimelineID != in.TimelineID {
		return state, timeline.Item{}, timeline.Media{},
			nonRetryable("timeline is no longer active", entity.ErrTypeTimelineNotActive)
	}
	var item *timeline.Item
	for index := range state.Doc.Items {
		if state.Doc.Items[index].ItemID == in.ItemID {
			item = &state.Doc.Items[index]
			break
		}
	}
	if item == nil || item.Kind != timeline.ItemVideo {
		return state, timeline.Item{}, timeline.Media{}, nonRetryable("video item not found", "ITEM_NOT_FOUND")
	}
	track := trackByID(state.Doc, item.TrackID)
	if track == nil || track.Locked {
		return state, *item, timeline.Media{}, nonRetryable("source video track is locked", "LOCKED_TRACK")
	}
	for _, media := range state.Doc.Media {
		if media.MediaID == item.MediaID {
			if media.Kind != timeline.MediaVideo {
				break
			}
			if media.Probe == nil || !media.Probe.HasAudio {
				return state, *item, media, nonRetryable("video has no audio stream", "NO_AUDIO")
			}
			return state, *item, media, nil
		}
	}
	return state, *item, timeline.Media{}, nonRetryable("video media not found", "MEDIA_NOT_FOUND")
}

func (a *Activities) loadState(ctx context.Context, projectID string) (entity.TimelineState, error) {
	if a.Temporal == nil {
		return entity.TimelineState{}, nonRetryable("temporal client is required", "CONFIGURATION")
	}
	value, err := a.Temporal.QueryWorkflow(
		ctx, entity.TimelineWorkflowID(projectID), "", entity.QueryGetState,
	)
	if err != nil {
		return entity.TimelineState{}, err
	}
	var state entity.TimelineState
	if err := value.Get(&state); err != nil {
		return entity.TimelineState{}, err
	}
	return state, nil
}

func separatedOutput(state entity.TimelineState, mediaID, itemID string) (Output, bool) {
	for _, item := range state.Doc.Items {
		if item.ItemID == itemID && item.Kind == timeline.ItemAudio && item.MediaID == mediaID {
			return Output{
				Revision: state.Revision, Doc: state.Doc, AudioMediaID: mediaID,
				AudioItemID: itemID, AudioTrackID: item.TrackID,
			}, true
		}
	}
	return Output{}, false
}

func trackByID(doc timeline.Doc, trackID string) *timeline.Track {
	for index := range doc.Tracks {
		if doc.Tracks[index].TrackID == trackID {
			return &doc.Tracks[index]
		}
	}
	return nil
}

func mediaExists(doc timeline.Doc, mediaID string) bool {
	for _, media := range doc.Media {
		if media.MediaID == mediaID {
			return true
		}
	}
	return false
}

func trackHasRoom(doc timeline.Doc, trackID string, from, to int64) bool {
	for _, item := range doc.Items {
		if item.TrackID == trackID && from < item.End() && item.From < to {
			return false
		}
	}
	return true
}

func separatedDisplayName(source timeline.Media) string {
	name := strings.TrimSpace(source.DisplayName)
	if name == "" {
		name = source.MediaID
	}
	return name + "（分离音频）"
}

func revisionConflict(latest uint64) error {
	return temporal.NewNonRetryableApplicationError(
		fmt.Sprintf("base revision does not match latest revision %d", latest),
		entity.ErrTypeRevisionConflict, nil,
		entity.RevisionConflictDetails{LatestRevision: latest},
	)
}

func nonRetryable(message, errorType string) error {
	return temporal.NewNonRetryableApplicationError(message, errorType, nil)
}
