package hub

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

type StorageStats struct {
	DatabaseBytes    int64
	WALBytes         int64
	EventCount       int64
	OldestSeq        int64
	LatestSeq        int64
	OldestReceivedAt *time.Time
	NewestReceivedAt *time.Time
	PageCount        int64
	FreePageCount    int64
	PageSize         int64
}

func (s StorageStats) ReusableBytes() int64 {
	return s.FreePageCount * s.PageSize
}

type SQLiteMaintenanceResult struct {
	CheckpointBusy   int
	LogFrames        int
	Checkpointed     int
}

// InspectDatabase reads bounded storage metadata without changing journal mode
// or creating a missing database. It is intended for the local `hub stats` CLI,
// not for a public HTTP diagnostics surface.
func InspectDatabase(ctx context.Context, path string) (StorageStats, error) {
	if path == "" {
		return StorageStats{}, errors.New("hub database path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return StorageStats{}, fmt.Errorf("stat hub database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return StorageStats{}, errors.New("hub database path is not a regular file")
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return StorageStats{}, fmt.Errorf("open hub database for stats: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		return StorageStats{}, fmt.Errorf("configure stats busy timeout: %w", err)
	}

	stats := StorageStats{DatabaseBytes: info.Size()}
	if wal, err := os.Stat(path + "-wal"); err == nil {
		stats.WALBytes = wal.Size()
	} else if !errors.Is(err, os.ErrNotExist) {
		return StorageStats{}, fmt.Errorf("stat Hub WAL: %w", err)
	}

	var oldestRaw, newestRaw sql.NullString
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(MIN(seq), 0), COALESCE(MAX(seq), 0), MIN(received_at), MAX(received_at)
		FROM events
	`).Scan(&stats.EventCount, &stats.OldestSeq, &stats.LatestSeq, &oldestRaw, &newestRaw); err != nil {
		return StorageStats{}, fmt.Errorf("query Hub event stats: %w", err)
	}
	if oldestRaw.Valid {
		value, err := time.Parse(time.RFC3339Nano, oldestRaw.String)
		if err != nil {
			return StorageStats{}, fmt.Errorf("decode oldest Hub event time: %w", err)
		}
		stats.OldestReceivedAt = &value
	}
	if newestRaw.Valid {
		value, err := time.Parse(time.RFC3339Nano, newestRaw.String)
		if err != nil {
			return StorageStats{}, fmt.Errorf("decode newest Hub event time: %w", err)
		}
		stats.NewestReceivedAt = &value
	}
	for query, target := range map[string]*int64{
		"PRAGMA page_count":     &stats.PageCount,
		"PRAGMA freelist_count": &stats.FreePageCount,
		"PRAGMA page_size":      &stats.PageSize,
	} {
		if err := db.QueryRowContext(ctx, query).Scan(target); err != nil {
			return StorageStats{}, fmt.Errorf("query Hub storage pragma %q: %w", query, err)
		}
	}
	return stats, nil
}

// OptimizeAndCheckpoint performs lightweight SQLite maintenance. PASSIVE WAL
// checkpointing never waits for readers; full VACUUM is intentionally excluded.
func (s *Store) OptimizeAndCheckpoint(ctx context.Context) (SQLiteMaintenanceResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.ExecContext(ctx, "PRAGMA optimize"); err != nil {
		return SQLiteMaintenanceResult{}, fmt.Errorf("optimize Hub database: %w", err)
	}
	var result SQLiteMaintenanceResult
	if err := s.db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)").Scan(
		&result.CheckpointBusy,
		&result.LogFrames,
		&result.Checkpointed,
	); err != nil {
		return SQLiteMaintenanceResult{}, fmt.Errorf("checkpoint Hub WAL: %w", err)
	}
	return result, nil
}
