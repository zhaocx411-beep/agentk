// Package heartbeat provides explicit keepalive heartbeats for Temporal Activities.
package heartbeat

import (
	"context"
	"time"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/internal/workermetrics"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/invocation"
	"go.temporal.io/sdk/activity"
)

const defaultInterval = 10 * time.Second

// Start immediately records an Activity heartbeat and keeps recording in the
// background until the returned stop function is called or ctx is canceled.
// The stop function waits for the background recorder to exit. Outside a
// Temporal Activity, Start returns a no-op stop function.
//
// Each heartbeat is counted in agentk.worker.heartbeats, attributed to the
// calling app when the Activity carries agentk invocation meta.
func Start(ctx context.Context) func() {
	if !activity.IsActivity(ctx) {
		return func() {}
	}
	meta, _ := invocation.MetaFromActivity(ctx)
	return start(ctx, intervalFor(activity.GetInfo(ctx).HeartbeatTimeout), func() {
		activity.RecordHeartbeat(ctx)
		workermetrics.Heartbeat(ctx, meta.AppID)
	})
}

func start(ctx context.Context, interval time.Duration, record func()) func() {
	heartbeatCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	record()
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				record()
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func intervalFor(timeout time.Duration) time.Duration {
	if timeout <= 0 || timeout >= 2*defaultInterval {
		return defaultInterval
	}
	interval := timeout / 2
	if interval <= 0 {
		return time.Nanosecond
	}
	return interval
}
