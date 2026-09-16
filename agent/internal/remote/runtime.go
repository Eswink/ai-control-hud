package remote

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/store"
)

type Runtime struct {
	store   *store.SnapshotStore
	client  *Client
	version string
	config  Config
	onError func(error)

	startOnce sync.Once
	wg        sync.WaitGroup
}

func NewRuntime(
	store *store.SnapshotStore,
	client *Client,
	version string,
	config Config,
	onError func(error),
) *Runtime {
	config.normalize()
	return &Runtime{
		store:   store,
		client:  client,
		version: version,
		config:  config,
		onError: onError,
	}
}

func (r *Runtime) Start(ctx context.Context) {
	r.startOnce.Do(func() {
		r.wg.Add(2)
		go r.snapshotLoop(ctx)
		go r.heartbeatLoop(ctx)
	})
}

func (r *Runtime) Wait() {
	r.wg.Wait()
}

func (r *Runtime) snapshotLoop(ctx context.Context) {
	defer r.wg.Done()
	failures := 0
	for {
		err := r.client.UploadState(ctx, r.store.Get())
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			failures++
			r.report(fmt.Errorf("state upload: %w", err))
		} else {
			failures = 0
		}
		delay := r.config.SnapshotInterval
		if failures > 0 {
			delay = backoff(r.config.SnapshotInterval, failures, r.config.MaxBackoff)
		}
		if !sleepContext(ctx, delay) {
			return
		}
	}
}

func (r *Runtime) heartbeatLoop(ctx context.Context) {
	defer r.wg.Done()
	failures := 0
	for {
		err := r.client.Heartbeat(ctx, r.version)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			failures++
			r.report(fmt.Errorf("heartbeat: %w", err))
		} else {
			failures = 0
		}
		delay := r.config.HeartbeatInterval
		if failures > 0 {
			delay = backoff(r.config.HeartbeatInterval, failures, r.config.MaxBackoff)
		}
		if !sleepContext(ctx, delay) {
			return
		}
	}
}

func (r *Runtime) report(err error) {
	if r.onError != nil {
		r.onError(err)
	}
}

func backoff(base time.Duration, failures int, maximum time.Duration) time.Duration {
	if failures <= 0 {
		return base
	}
	delay := base
	for i := 1; i < failures; i++ {
		if delay >= maximum/2 {
			return maximum
		}
		delay *= 2
	}
	if delay > maximum {
		return maximum
	}
	return delay
}

func sleepContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
