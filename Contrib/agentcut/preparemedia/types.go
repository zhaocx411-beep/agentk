package preparemedia

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"agentcut/timeline"
)

const (
	WorkflowName = "PrepareMediaWorkflow"

	LoadMediaActivityName       = "PrepareMedia.LoadMedia"
	ArchiveArtifactActivityName = "PrepareMedia.ArchiveArtifact"
	LocalArtifactsActivityName  = "PrepareMedia.LocalArtifacts"
	UpdateMediaActivityName     = "PrepareMedia.UpdateMedia"

	TranscribeCapabilityID = "agentk.transcribe_audio"
	UnderstandCapabilityID = "agentk.understand_media"
)

const defaultTaskQueue = "contrib.agentcut"

type Input struct {
	ProjectID          string `json:"projectId"`
	MediaID            string `json:"mediaId"`
	Generation         uint64 `json:"generation,omitempty"`
	AgentKSessionID    string `json:"agentkSessionId,omitempty"`
	ASRModel           string `json:"asrModel"`
	UnderstandingModel string `json:"understandingModel"`
}

type MediaSnapshot struct {
	Media timeline.Media `json:"media"`
	FPS   int            `json:"fps"`
}

type ArchiveArtifactRequest struct {
	ProjectID       string         `json:"projectId"`
	AgentKSessionID string         `json:"agentkSessionId"`
	Media           timeline.Media `json:"media"`
}

type ArchiveArtifactResult struct {
	BlobKey string `json:"blobKey"`
	URI     string `json:"uri"`
}

type StageResult struct {
	State timeline.ProcessingState `json:"state"`
	Error string                   `json:"error,omitempty"`
}

type LocalArtifactsRequest struct {
	ProjectID string         `json:"projectId"`
	Media     timeline.Media `json:"media"`
	FPS       int            `json:"fps"`
}

type LocalArtifactsResult struct {
	Probe            *timeline.MediaProbe `json:"probe,omitempty"`
	DurationInFrames int64                `json:"durationInFrames,omitempty"`
	ProbeStage       StageResult          `json:"probeStage"`
	ThumbnailKey     string               `json:"thumbnailKey,omitempty"`
	ThumbnailURI     string               `json:"thumbnailUri,omitempty"`
	ThumbnailStage   StageResult          `json:"thumbnailStage"`
	WaveformPeaks    []float32            `json:"waveformPeaks,omitempty"`
	WaveformStage    StageResult          `json:"waveformStage"`
	ASRAudioURI      string               `json:"asrAudioUri,omitempty"`
	TranscriptStage  StageResult          `json:"transcriptStage"`
	UnderstandingURI string               `json:"understandingUri,omitempty"`
}

type UpdateMediaRequest struct {
	ProjectID  string              `json:"projectId"`
	MediaID    string              `json:"mediaId"`
	Generation uint64              `json:"generation,omitempty"`
	Stage      string              `json:"stage"`
	Patch      timeline.MediaPatch `json:"patch"`
}

type TranscribeInput struct {
	Model    string `json:"model"`
	AudioURL string `json:"audioUrl"`
	Task     string `json:"task,omitempty"`
	Language string `json:"language,omitempty"`
	Prompt   string `json:"prompt,omitempty"`
}

type TranscribeOutput struct {
	Text     string              `json:"text"`
	Language string              `json:"language,omitempty"`
	Duration float64             `json:"duration,omitempty"`
	Segments []TranscribeSegment `json:"segments,omitempty"`
	Model    string              `json:"model"`
	Task     string              `json:"task"`
}

type TranscribeSegment struct {
	ID    int     `json:"id"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

type UnderstandInput struct {
	Model     string   `json:"model"`
	MediaURLs []string `json:"mediaUrls"`
	Prompt    string   `json:"prompt"`
}

type UnderstandOutput struct {
	Text   string         `json:"text"`
	Model  string         `json:"model"`
	Params map[string]any `json:"params,omitempty"`
}

type Starter interface {
	ExecuteWorkflow(ctx context.Context, options client.StartWorkflowOptions, workflow interface{}, args ...interface{}) (client.WorkflowRun, error)
}

func WorkflowID(projectID, mediaID string, generation ...uint64) string {
	if len(generation) == 0 || generation[0] == 0 {
		return "media-prepare:" + projectID + ":" + mediaID
	}
	return fmt.Sprintf("media-prepare:%s:%s:g%d", projectID, mediaID, generation[0])
}

func InputFromEnv(projectID, mediaID string, generation ...uint64) Input {
	asrModel := strings.TrimSpace(os.Getenv("AGENTCUT_ASR_MODEL"))
	if asrModel == "" {
		asrModel = "azure-whisper"
	}
	understandingModel := strings.TrimSpace(os.Getenv("AGENTCUT_UNDERSTANDING_MODEL"))
	if understandingModel == "" {
		understandingModel = "gemini-3.1-flash-lite"
	}
	var gen uint64
	if len(generation) > 0 {
		gen = generation[0]
	}
	return Input{
		ProjectID: projectID, MediaID: mediaID,
		Generation: gen,
		ASRModel:   asrModel, UnderstandingModel: understandingModel,
	}
}

// Start is idempotent while a run is active or has succeeded. A closed failed
// run may be started again with the same deterministic Workflow ID.
func Start(ctx context.Context, starter Starter, taskQueue string, in Input) (bool, error) {
	if err := timeline.ValidateMediaID(in.MediaID); err != nil {
		return false, err
	}
	if strings.TrimSpace(taskQueue) == "" {
		taskQueue = defaultTaskQueue
	}
	_, err := starter.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    WorkflowID(in.ProjectID, in.MediaID, in.Generation),
		TaskQueue:             taskQueue,
		WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
	}, WorkflowName, in)
	if err == nil {
		return true, nil
	}
	var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
	if errors.As(err, &alreadyStarted) {
		return false, nil
	}
	return false, err
}

func InitialProcessing(media timeline.Media) timeline.MediaProcessing {
	processing := timeline.MediaProcessing{}
	switch {
	case media.Kind == timeline.MediaImage:
		processing.Probe = timeline.ProcessingSkipped
		processing.ReadyForTimeline = true
	case media.Probe != nil && media.Probe.DurationMs > 0:
		processing.Probe = timeline.ProcessingSucceeded
		processing.ReadyForTimeline = true
	default:
		processing.Probe = timeline.ProcessingPending
	}
	if media.ThumbnailURI != "" {
		processing.Thumbnail = timeline.ProcessingSucceeded
	} else if media.Kind == timeline.MediaVideo || media.Kind == timeline.MediaImage {
		processing.Thumbnail = timeline.ProcessingPending
	} else {
		processing.Thumbnail = timeline.ProcessingSkipped
	}
	hasAudioKnown := media.Probe != nil
	if len(media.WaveformPeaks) > 0 {
		processing.Waveform = timeline.ProcessingSucceeded
	} else if media.Kind == timeline.MediaImage {
		processing.Waveform = timeline.ProcessingSkipped
	} else if hasAudioKnown && !media.Probe.HasAudio {
		processing.Waveform = timeline.ProcessingSkipped
	} else {
		processing.Waveform = timeline.ProcessingPending
	}
	if media.Transcript != nil {
		processing.Transcript = timeline.ProcessingSucceeded
	} else if media.Kind == timeline.MediaImage {
		processing.Transcript = timeline.ProcessingSkipped
	} else if hasAudioKnown && !media.Probe.HasAudio {
		processing.Transcript = timeline.ProcessingSkipped
	} else {
		processing.Transcript = timeline.ProcessingPending
	}
	if media.UnderstandingText != "" {
		processing.Understanding = timeline.ProcessingSucceeded
	} else if media.Kind == timeline.MediaVideo {
		processing.Understanding = timeline.ProcessingPending
	} else {
		processing.Understanding = timeline.ProcessingSkipped
	}
	FinalizeProcessing(&processing)
	return processing
}

func FinalizeProcessing(processing *timeline.MediaProcessing) {
	states := []timeline.ProcessingState{
		processing.Probe, processing.Thumbnail, processing.Waveform,
		processing.Transcript, processing.Understanding,
	}
	hasPending, hasFailed := false, false
	for _, state := range states {
		hasPending = hasPending || state == timeline.ProcessingPending || state == timeline.ProcessingRunning
		hasFailed = hasFailed || state == timeline.ProcessingFailed
	}
	switch {
	case processing.Probe == timeline.ProcessingFailed:
		processing.Status = "failed"
		processing.ReadyForTimeline = false
	case hasPending:
		processing.Status = "processing"
	case hasFailed:
		processing.Status = "partial"
	default:
		processing.Status = "ready"
	}
}
