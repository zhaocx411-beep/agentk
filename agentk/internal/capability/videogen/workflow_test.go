package videogen

import (
	"context"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"agentk/internal/tool"
)

func runVideoWorkflowTest[I any](
	t *testing.T,
	workflowFunc any,
	workflowType string,
	activityType string,
	input I,
) {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(workflowFunc, workflow.RegisterOptions{Name: workflowType})
	env.RegisterActivityWithOptions(
		func(context.Context, I) (ActivityOutput, error) {
			return ActivityOutput{Artifacts: []tool.ResultArtifact{{
				Type: "video", URI: "https://video.test/result.mp4",
			}}}, nil
		},
		activity.RegisterOptions{Name: activityType},
	)
	env.ExecuteWorkflow(workflowType, input)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	var out ActivityOutput
	if err := env.GetWorkflowResult(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Artifacts) != 1 || out.Artifacts[0].URI != "https://video.test/result.mp4" {
		t.Fatalf("out = %+v", out)
	}
}

func TestVideoWorkflowsDispatchTypedActivities(t *testing.T) {
	t.Run("kling t2v", func(t *testing.T) {
		runVideoWorkflowTest(t, KlingT2VWorkflow, KlingT2VWorkflowType,
			KlingT2VExecutionActivityName, KlingT2VInput{Model: "kling-3.0-t2v"})
	})
	t.Run("kling i2v", func(t *testing.T) {
		runVideoWorkflowTest(t, KlingI2VWorkflow, KlingI2VWorkflowType,
			KlingI2VExecutionActivityName, KlingI2VInput{Model: "kling-3.0"})
	})
	t.Run("kling omni", func(t *testing.T) {
		runVideoWorkflowTest(t, KlingOmniWorkflow, KlingOmniWorkflowType,
			KlingOmniExecutionActivityName, KlingOmniInput{Model: "kling-3.0-omni"})
	})
	t.Run("seedance", func(t *testing.T) {
		runVideoWorkflowTest(t, SeedanceWorkflow, SeedanceWorkflowType,
			SeedanceExecutionActivityName, SeedanceInput{Model: "doubao-seedance-2.0"})
	})
}

func TestVideoExecutionActivityRetryOptions(t *testing.T) {
	if videoExecutionActivityOptions.StartToCloseTimeout != videoWorkflowTimeout ||
		videoExecutionActivityOptions.ScheduleToCloseTimeout != videoWorkflowTimeout {
		t.Fatalf("activity timeouts = start %v schedule %v, want %v",
			videoExecutionActivityOptions.StartToCloseTimeout,
			videoExecutionActivityOptions.ScheduleToCloseTimeout,
			videoWorkflowTimeout)
	}
	if videoExecutionActivityOptions.RetryPolicy == nil ||
		videoExecutionActivityOptions.RetryPolicy.MaximumAttempts != 10 {
		t.Fatalf("retry policy = %+v, want maximum attempts 10",
			videoExecutionActivityOptions.RetryPolicy)
	}
}

func TestVideoWorkflowRetryLimit(t *testing.T) {
	t.Run("permanent failure stops immediately", func(t *testing.T) {
		attempts := executeFailingVideoWorkflow(t,
			temporal.NewNonRetryableApplicationError(
				"rejected", "UpstreamRequestRejected", nil))
		if attempts != 1 {
			t.Fatalf("attempts = %d, want 1", attempts)
		}
	})
	t.Run("transient failure stops at budget", func(t *testing.T) {
		attempts := executeFailingVideoWorkflow(t,
			temporal.NewApplicationError("temporary", "Transient"))
		if attempts != 10 {
			t.Fatalf("attempts = %d, want 10", attempts)
		}
	})
}

func executeFailingVideoWorkflow(t *testing.T, activityErr error) int {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(KlingT2VWorkflow,
		workflow.RegisterOptions{Name: KlingT2VWorkflowType})
	attempts := 0
	env.RegisterActivityWithOptions(
		func(context.Context, KlingT2VInput) (ActivityOutput, error) {
			attempts++
			return ActivityOutput{}, activityErr
		},
		activity.RegisterOptions{Name: KlingT2VExecutionActivityName},
	)
	env.ExecuteWorkflow(KlingT2VWorkflowType, KlingT2VInput{Model: "kling-3.0-t2v"})
	if err := env.GetWorkflowError(); err == nil {
		t.Fatal("expected workflow failure")
	}
	return attempts
}

func TestVideoWorkflowCancellationCancelsExecutionActivity(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(KlingT2VWorkflow,
		workflow.RegisterOptions{Name: KlingT2VWorkflowType})
	env.RegisterActivityWithOptions(
		func(ctx context.Context, _ KlingT2VInput) (ActivityOutput, error) {
			<-ctx.Done()
			return ActivityOutput{}, ctx.Err()
		},
		activity.RegisterOptions{Name: KlingT2VExecutionActivityName},
	)
	env.RegisterDelayedCallback(env.CancelWorkflow, time.Second)
	env.ExecuteWorkflow(KlingT2VWorkflowType, KlingT2VInput{Model: "kling-3.0-t2v"})
	if err := env.GetWorkflowError(); !temporal.IsCanceledError(err) {
		t.Fatalf("error = %v, want canceled", err)
	}
}
