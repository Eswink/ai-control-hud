package events

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
	agentruntime "github.com/Eswink/ai-control-hud/agent/internal/runtime"
)

func TestFirstSnapshotEstablishesBaselineWithoutHistoricalEvents(t *testing.T) {
	outbox, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer outbox.Close()

	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	state := stateWithTasks(now, task("task-1", domain.TaskCompleted, now))
	if err := outbox.Observe(context.Background(), state, now); err != nil {
		t.Fatal(err)
	}
	assertOutboxCount(t, outbox, 0)
}

func TestRunningToCompletedProducesOneDurableEvent(t *testing.T) {
	outbox, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer outbox.Close()

	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	if err := outbox.Observe(
		context.Background(),
		stateWithTasks(now, task("task-1", domain.TaskRunning, now)),
		now,
	); err != nil {
		t.Fatal(err)
	}

	completedAt := now.Add(2 * time.Second)
	completed := stateWithTasks(completedAt, task("task-1", domain.TaskCompleted, completedAt))
	if err := outbox.Observe(context.Background(), completed, completedAt); err != nil {
		t.Fatal(err)
	}
	if err := outbox.Observe(context.Background(), completed, completedAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	pending, err := outbox.Pending(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected one event, got %d", len(pending))
	}
	got := pending[0]
	if got.Type != TaskCompleted || got.Task.ID != "task-1" || got.Task.Status != domain.TaskCompleted {
		t.Fatalf("unexpected event %#v", got)
	}
	if !got.OccurredAt.Equal(completedAt) {
		t.Fatalf("occurredAt=%s want %s", got.OccurredAt, completedAt)
	}
	if len(got.EventID) != 32 {
		t.Fatalf("event id length=%d want 32", len(got.EventID))
	}
}

func TestNewTerminalTaskAfterBaselineProducesEvent(t *testing.T) {
	outbox, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer outbox.Close()

	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	if err := outbox.Observe(context.Background(), stateWithTasks(now), now); err != nil {
		t.Fatal(err)
	}
	failedAt := now.Add(time.Second)
	if err := outbox.Observe(
		context.Background(),
		stateWithTasks(failedAt, task("fast-task", domain.TaskFailed, failedAt)),
		failedAt,
	); err != nil {
		t.Fatal(err)
	}

	pending, err := outbox.Pending(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Type != TaskFailed {
		t.Fatalf("unexpected pending events %#v", pending)
	}
}

func TestBaselineAndPendingEventsSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.sqlite3")
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Observe(
		context.Background(),
		stateWithTasks(now, task("task-1", domain.TaskRunning, now)),
		now,
	); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	completedAt := now.Add(5 * time.Second)
	if err := second.Observe(
		context.Background(),
		stateWithTasks(completedAt, task("task-1", domain.TaskCompleted, completedAt)),
		completedAt,
	); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}

	third, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer third.Close()
	pending, err := third.Pending(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Type != TaskCompleted {
		t.Fatalf("restart lost event: %#v", pending)
	}
}

func TestAckRemovesOnlyAcknowledgedEvents(t *testing.T) {
	outbox, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer outbox.Close()

	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	if err := outbox.Observe(context.Background(), stateWithTasks(now), now); err != nil {
		t.Fatal(err)
	}
	if err := outbox.Observe(
		context.Background(),
		stateWithTasks(
			now.Add(time.Second),
			task("task-1", domain.TaskCompleted, now.Add(time.Second)),
			task("task-2", domain.TaskFailed, now.Add(time.Second)),
		),
		now.Add(time.Second),
	); err != nil {
		t.Fatal(err)
	}

	pending, err := outbox.Pending(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected two pending events, got %d", len(pending))
	}
	if err := outbox.Ack(context.Background(), []string{pending[0].EventID}); err != nil {
		t.Fatal(err)
	}
	remaining, err := outbox.Pending(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].EventID != pending[1].EventID {
		t.Fatalf("unexpected remaining events %#v", remaining)
	}
}

func TestStaleSnapshotsDoNotAdvanceEventBaseline(t *testing.T) {
	outbox, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer outbox.Close()

	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	running := stateWithTasks(now, task("task-1", domain.TaskRunning, now))
	if err := outbox.Observe(context.Background(), running, now); err != nil {
		t.Fatal(err)
	}

	completedAt := now.Add(time.Second)
	stale := stateWithTasks(completedAt, task("task-1", domain.TaskCompleted, completedAt))
	stale.ZCode.Health.Status = domain.SourceStale
	if err := outbox.Observe(context.Background(), stale, completedAt); err != nil {
		t.Fatal(err)
	}
	assertOutboxCount(t, outbox, 0)

	fresh := stateWithTasks(completedAt, task("task-1", domain.TaskCompleted, completedAt))
	if err := outbox.Observe(context.Background(), fresh, completedAt); err != nil {
		t.Fatal(err)
	}
	assertOutboxCount(t, outbox, 1)
}

func stateWithTasks(now time.Time, tasks ...domain.TaskSummary) domain.HudState {
	state := agentruntime.InitialState(now, "test", true, false)
	lastSuccess := now
	normalizedTasks := tasks
	if normalizedTasks == nil {
		normalizedTasks = []domain.TaskSummary{}
	}
	state.ZCode = domain.ZCodeState{
		Health: domain.SourceHealth{
			Status:        domain.SourceOK,
			ObservedAt:    now,
			LastSuccessAt: &lastSuccess,
		},
		Summary: &domain.ZCodeSummary{},
		Tasks:   normalizedTasks,
	}
	return state
}

func task(id string, status domain.TaskStatus, updatedAt time.Time) domain.TaskSummary {
	title := "Task " + id
	workspace := "backend"
	return domain.TaskSummary{
		ID:        id,
		Title:     title,
		Workspace: &workspace,
		Status:    status,
		UpdatedAt: &updatedAt,
	}
}

func assertOutboxCount(t *testing.T, outbox *Outbox, want int) {
	t.Helper()
	got, err := outbox.Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("outbox count=%d want %d", got, want)
	}
}
