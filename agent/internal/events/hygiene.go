package events

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	DefaultBaselineCompactAfter   = 180 * 24 * time.Hour
	DefaultOutboxMaintenanceEvery = 6 * time.Hour
	DefaultBaselineCompactBatch   = 1000
)

type OutboxStats struct {
	PendingEvents     int
	TaskBaselineRows  int
	CompactedTaskRows int
	PageCount         int64
	FreelistPages     int64
	PageSizeBytes     int64
	ReusableBytes     int64
	OldestPendingAt   *time.Time
}

type OutboxMaintenanceResult struct {
	CompactedRows      int
	PendingEvents      int
	TaskBaselineRows   int
	CompactedTaskRows  int
	CheckpointBusy     int
	CheckpointLogPages int
	CheckpointedPages  int
}

func (o *Outbox) Stats(ctx context.Context) (OutboxStats, error) {
	if o == nil || o.db == nil {
		return OutboxStats{}, errors.New("event outbox is not open")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.statsLocked(ctx)
}

func (o *Outbox) statsLocked(ctx context.Context) (OutboxStats, error) {
	var stats OutboxStats
	var oldestRaw *string
	if err := o.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM event_outbox),
			(SELECT COUNT(*) FROM task_state),
			(SELECT COUNT(*) FROM task_state WHERE title = '' AND status IN ('completed', 'failed')),
			(SELECT MIN(created_at) FROM event_outbox)
	`).Scan(
		&stats.PendingEvents,
		&stats.TaskBaselineRows,
		&stats.CompactedTaskRows,
		&oldestRaw,
	); err != nil {
		return OutboxStats{}, fmt.Errorf("read event outbox stats: %w", err)
	}
	if oldestRaw != nil && *oldestRaw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, *oldestRaw)
		if err != nil {
			return OutboxStats{}, fmt.Errorf("decode oldest pending event time: %w", err)
		}
		parsed = parsed.UTC()
		stats.OldestPendingAt = &parsed
	}
	for query, target := range map[string]*int64{
		"PRAGMA page_count":     &stats.PageCount,
		"PRAGMA freelist_count": &stats.FreelistPages,
		"PRAGMA page_size":      &stats.PageSizeBytes,
	} {
		if err := o.db.QueryRowContext(ctx, query).Scan(target); err != nil {
			return OutboxStats{}, fmt.Errorf("read event outbox storage pragma %q: %w", query, err)
		}
	}
	stats.ReusableBytes = stats.FreelistPages * stats.PageSizeBytes
	return stats, nil
}

// Maintain bounds metadata growth without weakening at-least-once delivery.
// It never deletes pending event_outbox rows and never forgets task identity or
// terminal status. Old terminal task baselines only have their display payload
// fields compacted after they have not been observed for compactAfter.
func (o *Outbox) Maintain(ctx context.Context, now time.Time, compactAfter time.Duration) (OutboxMaintenanceResult, error) {
	if o == nil || o.db == nil {
		return OutboxMaintenanceResult{}, errors.New("event outbox is not open")
	}
	if compactAfter < 24*time.Hour {
		return OutboxMaintenanceResult{}, errors.New("task baseline compact age must be at least 24 hours")
	}
	o.mu.Lock()
	defer o.mu.Unlock()

	cutoff := now.UTC().Add(-compactAfter).Format(time.RFC3339Nano)
	result, err := o.db.ExecContext(ctx, `
		UPDATE task_state
		SET title = '', workspace = NULL, updated_at = NULL
		WHERE task_id IN (
			SELECT task_id
			FROM task_state
			WHERE status IN ('completed', 'failed')
			  AND julianday(last_seen_at) < julianday(?)
			  AND (title <> '' OR workspace IS NOT NULL OR updated_at IS NOT NULL)
			ORDER BY julianday(last_seen_at) ASC
			LIMIT ?
		)
	`, cutoff, DefaultBaselineCompactBatch)
	if err != nil {
		return OutboxMaintenanceResult{}, fmt.Errorf("compact terminal task baselines: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return OutboxMaintenanceResult{}, fmt.Errorf("read task baseline compaction result: %w", err)
	}

	if _, err := o.db.ExecContext(ctx, "PRAGMA optimize"); err != nil {
		return OutboxMaintenanceResult{}, fmt.Errorf("optimize event outbox: %w", err)
	}

	maintenance := OutboxMaintenanceResult{CompactedRows: int(rows)}
	var journalMode string
	if err := o.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		return OutboxMaintenanceResult{}, fmt.Errorf("read event outbox journal mode: %w", err)
	}
	if strings.EqualFold(strings.TrimSpace(journalMode), "wal") {
		if err := o.db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)").Scan(
			&maintenance.CheckpointBusy,
			&maintenance.CheckpointLogPages,
			&maintenance.CheckpointedPages,
		); err != nil {
			return OutboxMaintenanceResult{}, fmt.Errorf("checkpoint event outbox WAL: %w", err)
		}
	}
	stats, err := o.statsLocked(ctx)
	if err != nil {
		return OutboxMaintenanceResult{}, err
	}
	maintenance.PendingEvents = stats.PendingEvents
	maintenance.TaskBaselineRows = stats.TaskBaselineRows
	maintenance.CompactedTaskRows = stats.CompactedTaskRows
	return maintenance, nil
}
