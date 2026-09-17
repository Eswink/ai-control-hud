package remote

import (
	"context"
	"io"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/events"
	agentruntime "github.com/Eswink/ai-control-hud/agent/internal/runtime"
	"github.com/Eswink/ai-control-hud/agent/internal/store"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestRuntimeStartCancelDoesNotLeakGoroutines(t *testing.T) {
	before := settledRemoteGoroutines()
	for i := 0; i < 30; i++ {
		now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
		state := agentruntime.InitialState(now, "test", false, false)
		stateStore, err := store.New(state)
		if err != nil {
			t.Fatal(err)
		}
		outbox, err := events.Open(":memory:")
		if err != nil {
			t.Fatal(err)
		}
		config, err := NewConfig("http://127.0.0.1:1", "desktop-main", "synthetic-token")
		if err != nil {
			t.Fatal(err)
		}
		// Short intervals exercise all four runtime loops without waiting for the
		// production cadence. The transport returns immediately and owns no
		// background connection pool goroutines.
		config.SnapshotInterval = time.Millisecond
		config.HeartbeatInterval = time.Millisecond
		config.EventScanInterval = time.Millisecond
		config.EventSendInterval = time.Millisecond
		config.MaxBackoff = 2 * time.Millisecond
		client, err := NewClient(config)
		if err != nil {
			t.Fatal(err)
		}
		client.http = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		})}
		runtimeLoop := NewRuntime(stateStore, client, outbox, "test", config, func(error) {})
		ctx, cancel := context.WithCancel(context.Background())
		runtimeLoop.Start(ctx)
		time.Sleep(5 * time.Millisecond)
		cancel()
		done := make(chan struct{})
		go func() {
			runtimeLoop.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("remote runtime %d did not stop after context cancellation", i)
		}
	}
	after := settledRemoteGoroutines()
	if after > before+4 {
		t.Fatalf("remote runtime goroutine growth did not settle: before=%d after=%d", before, after)
	}
}

func settledRemoteGoroutines() int {
	runtime.GC()
	time.Sleep(75 * time.Millisecond)
	return runtime.NumGoroutine()
}
