package worker

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
)

func TestReporterUsesFailureBackoffThenSuccessInterval(t *testing.T) {
	var sleeps []time.Duration
	calls := 0
	r := &reporter{
		taskQueue: "agentk",
		manifest:  capability.ManifestV1{TaskQueue: "business"},
		deps: reporterDeps{
			announce: func(context.Context, string, capability.ManifestV1) error {
				calls++
				if calls <= 2 {
					return errors.New("unavailable")
				}
				return nil
			},
			failureDelay: func(attempt int) time.Duration {
				return time.Duration(attempt) * time.Second
			},
			successDelay: func() time.Duration { return 5 * time.Minute },
			sleep: func(_ context.Context, delay time.Duration) bool {
				sleeps = append(sleeps, delay)
				return len(sleeps) < 4
			},
			logFailure: func(error) {},
		},
	}
	r.run(context.Background())

	want := []time.Duration{0, time.Second, 2 * time.Second, 5 * time.Minute}
	if !reflect.DeepEqual(sleeps, want) {
		t.Fatalf("sleeps = %v, want %v", sleeps, want)
	}
	if calls != 3 {
		t.Fatalf("announce calls = %d", calls)
	}
}

func TestAnnounceDelayBounds(t *testing.T) {
	for range 100 {
		delay := nextSuccessDelay()
		if delay < 12*time.Minute || delay > 18*time.Minute {
			t.Fatalf("success delay = %v", delay)
		}
	}
	for attempt := 1; attempt <= 20; attempt++ {
		delay := nextFailureDelay(attempt)
		if delay <= 0 || delay > time.Minute {
			t.Fatalf("failure delay %d = %v", attempt, delay)
		}
	}
}

func TestWorkerStartAnnouncesToAgentKQueue(t *testing.T) {
	native := &fakeTemporalWorker{}
	w := newWorker(native, "business")
	queues := make(chan string, 1)
	w.announce = func(
		_ context.Context,
		queue string,
		_ capability.ManifestV1,
	) error {
		queues <- queue
		return nil
	}
	w.PublishWorkflow(validCapability("contrib.example.summarize"))
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-queues:
		if got != coreTaskQueue {
			t.Fatalf("announce queue = %q, want %q", got, coreTaskQueue)
		}
	case <-time.After(time.Second):
		t.Fatal("startup announce not observed")
	}
	w.Stop()
	if native.stopCalls != 1 {
		t.Fatalf("stop calls = %d", native.stopCalls)
	}
}

func TestAnnounceFailureDoesNotFailStartedWorker(t *testing.T) {
	native := &fakeTemporalWorker{}
	w := newWorker(native, "business")
	called := make(chan struct{}, 1)
	w.announce = func(context.Context, string, capability.ManifestV1) error {
		called <- struct{}{}
		return errors.New("agent-k unavailable")
	}
	w.PublishWorkflow(validCapability("contrib.example.summarize"))
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("startup announce not observed")
	}
	if native.startCalls != 1 {
		t.Fatalf("native start calls = %d", native.startCalls)
	}
	w.Stop()
}

func TestDescribeActivityReturnsFrozenManifest(t *testing.T) {
	native := &fakeTemporalWorker{}
	w := newWorker(native, "business")
	w.PublishWorkflow(validCapability("contrib.example.summarize"))
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	describe, ok := native.registeredActivity.(func(context.Context) (capability.ManifestV1, error))
	if !ok {
		t.Fatalf("describe handler = %T", native.registeredActivity)
	}
	manifest, err := describe(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.TaskQueue != "business" ||
		len(manifest.Capabilities) != 1 ||
		manifest.Capabilities[0].ID != "contrib.example.summarize" {
		t.Fatalf("manifest = %+v", manifest)
	}
}
