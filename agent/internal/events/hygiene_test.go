package events

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
)

func TestMaintainCompactsOldTerminalPayloadWithoutForgettingBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.sqlite3")
	outbox, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer outbox.Close()

	started := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if err := outbox.Observe(context.Background(), stateWithTasks(started, task("task-1", domain.TaskRunning, started)), started); err != nil {
		t.Fatal(err)
	}
	completedAt := started.Add(time.Minute)
	completed := stateWithTasks(completedAt, task("task-1", domain.TaskCompleted, completedAt))
	if err := outbox.Observe(context.Background(), completed, completedAt); err != nil {
		t.Fatal(err)
	}
	pending, err := outbox.Pending(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending=%d want 1", len(pending))
	}
	if err := outbox.Ack(context.Background(), []string{pending[0].EventID}); err != nil {
		t.Fatal(err)
	}

	now := started.Add(200 * 24 * time.Hour)
	maintenance, err := outbox.Maintain(context.Background(), now, DefaultBaselineCompactAfter)
	if err != nil {
		t.Fatal(err)
	}
	if maintenance.CompactedRows != 1 || maintenance.PendingEvents != 0 || maintenance.CompactedTaskRows != 1 {
		t.Fatalf("unexpected maintenance %#v", maintenance)
	}
	var title string
	var workspace, updatedAt *string
	if err := outbox.db.QueryRow(`SELECT title, workspace, updated_at FROM task_state WHERE task_id = 'task-1'`).Scan(&title, &workspace, &updatedAt); err != nil {
		t.Fatal(err)
	}
	if title != "" || workspace != nil || updatedAt != nil {
		t.Fatalf("old terminal payload was not compacted title=%q workspace=%v updated=%v", title, workspace, updatedAt)
	}

	// Re-observing the same terminal task must not create a duplicate semantic
	// event after payload compaction because task_id + terminal status remain.
	if err := outbox.Observe(context.Background(), completed, now); err != nil {
		t.Fatal(err)
	}
	assertOutboxCount(t, outbox, 0)
	stats, err := outbox.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.TaskBaselineRows != 1 || stats.CompactedTaskRows != 0 {
		t.Fatalf("re-observation should restore payload %#v", stats)
	}
}

func TestMaintainNeverDeletesPendingEvents(t *testing.T) {
	outbox, err := Open(filepath.Join(t.TempDir(), "events.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer outbox.Close()

	started := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if err := outbox.Observe(context.Background(), stateWithTasks(started), started); err != nil {
		t.Fatal(err)
	}
	completedAt := started.Add(time.Minute)
	if err := outbox.Observe(context.Background(), stateWithTasks(completedAt, task("pending-task", domain.TaskCompleted, completedAt)), completedAt); err != nil {
		t.Fatal(err)
	}
	before, err := outbox.Pending(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 {
		t.Fatalf("pending=%d want 1", len(before))
	}

	maintenance, err := outbox.Maintain(context.Background(), started.Add(400*24*time.Hour), DefaultBaselineCompactAfter)
	if err != nil {
		t.Fatal(err)
	}
	after, err := outbox.Pending(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if maintenance.PendingEvents != 1 || len(after) != 1 || after[0].EventID != before[0].EventID {
		t.Fatalf("maintenance changed pending durable event before=%#v after=%#v maintenance=%#v", before, after, maintenance)
	}
}

func TestMaintainCompactsTerminalBaselineInBoundedBatches(t *testing.T) {
	outbox, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer outbox.Close()

	old := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tasks := make([]domain.TaskSummary, 0, DefaultBaselineCompactBatch+5)
	for i := 0; i < DefaultBaselineCompactBatch+5; i++ {
		tasks = append(tasks, task(fmt.Sprintf("old-%04d", i), domain.TaskCompleted, old))
	}
	// First trustworthy snapshot establishes a baseline without creating events.
	if err := outbox.Observe(context.Background(), stateWithTasks(old, tasks...), old); err != nil {
		t.Fatal(err)
	}
	now := old.Add(200 * 24 * time.Hour)
	first, err := outbox.Maintain(context.Background(), now, DefaultBaselineCompactAfter)
	if err != nil {
		t.Fatal(err)
	}
	if first.CompactedRows != DefaultBaselineCompactBatch || first.CompactedTaskRows != DefaultBaselineCompactBatch {
		t.Fatalf("first bounded compaction %#v", first)
	}
	second, err := outbox.Maintain(context.Background(), now, DefaultBaselineCompactAfter)
	if err != nil {
		t.Fatal(err)
	}
	if second.CompactedRows != 5 || second.CompactedTaskRows != DefaultBaselineCompactBatch+5 {
		t.Fatalf("second bounded compaction %#v", second)
	}
}

func TestMaintainLeavesOldNonTerminalTaskPayloadIntact(t *testing.T) {
	outbox, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer outbox.Close()

	old := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if err := outbox.Observe(context.Background(), stateWithTasks(old, task("running", domain.TaskRunning, old)), old); err != nil {
		t.Fatal(err)
	}
	maintenance, err := outbox.Maintain(context.Background(), old.Add(200*24*time.Hour), DefaultBaselineCompactAfter)
	if err != nil {
		t.Fatal(err)
	}
	if maintenance.CompactedRows != 0 {
		t.Fatalf("running baseline compacted unexpectedly %#v", maintenance)
	}
	var title string
	if err := outbox.db.QueryRow(`SELECT title FROM task_state WHERE task_id = 'running'`).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title == "" {
		t.Fatal("running baseline payload was cleared")
	}
}
