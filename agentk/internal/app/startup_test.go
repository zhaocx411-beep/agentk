package app

import (
	"errors"
	"testing"
)

type embeddedWorkerStub struct {
	startErr error
	started  bool
	stopped  bool
}

func (w *embeddedWorkerStub) Start() error {
	w.started = true
	return w.startErr
}

func (w *embeddedWorkerStub) Stop() { w.stopped = true }

func TestStartEmbeddedWorker(t *testing.T) {
	t.Run("deployed does not construct worker", func(t *testing.T) {
		constructed := false
		stop, err := startEmbeddedWorker(ModeDeployed, temporalTaskQueue, func() embeddedWorker {
			constructed = true
			return &embeddedWorkerStub{}
		})
		if err != nil {
			t.Fatal(err)
		}
		stop()
		if constructed {
			t.Fatal("deployed API constructed an embedded Worker")
		}
	})

	t.Run("standalone owns worker lifecycle", func(t *testing.T) {
		worker := &embeddedWorkerStub{}
		stop, err := startEmbeddedWorker(ModeStandalone, temporalTaskQueue,
			func() embeddedWorker { return worker })
		if err != nil {
			t.Fatal(err)
		}
		if !worker.started || worker.stopped {
			t.Fatalf("worker after start = %+v", worker)
		}
		stop()
		if !worker.stopped {
			t.Fatal("embedded Worker was not stopped")
		}
	})

	t.Run("start error is returned", func(t *testing.T) {
		worker := &embeddedWorkerStub{startErr: errors.New("boom")}
		stop, err := startEmbeddedWorker(ModeStandalone, temporalTaskQueue,
			func() embeddedWorker { return worker })
		if err == nil {
			t.Fatal("expected start error")
		}
		if stop != nil {
			t.Fatal("failed start returned a stop function")
		}
		if worker.stopped {
			t.Fatal("failed Worker was stopped despite never starting")
		}
	})
}
