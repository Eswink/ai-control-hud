package hub

import (
	"context"
	"runtime"
	"testing"
	"time"
)

func TestMaintenanceWorkersDoNotLeakGoroutines(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	before := settledGoroutineCount()
	config := testConfig(":memory:")
	for i := 0; i < 40; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			RunMaintenance(ctx, store, config, nil)
		}()
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("maintenance worker %d did not stop", i)
		}
	}
	after := settledGoroutineCount()
	if after > before+4 {
		t.Fatalf("maintenance goroutine growth did not settle: before=%d after=%d", before, after)
	}
}

func settledGoroutineCount() int {
	runtime.GC()
	time.Sleep(75 * time.Millisecond)
	return runtime.NumGoroutine()
}
