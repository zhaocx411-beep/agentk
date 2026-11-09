package separateaudio

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

func Workflow(ctx workflow.Context, in Input) (Output, error) {
	activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 15 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval: time.Second, BackoffCoefficient: 2, MaximumAttempts: 3,
			NonRetryableErrorTypes: []string{
				"BAD_REQUEST", "REVISION_CONFLICT", "MEDIA_NOT_FOUND", "ITEM_NOT_FOUND",
				"NO_AUDIO", "LOCKED_TRACK", "BLOB_NOT_CONFIGURED", "MEDIA_TOO_LARGE",
			},
		},
	})
	var extracted ExtractResult
	if err := workflow.ExecuteActivity(
		activityCtx, ExtractActivityName, ExtractRequest{Input: in},
	).Get(activityCtx, &extracted); err != nil {
		return Output{}, fmt.Errorf("extract audio: %w", err)
	}
	var output Output
	if err := workflow.ExecuteActivity(
		activityCtx, CommitActivityName, CommitRequest{Input: in, Extract: extracted},
	).Get(activityCtx, &output); err != nil {
		if extracted.BlobKey != "" && !extracted.Reused {
			cleanupCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
				StartToCloseTimeout: time.Minute,
				RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
			})
			_ = workflow.ExecuteActivity(cleanupCtx, CleanupActivityName, CleanupRequest{
				ProjectID: in.ProjectID, ItemID: in.ItemID, BlobKey: extracted.BlobKey,
			}).Get(cleanupCtx, nil)
		}
		return Output{}, fmt.Errorf("commit separated audio: %w", err)
	}
	return output, nil
}
