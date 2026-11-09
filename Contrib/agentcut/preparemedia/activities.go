package preparemedia

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"

	"agentcut/entity"
	"agentcut/platform/artifactarchive"
	"agentcut/platform/blob"
	platformmedia "agentcut/platform/media"
	"agentcut/timeline"
)

const (
	maxPrepareSourceBytes = int64(2 << 30)
	maxASRAudioBytes      = int64(25 << 20)
)

type TemporalClient interface {
	QueryWorkflow(ctx context.Context, workflowID string, runID string, queryType string, args ...any) (converter.EncodedValue, error)
	UpdateWorkflow(ctx context.Context, options client.UpdateWorkflowOptions) (client.WorkflowUpdateHandle, error)
}

type Activities struct {
	Temporal          TemporalClient
	Store             blob.Storage
	Prober            platformmedia.Prober
	Thumbnailer       platformmedia.Thumbnailer
	WaveformExtractor platformmedia.WaveformExtractor
	AudioExtractor    platformmedia.AudioExtractor
	ArtifactVerifier  artifactarchive.Verifier
	ArtifactArchiver  *artifactarchive.Archiver
}

func (a *Activities) LoadMedia(ctx context.Context, in Input) (MediaSnapshot, error) {
	if err := timeline.ValidateMediaID(in.MediaID); err != nil {
		return MediaSnapshot{}, temporal.NewNonRetryableApplicationError(err.Error(), "BAD_MEDIA_ID", nil)
	}
	if a.Temporal == nil {
		return MediaSnapshot{}, temporal.NewNonRetryableApplicationError("temporal client is required", "CONFIGURATION", nil)
	}
	value, err := a.Temporal.QueryWorkflow(ctx, entity.TimelineWorkflowID(in.ProjectID), "", entity.QueryGetState)
	if err != nil {
		return MediaSnapshot{}, err
	}
	var state entity.TimelineState
	if err := value.Get(&state); err != nil {
		return MediaSnapshot{}, err
	}
	for _, media := range state.Doc.Media {
		if media.MediaID == in.MediaID {
			if media.Generation != in.Generation {
				return MediaSnapshot{}, temporal.NewNonRetryableApplicationError(
					fmt.Sprintf("media %q generation is %d, expected %d", in.MediaID, media.Generation, in.Generation),
					"STALE_MEDIA_GENERATION", nil)
			}
			return MediaSnapshot{Media: media, FPS: state.Doc.Metadata.FPS}, nil
		}
	}
	return MediaSnapshot{}, temporal.NewNonRetryableApplicationError(
		fmt.Sprintf("media %q not found", in.MediaID), "MEDIA_NOT_FOUND", nil)
}

func (a *Activities) ArchiveArtifact(
	ctx context.Context,
	req ArchiveArtifactRequest,
) (ArchiveArtifactResult, error) {
	archiver := a.ArtifactArchiver
	if archiver == nil {
		archiver = &artifactarchive.Archiver{
			Store: a.Store, Verifier: a.ArtifactVerifier,
		}
	}
	result, err := archiver.Archive(ctx, req.ProjectID, req.AgentKSessionID, req.Media)
	if err != nil {
		return ArchiveArtifactResult{}, temporal.NewNonRetryableApplicationError(
			err.Error(), "ARTIFACT_ARCHIVE_REJECTED", nil)
	}
	return ArchiveArtifactResult{BlobKey: result.BlobKey, URI: result.URI}, nil
}

func (a *Activities) LocalArtifacts(ctx context.Context, req LocalArtifactsRequest) (LocalArtifactsResult, error) {
	result := LocalArtifactsResult{
		ProbeStage:      StageResult{State: timeline.ProcessingPending},
		ThumbnailStage:  StageResult{State: timeline.ProcessingSkipped},
		WaveformStage:   StageResult{State: timeline.ProcessingSkipped},
		TranscriptStage: StageResult{State: timeline.ProcessingSkipped},
	}
	if a.Store == nil {
		return result, temporal.NewNonRetryableApplicationError(
			"blob storage is required for media preparation", "BLOB_NOT_CONFIGURED", nil)
	}
	if err := timeline.ValidateMediaID(req.Media.MediaID); err != nil {
		return result, temporal.NewNonRetryableApplicationError(err.Error(), "BAD_MEDIA_ID", nil)
	}
	key, err := blob.ResolveMediaSourceKey(req.ProjectID, req.Media.MediaID, req.Media.BlobKey, req.Media.URI)
	if err != nil {
		return result, temporal.NewNonRetryableApplicationError(err.Error(), "UNTRUSTED_MEDIA_SOURCE", nil)
	}
	managedSourceURI := blob.ManagedHTTPURI(key, req.Media.URI)
	result.UnderstandingURI = managedSourceURI
	object, err := a.Store.Get(ctx, key)
	if err != nil {
		return result, fmt.Errorf("get media source %q: %w", key, err)
	}
	defer object.Body.Close()
	if object.Size > maxPrepareSourceBytes {
		return result, temporal.NewNonRetryableApplicationError("media source exceeds 2 GiB", "MEDIA_TOO_LARGE", nil)
	}
	ext := filepath.Ext(key)
	tmp, err := os.CreateTemp("", "agentcut-prepare-*"+ext)
	if err != nil {
		return result, fmt.Errorf("create source temp file: %w", err)
	}
	sourcePath := tmp.Name()
	defer os.Remove(sourcePath)
	written, copyErr := io.Copy(tmp, io.LimitReader(object.Body, maxPrepareSourceBytes+1))
	closeErr := tmp.Close()
	if written > maxPrepareSourceBytes {
		return result, temporal.NewNonRetryableApplicationError("media source exceeds 2 GiB", "MEDIA_TOO_LARGE", nil)
	}
	if err := errors.Join(copyErr, closeErr); err != nil {
		return result, fmt.Errorf("copy media source to temp file: %w", err)
	}

	probe := req.Media.Probe
	switch req.Media.Kind {
	case timeline.MediaImage:
		result.ProbeStage.State = timeline.ProcessingSkipped
	case timeline.MediaVideo, timeline.MediaAudio:
		if probe == nil || probe.DurationMs <= 0 {
			prober := a.Prober
			if prober == nil {
				prober = platformmedia.FFProber{Timeout: 30 * time.Second}
			}
			detected, probeErr := prober.Probe(ctx, sourcePath)
			if probeErr != nil {
				result.ProbeStage = failedStage(probeErr)
				result.ThumbnailStage = failedOrSkipped(req.Media.Kind == timeline.MediaAudio, probeErr)
				result.WaveformStage = failedStage(probeErr)
				result.TranscriptStage = failedStage(probeErr)
				return result, nil
			}
			probe = &detected
		}
		if probe.DurationMs > int64((6*time.Hour)/time.Millisecond) {
			result.ProbeStage = failedStage(errors.New("media duration exceeds 6 hours"))
			result.ThumbnailStage = failedOrSkipped(req.Media.Kind == timeline.MediaAudio, errors.New("media duration exceeds 6 hours"))
			result.WaveformStage = failedStage(errors.New("media duration exceeds 6 hours"))
			result.TranscriptStage = failedStage(errors.New("media duration exceeds 6 hours"))
			return result, nil
		}
		result.Probe = cloneProbe(probe)
		result.DurationInFrames = framesFromMs(probe.DurationMs, req.FPS)
		result.ProbeStage.State = timeline.ProcessingSucceeded
	default:
		return result, temporal.NewNonRetryableApplicationError("unsupported media kind", "BAD_MEDIA_KIND", nil)
	}

	if req.Media.Kind == timeline.MediaVideo || req.Media.Kind == timeline.MediaImage {
		if req.Media.ThumbnailURI != "" {
			result.ThumbnailKey = req.Media.ThumbnailKey
			result.ThumbnailURI = req.Media.ThumbnailURI
			result.ThumbnailStage.State = timeline.ProcessingSucceeded
		} else {
			thumbnailer := a.Thumbnailer
			if thumbnailer == nil {
				thumbnailer = platformmedia.FFThumbnailer{Timeout: 45 * time.Second}
			}
			var data []byte
			if req.Media.Kind == timeline.MediaVideo {
				data, err = thumbnailer.GenerateVideoSprite(ctx, sourcePath, float64(probe.DurationMs)/1000)
			} else {
				data, err = thumbnailer.GenerateImageThumbnail(ctx, sourcePath)
			}
			if err != nil {
				result.ThumbnailStage = failedStage(err)
			} else {
				result.ThumbnailKey = blob.ThumbnailKeyForGeneration(
					req.ProjectID, req.Media.MediaID, req.Media.Generation)
				result.ThumbnailURI, err = a.Store.Put(ctx, result.ThumbnailKey, bytes.NewReader(data), int64(len(data)), "image/jpeg")
				if err != nil {
					result.ThumbnailStage = failedStage(err)
				} else {
					if a.Store.Backend() == "local" {
						result.ThumbnailURI = fmt.Sprintf("/v1/projects/%s/media/%s/thumbnail", req.ProjectID, req.Media.MediaID)
					}
					result.ThumbnailStage.State = timeline.ProcessingSucceeded
				}
			}
		}
	}

	if probe == nil || !probe.HasAudio {
		result.WaveformStage.State = timeline.ProcessingSkipped
		result.TranscriptStage.State = timeline.ProcessingSkipped
		return result, nil
	}

	if len(req.Media.WaveformPeaks) > 0 {
		result.WaveformPeaks = append([]float32(nil), req.Media.WaveformPeaks...)
		result.WaveformStage.State = timeline.ProcessingSucceeded
	} else {
		waveform := a.WaveformExtractor
		if waveform == nil {
			waveform = platformmedia.FFWaveformExtractor{Timeout: 2 * time.Minute}
		}
		result.WaveformPeaks, err = waveform.Extract(ctx, sourcePath)
		if err != nil {
			result.WaveformStage = failedStage(err)
		} else {
			result.WaveformStage.State = timeline.ProcessingSucceeded
		}
	}

	if req.Media.Transcript != nil {
		result.TranscriptStage.State = timeline.ProcessingSucceeded
		return result, nil
	}

	// Small managed audio can be sent directly. Larger audio must be normalized
	// to bounded 16 kHz mono 32 kbps MP3 before ASR.
	if req.Media.Kind == timeline.MediaAudio && written <= maxASRAudioBytes && managedSourceURI != "" {
		result.ASRAudioURI = managedSourceURI
		result.TranscriptStage.State = timeline.ProcessingPending
		return result, nil
	}

	audioFile, err := os.CreateTemp("", "agentcut-asr-*.mp3")
	if err != nil {
		result.TranscriptStage = failedStage(err)
		return result, nil
	}
	audioPath := audioFile.Name()
	_ = audioFile.Close()
	defer os.Remove(audioPath)
	extractor := a.AudioExtractor
	if extractor == nil {
		extractor = platformmedia.FFAudioExtractor{Timeout: 5 * time.Minute}
	}
	if err := extractor.ExtractForASR(ctx, sourcePath, audioPath); err != nil {
		result.TranscriptStage = failedStage(err)
		return result, nil
	}
	info, err := os.Stat(audioPath)
	if err != nil {
		result.TranscriptStage = failedStage(err)
		return result, nil
	}
	if info.Size() <= 0 || info.Size() > maxASRAudioBytes {
		result.TranscriptStage = failedStage(fmt.Errorf("ASR audio size %d is outside (0, 25 MiB]", info.Size()))
		return result, nil
	}
	audio, err := os.Open(audioPath)
	if err != nil {
		result.TranscriptStage = failedStage(err)
		return result, nil
	}
	defer audio.Close()
	result.ASRAudioURI, err = a.Store.Put(ctx, blob.ASRAudioKeyForGeneration(
		req.ProjectID, req.Media.MediaID, req.Media.Generation),
		audio, info.Size(), "audio/mpeg")
	if err != nil {
		result.TranscriptStage = failedStage(err)
	} else if !blob.IsManagedHTTPURI(result.ASRAudioURI) {
		result.TranscriptStage = failedStage(errors.New("derived audio is not exposed through an allowlisted HTTP/HTTPS Blob URI"))
	} else {
		result.TranscriptStage.State = timeline.ProcessingPending
	}
	return result, nil
}

func (a *Activities) UpdateMedia(ctx context.Context, req UpdateMediaRequest) error {
	if err := timeline.ValidateMediaID(req.MediaID); err != nil {
		return temporal.NewNonRetryableApplicationError(err.Error(), "BAD_MEDIA_ID", nil)
	}
	if a.Temporal == nil {
		return temporal.NewNonRetryableApplicationError("temporal client is required", "CONFIGURATION", nil)
	}
	baseID := "prepare:" + req.ProjectID + ":" + req.MediaID + ":" + req.Stage
	if activity.IsActivity(ctx) {
		info := activity.GetInfo(ctx)
		baseID = "prepare:" + info.WorkflowExecution.ID + ":" + info.WorkflowExecution.RunID + ":" + info.ActivityID
	}
	var lastConflict error
	for attempt := 0; attempt < 4; attempt++ {
		value, err := a.Temporal.QueryWorkflow(ctx, entity.TimelineWorkflowID(req.ProjectID), "", entity.QueryGetState)
		if err != nil {
			return err
		}
		var state entity.TimelineState
		if err := value.Get(&state); err != nil {
			return err
		}
		handle, err := a.Temporal.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
			WorkflowID:   entity.TimelineWorkflowID(req.ProjectID),
			UpdateName:   entity.UpdateApplyOps,
			UpdateID:     fmt.Sprintf("%s:%d", baseID, attempt),
			WaitForStage: client.WorkflowUpdateStageCompleted,
			Args: []any{entity.ApplyOpsRequest{
				BaseRevision: state.Revision,
				Source:       "system",
				Operations: []timeline.Operation{{
					Op: timeline.OpUpdateMedia, MediaID: req.MediaID,
					MediaGeneration: &req.Generation, MediaPatch: &req.Patch,
				}},
			}},
		})
		if err != nil {
			return err
		}
		err = handle.Get(ctx, nil)
		if !isRevisionConflict(err) {
			return err
		}
		lastConflict = err
	}
	return fmt.Errorf("update media after revision conflicts: %w", lastConflict)
}

func isRevisionConflict(err error) bool {
	var applicationErr *temporal.ApplicationError
	return errors.As(err, &applicationErr) && applicationErr.Type() == entity.ErrTypeRevisionConflict
}

func failedStage(err error) StageResult {
	return StageResult{State: timeline.ProcessingFailed, Error: truncateError(err)}
}

func failedOrSkipped(skip bool, err error) StageResult {
	if skip {
		return StageResult{State: timeline.ProcessingSkipped}
	}
	return failedStage(err)
}

func truncateError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if len(message) > 1024 {
		return message[:1024]
	}
	return message
}

func cloneProbe(probe *timeline.MediaProbe) *timeline.MediaProbe {
	if probe == nil {
		return nil
	}
	out := *probe
	return &out
}

func framesFromMs(ms int64, fps int) int64 {
	if ms <= 0 || fps <= 0 {
		return 0
	}
	return (ms*int64(fps) + 500) / 1000
}
