package preparemedia

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"agentcut/timeline"
)

type starterRun struct{}

func (*starterRun) GetID() string                  { return "id" }
func (*starterRun) GetRunID() string               { return "run" }
func (*starterRun) Get(context.Context, any) error { return nil }
func (*starterRun) GetWithOptions(context.Context, any, client.WorkflowRunGetOptions) error {
	return nil
}

type fakeStarter struct {
	options client.StartWorkflowOptions
	err     error
}

func (s *fakeStarter) ExecuteWorkflow(
	_ context.Context, options client.StartWorkflowOptions, _ interface{}, _ ...interface{},
) (client.WorkflowRun, error) {
	s.options = options
	if s.err != nil {
		return nil, s.err
	}
	return &starterRun{}, nil
}

func TestStartUsesDeterministicIDAndAllowsFailedRetry(t *testing.T) {
	input := Input{ProjectID: "p1", MediaID: "m1"}
	starter := &fakeStarter{}
	started, err := Start(context.Background(), starter, "queue", input)
	require.NoError(t, err)
	assert.True(t, started)
	assert.Equal(t, "media-prepare:p1:m1", starter.options.ID)
	assert.Equal(t, "queue", starter.options.TaskQueue)
	assert.Equal(t, enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
		starter.options.WorkflowIDReusePolicy)

	starter.err = serviceerror.NewWorkflowExecutionAlreadyStarted("already", "id", "run")
	started, err = Start(context.Background(), starter, "queue", input)
	require.NoError(t, err)
	assert.False(t, started)
}

func TestProcessingStateTransitions(t *testing.T) {
	processing := InitialProcessing(timeline.Media{
		MediaID: "m1", Kind: timeline.MediaVideo,
		Probe: &timeline.MediaProbe{DurationMs: 1000, HasAudio: true},
	})
	assert.True(t, processing.ReadyForTimeline)
	assert.Equal(t, "processing", processing.Status)

	processing.Thumbnail = timeline.ProcessingSucceeded
	processing.Waveform = timeline.ProcessingSucceeded
	processing.Transcript = timeline.ProcessingFailed
	processing.Understanding = timeline.ProcessingSucceeded
	FinalizeProcessing(&processing)
	assert.Equal(t, "partial", processing.Status)
	assert.True(t, processing.ReadyForTimeline)

	processing.Probe = timeline.ProcessingFailed
	FinalizeProcessing(&processing)
	assert.Equal(t, "failed", processing.Status)
	assert.False(t, processing.ReadyForTimeline)
}

func TestLegacyMediaWithoutProcessingRemainsCompatible(t *testing.T) {
	var media timeline.Media
	require.NoError(t, json.Unmarshal([]byte(`{
		"mediaId":"m1","uri":"https://blob.example/a.mp4","kind":"video","durationInFrames":30
	}`), &media))
	assert.Nil(t, media.Processing)
	processing := InitialProcessing(media)
	assert.Equal(t, timeline.ProcessingPending, processing.Probe)
	assert.False(t, processing.ReadyForTimeline)
	assert.Zero(t, media.Generation)
	assert.Equal(t, "media-prepare:p1:m1", WorkflowID("p1", "m1", 0))
	assert.Equal(t, "media-prepare:p1:m1:g2", WorkflowID("p1", "m1", 2))
}
