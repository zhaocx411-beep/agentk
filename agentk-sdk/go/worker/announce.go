package worker

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	randv2 "math/rand/v2"
	"sync/atomic"
	"time"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/internal/workermetrics"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
	sdkdiscovery "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/discovery"
	"go.temporal.io/sdk/client"
)

const (
	coreTaskQueue      = "agentk"
	announceInterval   = 15 * time.Minute
	announceJitter     = 3 * time.Minute
	maxAnnounceBackoff = time.Minute
)

type announceFunc func(context.Context, string, capability.ManifestV1) error

type reporterDeps struct {
	announce     announceFunc
	successDelay func() time.Duration
	failureDelay func(attempt int) time.Duration
	sleep        func(context.Context, time.Duration) bool
	logFailure   func(error)
}

type reporter struct {
	taskQueue string
	manifest  capability.ManifestV1
	deps      reporterDeps
}

func newReporter(
	taskQueue string,
	manifest capability.ManifestV1,
	announce announceFunc,
) *reporter {
	return &reporter{
		taskQueue: taskQueue,
		manifest:  manifest,
		deps: reporterDeps{
			announce:     announce,
			successDelay: nextSuccessDelay,
			failureDelay: nextFailureDelay,
			sleep:        sleepContext,
			logFailure: func(err error) {
				slog.Warn("agentk capability manifest announce failed", "error", err)
			},
		},
	}
}

func (r *reporter) run(ctx context.Context) {
	delay := time.Duration(0)
	failures := 0
	for {
		if !r.deps.sleep(ctx, delay) {
			return
		}
		err := r.deps.announce(ctx, r.taskQueue, r.manifest)
		workermetrics.Announce(ctx, workermetrics.Status(err))
		if err != nil {
			failures++
			r.deps.logFailure(err)
			delay = r.deps.failureDelay(failures)
			continue
		}
		failures = 0
		delay = r.deps.successDelay()
	}
}

func nextSuccessDelay() time.Duration {
	return announceInterval - announceJitter +
		time.Duration(randv2.Int64N(int64(2*announceJitter)+1))
}

func nextFailureDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Second
	for range attempt - 1 {
		if delay >= maxAnnounceBackoff/2 {
			delay = maxAnnounceBackoff
			break
		}
		delay *= 2
	}
	// ±20% jitter, capped at the configured maximum.
	spread := delay / 5
	if spread > 0 {
		delay = delay - spread + time.Duration(randv2.Int64N(int64(2*spread)+1))
	}
	if delay > maxAnnounceBackoff {
		return maxAnnounceBackoff
	}
	return delay
}

func sleepContext(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func temporalAnnounce(c client.Client) announceFunc {
	return func(
		ctx context.Context,
		taskQueue string,
		manifest capability.ManifestV1,
	) error {
		run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
			ID:                       nextRegisterWorkflowID(manifest.TaskQueue),
			TaskQueue:                taskQueue,
			WorkflowExecutionTimeout: 2 * time.Minute,
		}, sdkdiscovery.RegisterWorkflowType, manifest)
		if err != nil {
			return fmt.Errorf("start discovery workflow: %w", err)
		}
		if err := run.Get(ctx, nil); err != nil {
			return fmt.Errorf("complete discovery workflow: %w", err)
		}
		return nil
	}
}

var workflowIDFallback atomic.Uint64

func nextRegisterWorkflowID(taskQueue string) string {
	var token [9]byte
	if _, err := rand.Read(token[:]); err == nil {
		return "agentk-discovery-register/" +
			base64.RawURLEncoding.EncodeToString([]byte(taskQueue)) + "/" +
			base64.RawURLEncoding.EncodeToString(token[:])
	}
	return fmt.Sprintf("agentk-discovery-register/%s/%d-%d",
		base64.RawURLEncoding.EncodeToString([]byte(taskQueue)),
		time.Now().UnixNano(),
		workflowIDFallback.Add(1),
	)
}
