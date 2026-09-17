package hub

import (
	"context"
	"time"
)

type MaintenanceLogger func(format string, args ...any)

// RunMaintenance performs one asynchronous retention pass immediately and then
// repeats on the configured interval until ctx is cancelled. Pruning itself is
// batched, so request traffic can make progress between delete transactions.
func RunMaintenance(ctx context.Context, store *Store, config Config, logf MaintenanceLogger) {
	if store == nil {
		return
	}
	policy := RetentionPolicyFromConfig(config)
	run := func() {
		started := time.Now()
		result, err := store.PruneEvents(ctx, time.Now().UTC(), policy)
		if err != nil {
			if ctx.Err() == nil && logf != nil {
				logf("[hub-maintenance] event retention failed: %v", err)
			}
			return
		}
		if result.Deleted > 0 && logf != nil {
			logf(
				"[hub-maintenance] pruned=%d agents=%d elapsed=%s",
				result.Deleted,
				result.Agents,
				time.Since(started).Round(time.Millisecond),
			)
		}
	}

	run()
	if ctx.Err() != nil {
		return
	}
	ticker := time.NewTicker(config.MaintenanceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
