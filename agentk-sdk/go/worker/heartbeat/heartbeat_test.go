package heartbeat

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestIntervalFor(t *testing.T) {
	for _, tc := range []struct {
		timeout time.Duration
		want    time.Duration
	}{
		{timeout: 0, want: defaultInterval},
		{timeout: 30 * time.Second, want: defaultInterval},
		{timeout: 8 * time.Second, want: 4 * time.Second},
	} {
		if got := intervalFor(tc.timeout); got != tc.want {
			t.Fatalf("intervalFor(%v) = %v, want %v", tc.timeout, got, tc.want)
		}
	}
}

func TestStartOutsideActivityIsNoop(t *testing.T) {
	Start(context.Background())()
}

func TestStartRecordsUntilStopped(t *testing.T) {
	var heartbeats atomic.Int32
	stop := start(context.Background(), time.Millisecond, func() {
		heartbeats.Add(1)
	})
	deadline := time.Now().Add(time.Second)
	for heartbeats.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if heartbeats.Load() < 2 {
		t.Fatalf("heartbeats = %d, want at least 2", heartbeats.Load())
	}
	stop()
	stoppedAt := heartbeats.Load()
	time.Sleep(5 * time.Millisecond)
	if got := heartbeats.Load(); got != stoppedAt {
		t.Fatalf("heartbeats continued after stop: %d -> %d", stoppedAt, got)
	}
}
