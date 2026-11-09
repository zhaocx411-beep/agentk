package entity

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"agentcut/timeline"
)

func TestTimelineWorkflowRestoreSnapshotAndConflict(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(TimelineEntityWorkflow)
	before := baseInit()
	restored := before.Doc.Clone()
	restored.Metadata.Name = "restored"
	var success RestoreTimelineResult
	var successErr, conflictErr error
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(UpdateRestoreTimeline, "restore-ok", &testsuite.TestUpdateCallback{
			OnAccept: func() {},
			OnReject: func(err error) { successErr = err },
			OnComplete: func(value any, err error) {
				successErr = err
				if err == nil {
					successErr = decodeUpdateResult(value, &success)
				}
			},
		}, RestoreTimelineRequest{BaseRevision: 0, Source: "user", Doc: restored})
	}, time.Millisecond)
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(UpdateRestoreTimeline, "restore-conflict", &testsuite.TestUpdateCallback{
			OnAccept:   func() {},
			OnReject:   func(err error) { conflictErr = err },
			OnComplete: func(_ any, err error) { conflictErr = err },
		}, RestoreTimelineRequest{BaseRevision: 0, Source: "user", Doc: before.Doc})
	}, 2*time.Millisecond)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalDeleteTimeline, nil)
	}, 3*time.Millisecond)

	env.ExecuteWorkflow(TimelineEntityWorkflow, before)
	require.NoError(t, env.GetWorkflowError())
	require.NoError(t, successErr)
	assert.Equal(t, uint64(1), success.Revision)
	assert.Equal(t, "restored", success.Doc.Metadata.Name)
	require.Error(t, conflictErr)
	assert.Contains(t, conflictErr.Error(), ErrTypeRevisionConflict)
}

func TestRestoreValidatorRejectsNonUserAndInvalidDoc(t *testing.T) {
	valid := timeline.NewDoc("valid", 1920, 1080, 30)
	assert.NoError(t, ValidateRestoreTimelineRequest(RestoreTimelineRequest{Source: "user", Doc: valid}))
	assert.Error(t, ValidateRestoreTimelineRequest(RestoreTimelineRequest{Source: "agent", Doc: valid}))
	invalid := valid
	invalid.Metadata.FPS = 31
	assert.Error(t, ValidateRestoreTimelineRequest(RestoreTimelineRequest{Source: "user", Doc: invalid}))
}
