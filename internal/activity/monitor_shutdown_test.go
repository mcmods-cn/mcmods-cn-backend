package activity

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCanceledCloseStillStopsActivityWorkerAfterInflightWrite(t *testing.T) {
	store := &fakeActivityStore{writeEntered: make(chan struct{}, 1), releaseWrite: make(chan struct{})}
	monitor := newMonitor(store, Options{BatchSize: 1, QueueCapacity: 1, FlushInterval: time.Hour, WriteTimeout: time.Second})
	if !monitor.RecordBestEffort(testViewEvent()) {
		t.Fatal("initial view was not accepted")
	}
	select {
	case <-store.writeEntered:
	case <-time.After(time.Second):
		t.Fatal("worker did not start the write")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := monitor.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled close error=%v", err)
	}
	close(store.releaseWrite)
	select {
	case <-monitor.stopped:
	case <-time.After(time.Second):
		t.Fatal("canceled close left the worker running after the in-flight write completed")
	}
}
