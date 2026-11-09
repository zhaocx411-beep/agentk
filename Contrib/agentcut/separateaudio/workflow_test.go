package separateaudio

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"agentcut/timeline"
)

func TestWorkflowExtractsBeforeSingleAtomicCommit(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(Workflow)
	var order []string
	env.RegisterActivityWithOptions(func(ExtractRequest) (ExtractResult, error) {
		order = append(order, "extract")
		return ExtractResult{
			AudioMedia:  timeline.Media{MediaID: "audio", Kind: timeline.MediaAudio},
			AudioItemID: "audio-item", BlobKey: "source/audio.mp3",
		}, nil
	}, activity.RegisterOptions{Name: ExtractActivityName})
	env.RegisterActivityWithOptions(func(CommitRequest) (Output, error) {
		order = append(order, "commit")
		return Output{Revision: 2, AudioMediaID: "audio", AudioItemID: "audio-item"}, nil
	}, activity.RegisterOptions{Name: CommitActivityName})

	env.ExecuteWorkflow(Workflow, Input{
		ProjectID: "p1", ItemID: "clip", BaseRevision: 1, ClientMutation: "m1",
	})
	require.NoError(t, env.GetWorkflowError())
	assert.Equal(t, []string{"extract", "commit"}, order)
}

func TestWorkflowCleansUncommittedExtraction(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(Workflow)
	var cleaned bool
	env.RegisterActivityWithOptions(func(ExtractRequest) (ExtractResult, error) {
		return ExtractResult{
			AudioMedia:  timeline.Media{MediaID: "audio", Kind: timeline.MediaAudio},
			AudioItemID: "audio-item", BlobKey: "source/audio.mp3",
		}, nil
	}, activity.RegisterOptions{Name: ExtractActivityName})
	env.RegisterActivityWithOptions(func(CommitRequest) (Output, error) {
		return Output{}, errors.New("conflict")
	}, activity.RegisterOptions{Name: CommitActivityName})
	env.RegisterActivityWithOptions(func(CleanupRequest) error {
		cleaned = true
		return nil
	}, activity.RegisterOptions{Name: CleanupActivityName})

	env.ExecuteWorkflow(Workflow, Input{
		ProjectID: "p1", ItemID: "clip", BaseRevision: 1, ClientMutation: "m1",
	})
	require.Error(t, env.GetWorkflowError())
	assert.True(t, cleaned)
}
