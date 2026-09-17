package remote

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/events"
	"github.com/Eswink/ai-control-hud/agent/internal/store"
)

type Runtime struct {
	store   *store.SnapshotStore
	client  *Client
	outbox  *events.Outbox
	version string
	config  Config
	onError func(error)
	now     func() time.Time

	startOnce sync.Once
	closeOnce sync.Once
	wg        sync.WaitGroup
}

func NewRuntime(
	store *store.SnapshotStore,
	client *Client,
	outbox *events.Outbox,
	version string,
	config Config,
	onError func(error),
) *Runtime {
	config.normalize()
	return &Runtime{
		store:   store,
		client:  client,
		outbox:  outbox,
		version: version,
		config:  config,
		onError: onError,
		now:     time.Now,
	}
}

func (r *Runtime) Start(ctx context.Context) {
	r.startOnce.Do(func() {
		loops := 2
		if r.outbox != nil {
			loops += 2
		}
		r.wg.Add(loops)
		go r.snapshotLoop(ctx)
		go r.heartbeatLoop(ctx)
		if r.outbox != nil {
			go r.eventObserveLoop(ctx)
			go r.eventDeliveryLoop(ctx)
		}
	})
}

func (r *Runtime) Wait() {
	r.wg.Wait()
	if r.outbox != nil {
		r.closeOnce.Do(func() {
			if err := r.outbox.Close(); err != nil {
				r.report(fmt.Errorf("close event outbox: %w", err))
			}
		})
	}
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

func (r *Runtime) eventObserveLoop(ctx context.Context) {
	defer r.wg.Done()
	var nextMaintenance time.Time
	for {
		now := r.utcnow()
		if err := r.outbox.Observe(ctx, r.store.Get(), now); err != nil {
			if ctx.Err() != nil {
				return
			}
			r.report(fmt.Errorf("event observation: %w", err))
		}

		// Reuse the existing observation goroutine for low-frequency SQLite
		// hygiene instead of creating another long-lived maintenance goroutine.
		if nextMaintenance.IsZero() || !now.Before(nextMaintenance) {
			if _, err := r.outbox.Maintain(ctx, now, events.DefaultBaselineCompactAfter); err != nil {
				if ctx.Err() != nil {
					return
				}
				r.report(fmt.Errorf("event outbox maintenance: %w", err))
			}
			nextMaintenance = now.Add(events.DefaultOutboxMaintenanceEvery)
		}

		if !sleepContext(ctx, r.config.EventScanInterval) {
			return
		}
	}
}

func (r *Runtime) eventDeliveryLoop(ctx context.Context) {
	defer r.wg.Done()
	failures := 0
	for {
		pending, err := r.outbox.Pending(ctx, 100)
		if err == nil && len(pending) > 0 {
			err = r.client.UploadEvents(ctx, pending)
			if err == nil {
				ids := make([]string, 0, len(pending))
				for _, event := range pending {
					ids = append(ids, event.EventID)
				}
				err = r.outbox.Ack(ctx, ids)
			}
		}

		if err != nil {
			if ctx.Err() != nil {
				return
			}
			failures++
			r.report(fmt.Errorf("event delivery: %w", err))
		} else {
			failures = 0
		}

		delay := r.config.EventSendInterval
		if failures > 0 {
			delay = backoff(r.config.EventSendInterval, failures, r.config.MaxBackoff)
		}
		if !sleepContext(ctx, delay) {
			return
		}
	}
}

func (r *Runtime) utcnow() time.Time {
	if r.now == nil {
		return time.Now().UTC()
	}
	return r.now().UTC()
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
