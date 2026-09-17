package hub

import (
	"context"
	"time"
)

type MaintenanceLogger func(format string, args ...any)

// RunMaintenance performs one asynchronous retention/SQLite maintenance pass
// immediately and then repeats on the configured interval until ctx is
// cancelled. Pruning itself is batched, so request traffic can make progress
// between delete transactions. PASSIVE checkpointing never waits for readers.
func RunMaintenance(ctx context.Context, store *Store, config Config, logf MaintenanceLogger) {
	if store == nil {
		return
	}
	policy := RetentionPolicyFromConfig(config)
	_, _, _, interval := config.effectiveRetention()
	run := func() {
		started := time.Now()
		result, err := store.PruneEvents(ctx, time.Now().UTC(), policy)
		if err != nil {
			if ctx.Err() == nil && logf != nil {
				logf("[hub-maintenance] event retention failed: %v", err)
			}
			return
		}
		if ctx.Err() != nil {
			return
		}
		sqliteResult, err := store.OptimizeAndCheckpoint(ctx)
		if err != nil {
			if ctx.Err() == nil && logf != nil {
				logf("[hub-maintenance] SQLite maintenance failed: %v", err)
			}
			return
		}
		if logf != nil && (result.Deleted > 0 || sqliteResult.CheckpointBusy != 0) {
			logf(
				"[hub-maintenance] pruned=%d agents=%d checkpointBusy=%d logFrames=%d checkpointed=%d elapsed=%s",
				result.Deleted,
				result.Agents,
				sqliteResult.CheckpointBusy,
				sqliteResult.LogFrames,
				sqliteResult.Checkpointed,
				time.Since(started).Round(time.Millisecond),
			)
		}
	}

	run()
	if ctx.Err() != nil {
		return
	}
	ticker := time.NewTicker(interval)
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
