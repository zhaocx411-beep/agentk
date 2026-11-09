package preparemedia

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"agentcut/timeline"
)

const invokeWorkflowType = "agentk.capability.invoke.v1"

type invokeWorkflowInput struct {
	CapabilityID string          `json:"capabilityId"`
	Input        json.RawMessage `json:"input"`
}

type workflowCapture struct {
	mu               sync.Mutex
	patches          []timeline.MediaPatch
	localRequests    []LocalArtifactsRequest
	transcribeInputs []TranscribeInput
	understandInputs []UnderstandInput
}

func (c *workflowCapture) update(_ context.Context, req UpdateMediaRequest) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.patches = append(c.patches, req.Patch)
	return nil
}

func newPrepareEnv(
	t *testing.T,
	snapshot MediaSnapshot,
	local LocalArtifactsResult,
	transcribe func(TranscribeInput) (TranscribeOutput, error),
	understand func(UnderstandInput) (UnderstandOutput, error),
) (*testsuite.TestWorkflowEnvironment, *workflowCapture) {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	capture := &workflowCapture{}
	env.RegisterWorkflowWithOptions(Workflow, workflow.RegisterOptions{Name: WorkflowName})
	env.RegisterActivityWithOptions(
		func(context.Context, Input) (MediaSnapshot, error) { return snapshot, nil },
		activity.RegisterOptions{Name: LoadMediaActivityName},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, req LocalArtifactsRequest) (LocalArtifactsResult, error) {
			capture.mu.Lock()
			capture.localRequests = append(capture.localRequests, req)
			capture.mu.Unlock()
			return local, nil
		},
		activity.RegisterOptions{Name: LocalArtifactsActivityName},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, ArchiveArtifactRequest) (ArchiveArtifactResult, error) {
			return ArchiveArtifactResult{
				BlobKey: "agentcut/p1/source/m1/video.mp4",
				URI:     "https://blob.example/bucket/agentcut/p1/source/m1/video.mp4",
			}, nil
		},
		activity.RegisterOptions{Name: ArchiveArtifactActivityName},
	)
	env.RegisterActivityWithOptions(capture.update, activity.RegisterOptions{Name: UpdateMediaActivityName})
	env.RegisterWorkflowWithOptions(
		func(_ workflow.Context, in invokeWorkflowInput) (json.RawMessage, error) {
			switch in.CapabilityID {
			case TranscribeCapabilityID:
				var input TranscribeInput
				if err := json.Unmarshal(in.Input, &input); err != nil {
					return nil, err
				}
				capture.mu.Lock()
				capture.transcribeInputs = append(capture.transcribeInputs, input)
				capture.mu.Unlock()
				output, err := transcribe(input)
				if err != nil {
					return nil, err
				}
				return json.Marshal(output)
			case UnderstandCapabilityID:
				var input UnderstandInput
				if err := json.Unmarshal(in.Input, &input); err != nil {
					return nil, err
				}
				capture.mu.Lock()
				capture.understandInputs = append(capture.understandInputs, input)
				capture.mu.Unlock()
				output, err := understand(input)
				if err != nil {
					return nil, err
				}
				return json.Marshal(output)
			default:
				return nil, temporal.NewNonRetryableApplicationError("unknown capability", "NotFound", nil)
			}
		},
		workflow.RegisterOptions{Name: invokeWorkflowType},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, in TranscribeInput) (TranscribeOutput, error) {
			capture.mu.Lock()
			capture.transcribeInputs = append(capture.transcribeInputs, in)
			capture.mu.Unlock()
			return transcribe(in)
		},
		activity.RegisterOptions{Name: "fake.transcribe"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, in UnderstandInput) (UnderstandOutput, error) {
			capture.mu.Lock()
			capture.understandInputs = append(capture.understandInputs, in)
			capture.mu.Unlock()
			return understand(in)
		},
		activity.RegisterOptions{Name: "fake.understand"},
	)
	return env, capture
}

func videoSnapshot() MediaSnapshot {
	return MediaSnapshot{
		Media: timeline.Media{
			MediaID: "m1", URI: "https://blob.example/bucket/agentcut/p1/source/m1/video.mp4",
			BlobKey: "agentcut/p1/source/m1/video.mp4", Kind: timeline.MediaVideo,
			DurationInFrames: 90,
			Probe:            &timeline.MediaProbe{DurationMs: 3000, Width: 1280, Height: 720, HasAudio: true},
		},
		FPS: 30,
	}
}

func successfulLocal() LocalArtifactsResult {
	return LocalArtifactsResult{
		Probe:            &timeline.MediaProbe{DurationMs: 3000, Width: 1280, Height: 720, HasAudio: true},
		DurationInFrames: 90,
		ProbeStage:       StageResult{State: timeline.ProcessingSucceeded},
		ThumbnailKey:     "agentcut/p1/thumbnails/m1.jpg",
		ThumbnailURI:     "https://blob.example/bucket/agentcut/p1/thumbnails/m1.jpg",
		ThumbnailStage:   StageResult{State: timeline.ProcessingSucceeded},
		WaveformPeaks:    []float32{0.1, 0.8},
		WaveformStage:    StageResult{State: timeline.ProcessingSucceeded},
		ASRAudioURI:      "https://blob.example/bucket/agentcut/p1/derived/m1/asr.mp3",
		TranscriptStage:  StageResult{State: timeline.ProcessingPending},
		UnderstandingURI: "https://blob.example/bucket/agentcut/p1/source/m1/video.mp4",
	}
}

func TestPrepareMediaWorkflowArchivesExternalArtifactBeforeLocalProcessing(t *testing.T) {
	snapshot := videoSnapshot()
	snapshot.Media.URI = "https://artifacts.example/run/generated.mp4"
	snapshot.Media.BlobKey = ""
	local := successfulLocal()
	local.TranscriptStage = StageResult{State: timeline.ProcessingSkipped}
	env, capture := newPrepareEnv(t, snapshot, local,
		func(TranscribeInput) (TranscribeOutput, error) { return TranscribeOutput{}, nil },
		func(UnderstandInput) (UnderstandOutput, error) { return UnderstandOutput{Text: "产品演示"}, nil },
	)
	env.ExecuteWorkflow(WorkflowName, Input{
		ProjectID: "p1", MediaID: "m1", AgentKSessionID: "ses_1",
		ASRModel: "azure-whisper", UnderstandingModel: "gemini-3.1-flash-lite",
	})
	require.NoError(t, env.GetWorkflowError())

	capture.mu.Lock()
	defer capture.mu.Unlock()
	require.NotEmpty(t, capture.patches)
	assert.Equal(t, "agentcut/p1/source/m1/video.mp4", *capture.patches[0].BlobKey)
	assert.Equal(t, "https://blob.example/bucket/agentcut/p1/source/m1/video.mp4", *capture.patches[0].URI)
	require.NotEmpty(t, capture.localRequests)
	assert.Equal(t, "agentcut/p1/source/m1/video.mp4", capture.localRequests[0].Media.BlobKey)
}

func TestPrepareMediaWorkflowHappyPath(t *testing.T) {
	env, capture := newPrepareEnv(t, videoSnapshot(), successfulLocal(),
		func(TranscribeInput) (TranscribeOutput, error) {
			return TranscribeOutput{
				Language: "zh",
				Segments: []TranscribeSegment{{ID: 0, Start: 0.25, End: 1.75, Text: "你好，世界"}},
			}, nil
		},
		func(UnderstandInput) (UnderstandOutput, error) {
			return UnderstandOutput{Text: "产品演示"}, nil
		},
	)
	env.ExecuteWorkflow(WorkflowName, Input{
		ProjectID: "p1", MediaID: "m1",
		ASRModel: "azure-whisper", UnderstandingModel: "gemini-3.1-flash-lite",
	})
	require.NoError(t, env.GetWorkflowError())

	require.NotEmpty(t, capture.patches)
	final := capture.patches[len(capture.patches)-1].Processing
	require.NotNil(t, final)
	assert.Equal(t, "ready", final.Status)
	assert.True(t, final.ReadyForTimeline)
	assert.Equal(t, timeline.ProcessingSucceeded, final.Transcript)
	assert.Equal(t, timeline.ProcessingSucceeded, final.Understanding)

	require.Len(t, capture.transcribeInputs, 1)
	assert.Equal(t, TranscribeInput{
		Model:    "azure-whisper",
		AudioURL: "https://blob.example/bucket/agentcut/p1/derived/m1/asr.mp3",
		Task:     "transcribe",
	}, capture.transcribeInputs[0])
	require.Len(t, capture.understandInputs, 1)
	assert.Equal(t, "gemini-3.1-flash-lite", capture.understandInputs[0].Model)
	assert.Equal(t, []string{"https://blob.example/bucket/agentcut/p1/source/m1/video.mp4"}, capture.understandInputs[0].MediaURLs)

	var transcript *timeline.Transcript
	for _, patch := range capture.patches {
		if patch.Transcript != nil {
			transcript = patch.Transcript
		}
	}
	require.NotNil(t, transcript)
	assert.Equal(t, int64(250), transcript.Segments[0].StartMs)
	assert.Equal(t, int64(1750), transcript.Segments[0].EndMs)

	doc := timeline.NewDoc("prepared", 1920, 1080, 30)
	doc.Media = []timeline.Media{videoSnapshot().Media}
	authoritative, _, applyErr := timeline.ApplyOps(doc, []timeline.Operation{{
		Op: timeline.OpUpdateMedia, MediaID: "m1",
		MediaPatch: &timeline.MediaPatch{Transcript: transcript},
	}})
	require.NoError(t, applyErr)
	require.NotNil(t, authoritative.Media[0].Transcript)
	assert.Equal(t, uint64(1), authoritative.Media[0].Transcript.Revision)
	assert.NotEmpty(t, authoritative.Media[0].Transcript.Segments[0].SegmentID)
}

func TestPrepareMediaWorkflowNoAudioSkipsWaveformAndTranscript(t *testing.T) {
	snapshot := videoSnapshot()
	snapshot.Media.Probe.HasAudio = false
	local := successfulLocal()
	local.Probe.HasAudio = false
	local.WaveformPeaks = nil
	local.WaveformStage = StageResult{State: timeline.ProcessingSkipped}
	local.ASRAudioURI = ""
	local.TranscriptStage = StageResult{State: timeline.ProcessingSkipped}
	env, capture := newPrepareEnv(t, snapshot, local,
		func(TranscribeInput) (TranscribeOutput, error) {
			return TranscribeOutput{}, errors.New("must not be called")
		},
		func(UnderstandInput) (UnderstandOutput, error) {
			return UnderstandOutput{Text: "无声视频"}, nil
		},
	)
	env.ExecuteWorkflow(WorkflowName, Input{
		ProjectID: "p1", MediaID: "m1",
		ASRModel: "azure-whisper", UnderstandingModel: "gemini-3.1-flash-lite",
	})
	require.NoError(t, env.GetWorkflowError())
	assert.Empty(t, capture.transcribeInputs)
	final := capture.patches[len(capture.patches)-1].Processing
	assert.Equal(t, timeline.ProcessingSkipped, final.Waveform)
	assert.Equal(t, timeline.ProcessingSkipped, final.Transcript)
	assert.Equal(t, "ready", final.Status)
}

func TestPrepareMediaWorkflowCapabilityFailuresArePartial(t *testing.T) {
	env, capture := newPrepareEnv(t, videoSnapshot(), successfulLocal(),
		func(TranscribeInput) (TranscribeOutput, error) {
			return TranscribeOutput{}, temporal.NewNonRetryableApplicationError("ASR unavailable", "Configuration", nil)
		},
		func(UnderstandInput) (UnderstandOutput, error) {
			return UnderstandOutput{}, temporal.NewNonRetryableApplicationError("understanding timeout", "Timeout", nil)
		},
	)
	env.ExecuteWorkflow(WorkflowName, Input{
		ProjectID: "p1", MediaID: "m1",
		ASRModel: "azure-whisper", UnderstandingModel: "gemini-3.1-flash-lite",
	})
	err := env.GetWorkflowError()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MEDIA_PREPARE_PARTIAL")
	final := capture.patches[len(capture.patches)-1].Processing
	assert.Equal(t, "partial", final.Status)
	assert.True(t, final.ReadyForTimeline)
	assert.Equal(t, timeline.ProcessingFailed, final.Transcript)
	assert.Equal(t, timeline.ProcessingFailed, final.Understanding)
	assert.NotEmpty(t, final.Error)
}
