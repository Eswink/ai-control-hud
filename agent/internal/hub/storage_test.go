package hub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInspectDatabaseReportsEventsAndPages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.sqlite3")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 14, 0, 0, 0, time.UTC)
	if err := recordRetentionEvent(t.Context(), store, "desktop-main", 1, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := recordRetentionEvent(t.Context(), store, "desktop-main", 2, now); err != nil {
		t.Fatal(err)
	}

	maintenance, err := store.OptimizeAndCheckpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if maintenance.CheckpointBusy < 0 || maintenance.LogFrames < 0 || maintenance.Checkpointed < 0 {
		t.Fatalf("unexpected checkpoint result %#v", maintenance)
	}
	stats, err := InspectDatabase(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if stats.DatabaseBytes <= 0 || stats.PageSize <= 0 || stats.PageCount <= 0 {
		t.Fatalf("missing storage metrics %#v", stats)
	}
	if stats.EventCount != 2 || stats.OldestSeq != 1 || stats.LatestSeq != 2 {
		t.Fatalf("unexpected event storage stats %#v", stats)
	}
	if stats.OldestReceivedAt == nil || stats.NewestReceivedAt == nil || !stats.OldestReceivedAt.Equal(now.Add(-time.Hour)) || !stats.NewestReceivedAt.Equal(now) {
		t.Fatalf("unexpected event times %#v", stats)
	}
	if stats.ReusableBytes() != stats.FreePageCount*stats.PageSize {
		t.Fatalf("unexpected reusable bytes %#v", stats)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestInspectDatabaseDoesNotCreateMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.sqlite3")
	_, err := InspectDatabase(context.Background(), path)
	if err == nil {
		t.Fatal("expected missing database error")
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stats unexpectedly created database: %v", statErr)
	}
}

func TestOptimizeAndCheckpointPreservesData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.sqlite3")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 17, 14, 0, 0, 0, time.UTC)
	state := healthyState(now)
	if err := store.RecordState(t.Context(), "desktop-main", now, state, now); err != nil {
		t.Fatal(err)
	}
	if err := recordRetentionEvent(t.Context(), store, "desktop-main", 1, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.OptimizeAndCheckpoint(context.Background()); err != nil {
		t.Fatal(err)
	}
	loaded, lastSeen, found, err := store.LoadState(context.Background(), "desktop-main")
	if err != nil || !found || !lastSeen.Equal(now) || loaded.SchemaVersion != state.SchemaVersion {
		t.Fatalf("state changed after maintenance found=%t lastSeen=%v err=%v state=%#v", found, lastSeen, err, loaded)
	}
	items, latest, err := store.ListEvents(context.Background(), "desktop-main", 0, 100)
	if err != nil || len(items) != 1 || latest != 1 {
		t.Fatalf("events changed after maintenance len=%d latest=%d err=%v", len(items), latest, err)
	}
}
