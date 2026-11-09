// Package keffects provides the thin gRPC client surface AgentK consumers need
// to run published keffects workflows.
//
// A typical media workflow spans several calls:
//
//  1. Call StageMedia for each input URL, then use the returned Media values
//     to build the workflow-specific input and its SubmitInput.
//  2. Set a stable RequestID, call Submit, and persist the returned instance ID.
//     Submission accepts the task; it does not wait for generation to finish.
//     Reuse the RequestID when retrying an uncertain submission.
//  3. Poll FetchResult with that instance ID. CodeProcessing means keep polling;
//     CodeFailed is a task failure. The caller controls polling and its deadline.
//  4. On CodeSuccess, use the workflow-specific Parse function. The service can
//     report success before outputs become available: ErrOutputMissing means
//     keep polling the same instance, not submit another task.
//  5. For media outputs, call OutputURI on the parsed Media values.
//
// FetchResult also copies successful media outputs when OutputBucket is set,
// and returns Media values pointing to that bucket. A copy error can occur
// after generation has succeeded; retry FetchResult for the same instance
// rather than resubmitting the workflow. MediaStore.Copy must tolerate repeats,
// including outputs already copied before another output failed.
//
// OutputURI returns a storage URL, not a public or signed download URL.
// Consumers that pass results to external services must publish them separately.
package keffects
