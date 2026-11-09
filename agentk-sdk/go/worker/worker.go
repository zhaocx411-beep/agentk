// Package worker adds AgentK capability publication to Temporal workers.
package worker

import (
	"context"
	"fmt"
	"log"
	"sync"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/interceptor"
	temporalworker "go.temporal.io/sdk/worker"
)

// Worker wraps a Temporal Worker and publishes selected workflows and
// activities as AgentK capabilities. Register implementations through the
// embedded Temporal Worker, then call PublishWorkflow or PublishActivity before
// starting the Worker.
//
// Starting the Worker freezes its capability declarations. The Worker then
// announces the frozen declarations in the background until it stops;
// announcement failures are retried without stopping workflow or activity
// processing.
type Worker struct {
	temporalworker.Worker

	workerLane workerLane
	collector  *manifestCollector
	announce   announceFunc

	reporterMu     sync.Mutex
	reporterCancel context.CancelFunc
	reporterDone   chan struct{}
}

// New creates a Worker for taskQueue using the supplied Temporal client and
// worker options. Contributor workers must use their own task queue rather than
// the AgentK core task queue.
//
// New appends the SDK telemetry interceptor (see TelemetryInterceptor) to
// options.Interceptors; the published capabilities are instrumented once the
// Worker starts.
//
// The caller retains ownership of c and must close it separately.
func New(
	c client.Client,
	taskQueue string,
	options temporalworker.Options,
) *Worker {
	lane := readWorkerLane(taskQueue)
	collector := newManifestCollector()
	interceptors := make([]interceptor.WorkerInterceptor, 0, len(options.Interceptors)+1)
	interceptors = append(interceptors, options.Interceptors...)
	options.Interceptors = append(interceptors, newTelemetryInterceptor(collector.isCapability))
	w := newWorkerWithLane(temporalworker.New(c, lane.pollQueue, options), lane, collector)
	w.announce = temporalAnnounce(c)
	return w
}

// newWorker 只供同包测试注入 fake；公开 API 只有 New 一条构造路径。
func newWorker(native temporalworker.Worker, taskQueue string) *Worker {
	return newWorkerWithLane(native,
		workerLane{baseQueue: taskQueue, pollQueue: taskQueue}, newManifestCollector())
}

func newWorkerWithLane(
	native temporalworker.Worker,
	lane workerLane,
	collector *manifestCollector,
) *Worker {
	return &Worker{
		Worker:     native,
		workerLane: lane,
		collector:  collector,
	}
}

// PublishWorkflow publishes the contract for a workflow that is already
// registered with the embedded Temporal Worker. It does not register the
// workflow implementation.
//
// PublishWorkflow panics if the Worker has already started and its declarations
// have been frozen.
func (w *Worker) PublishWorkflow(declared capability.Capability) {
	w.collector.add(capability.CapabilityKindWorkflow, declared)
}

// PublishActivity publishes the contract for an activity that is already
// registered with the embedded Temporal Worker. It does not register the
// activity implementation.
//
// PublishActivity panics if the Worker has already started and its declarations
// have been frozen.
func (w *Worker) PublishActivity(declared capability.Capability) {
	w.collector.add(capability.CapabilityKindActivity, declared)
}

// Start validates and freezes the published capabilities, starts the embedded
// Temporal Worker, and begins announcing the capabilities in the background.
// An announcement failure does not make Start fail; the Worker retries while it
// remains running.
func (w *Worker) Start() error {
	manifest, err := w.prepareStart()
	if err != nil {
		return err
	}
	w.logRoute()
	if err := w.Worker.Start(); err != nil {
		return err
	}
	w.startReporter(manifest)
	return nil
}

// Run validates and freezes the published capabilities, announces them in the
// background, and runs the embedded Temporal Worker. Capability announcement
// stops when Run returns.
func (w *Worker) Run(interruptCh <-chan interface{}) error {
	manifest, err := w.prepareStart()
	if err != nil {
		return err
	}
	w.logRoute()
	w.startReporter(manifest)
	defer w.stopReporter()
	return w.Worker.Run(interruptCh)
}

func (w *Worker) logRoute() {
	lane := "baseline"
	if w.workerLane.lane != "" {
		lane = w.workerLane.lane
	}
	log.Printf("agentk worker route (lane=%s, base_task_queue=%s, task_queue=%s)",
		lane, w.workerLane.baseQueue, w.workerLane.pollQueue)
}

// Stop stops capability announcement before stopping the embedded Temporal
// Worker.
func (w *Worker) Stop() {
	w.stopReporter()
	w.Worker.Stop()
}

func (w *Worker) prepareStart() (capability.ManifestV1, error) {
	if w.workerLane.err != nil {
		return capability.ManifestV1{}, fmt.Errorf("agentk worker: invalid worker lane: %w", w.workerLane.err)
	}
	manifest, err := w.collector.buildAndFreeze(w.workerLane.baseQueue, w.workerLane.lane)
	if err != nil {
		return capability.ManifestV1{},
			fmt.Errorf("agentk worker: invalid capability manifest: %w", err)
	}
	w.registerDescribeActivity(manifest)
	return manifest, nil
}

func (w *Worker) startReporter(manifest capability.ManifestV1) {
	if w.announce == nil {
		return
	}
	w.reporterMu.Lock()
	defer w.reporterMu.Unlock()
	if w.reporterDone != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	w.reporterCancel = cancel
	w.reporterDone = done
	r := newReporter(coreTaskQueue, manifest, w.announce)
	go func() {
		defer close(done)
		r.run(ctx)
	}()
}

func (w *Worker) stopReporter() {
	w.reporterMu.Lock()
	cancel := w.reporterCancel
	done := w.reporterDone
	w.reporterCancel = nil
	w.reporterDone = nil
	w.reporterMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}
