package worker

import (
	"fmt"
	"strings"
	"testing"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
	sdkdiscovery "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/discovery"
	"go.temporal.io/sdk/activity"
	temporalworker "go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

type fakeTemporalWorker struct {
	temporalworker.Worker

	startCalls             int
	runCalls               int
	stopCalls              int
	registeredWorkflowName string
	registeredActivityName string
	registeredActivity     any
}

var _ temporalworker.Worker = (*fakeTemporalWorker)(nil)

func (f *fakeTemporalWorker) RegisterWorkflowWithOptions(_ any, options workflow.RegisterOptions) {
	f.registeredWorkflowName = options.Name
}

func (f *fakeTemporalWorker) RegisterActivityWithOptions(
	handler any,
	options activity.RegisterOptions,
) {
	f.registeredActivityName = options.Name
	f.registeredActivity = handler
}

func (f *fakeTemporalWorker) Start() error {
	f.startCalls++
	return nil
}

func (f *fakeTemporalWorker) Run(<-chan interface{}) error {
	f.runCalls++
	return nil
}

func (f *fakeTemporalWorker) Stop() {
	f.stopCalls++
}

func TestWorkerPromotesNativeRegistrationMethods(t *testing.T) {
	native := &fakeTemporalWorker{}
	w := newWorker(native, "demo-queue")
	w.RegisterWorkflowWithOptions(
		func(workflow.Context) error { return nil },
		workflow.RegisterOptions{Name: "demo.workflow"},
	)
	if native.registeredWorkflowName != "demo.workflow" {
		t.Fatalf("registered workflow = %q", native.registeredWorkflowName)
	}
}

func TestWorkerStartValidatesDelegatesAndFreezes(t *testing.T) {
	native := &fakeTemporalWorker{}
	w := newWorker(native, "demo-queue")
	declared := validCapability("contrib.demo.capability")
	w.PublishWorkflow(declared)

	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	if native.startCalls != 1 {
		t.Fatalf("start calls = %d", native.startCalls)
	}
	if native.registeredActivityName != sdkdiscovery.DescribeActivityType {
		t.Fatalf("describe activity = %q", native.registeredActivityName)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("publish after Start must panic")
		}
	}()
	w.PublishWorkflow(validCapability("contrib.demo.late"))
}

func TestWorkerRejectsInvalidManifestBeforeStart(t *testing.T) {
	native := &fakeTemporalWorker{}
	w := newWorker(native, "")
	w.PublishWorkflow(validCapability("contrib.demo.capability"))

	if err := w.Start(); err == nil {
		t.Fatal("expected error")
	}
	if native.startCalls != 0 {
		t.Fatalf("native worker started %d times", native.startCalls)
	}
}

func TestWorkerRejectsInvalidLaneBeforeStart(t *testing.T) {
	native := &fakeTemporalWorker{}
	w := newWorkerWithLane(native, workerLane{
		baseQueue: "demo-queue",
		pollQueue: "demo-queue",
		err:       fmt.Errorf("invalid lane"),
	}, newManifestCollector())
	w.PublishWorkflow(validCapability("contrib.demo.capability"))

	if err := w.Start(); err == nil || !strings.Contains(err.Error(), "invalid worker lane") {
		t.Fatalf("error = %v", err)
	}
	if native.startCalls != 0 {
		t.Fatalf("native worker started %d times", native.startCalls)
	}
}

func TestWorkerStartRejectsAgentKTaskQueue(t *testing.T) {
	native := &fakeTemporalWorker{}
	w := newWorker(native, "agentk")
	w.PublishWorkflow(validCapability("contrib.demo.capability"))

	err := w.Start()
	if err == nil || !strings.Contains(err.Error(), `task queue must not be "agentk"`) {
		t.Fatalf("error = %v", err)
	}
	if native.startCalls != 0 {
		t.Fatalf("native worker started %d times", native.startCalls)
	}
}

func TestWorkerRunRejectsAgentKCapability(t *testing.T) {
	native := &fakeTemporalWorker{}
	w := newWorker(native, "demo-queue")
	w.PublishWorkflow(validCapability("agentk.capability"))

	err := w.Run(make(chan interface{}))
	if err == nil || !strings.Contains(err.Error(), "must match contrib.<owner>.<name>") {
		t.Fatalf("error = %v", err)
	}
	if native.runCalls != 0 {
		t.Fatalf("native worker ran %d times", native.runCalls)
	}
}

func TestWorkerRunValidatesAndDelegates(t *testing.T) {
	native := &fakeTemporalWorker{}
	w := newWorker(native, "demo-queue")
	declared := validCapability("contrib.demo.activity")
	declared.Tool = &capability.Tool{}
	w.PublishActivity(declared)

	if err := w.Run(make(chan interface{})); err != nil {
		t.Fatal(err)
	}
	if native.runCalls != 1 {
		t.Fatalf("run calls = %d", native.runCalls)
	}
}
