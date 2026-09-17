package remote

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
	"github.com/Eswink/ai-control-hud/agent/internal/events"
	agentruntime "github.com/Eswink/ai-control-hud/agent/internal/runtime"
)

func TestOutboxDiagnosticsNotConfiguredWithoutOutbox(t *testing.T) {
	runtime := &Runtime{}
	got := runtime.OutboxDiagnostics(context.Background(), time.Now().UTC())
	if got.Status != "not-configured" || got.PendingEvents != 0 {
		t.Fatalf("unexpected diagnostics %#v", got)
	}
}

func TestOutboxDiagnosticsWarnsForOldPendingEvent(t *testing.T) {
	outbox, err := events.Open(filepath.Join(t.TempDir(), "events.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer outbox.Close()

	old := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	baseline := diagnosticState(old)
	if err := outbox.Observe(context.Background(), baseline, old); err != nil {
		t.Fatal(err)
	}
	completedAt := old.Add(time.Minute)
	completed := diagnosticState(completedAt)
	completed.ZCode.Summary.Completed = 1
	completed.ZCode.Tasks = []domain.TaskSummary{{
		ID:        "task-1",
		Title:     "Task 1",
		Status:    domain.TaskCompleted,
		UpdatedAt: &completedAt,
	}}
	if err := outbox.Observe(context.Background(), completed, completedAt); err != nil {
		t.Fatal(err)
	}

	runtime := &Runtime{outbox: outbox}
	got := runtime.OutboxDiagnostics(context.Background(), completedAt.Add(25*time.Hour))
	if got.Status != "warning" || got.PendingEvents != 1 || got.OldestPendingAgeSeconds == nil {
		t.Fatalf("unexpected old-pending diagnostics %#v", got)
	}
	if *got.OldestPendingAgeSeconds < int64(24*time.Hour/time.Second) {
		t.Fatalf("pending age too small: %d", *got.OldestPendingAgeSeconds)
	}
}

func diagnosticState(now time.Time) domain.HudState {
	state := agentruntime.InitialState(now, "test", true, false)
	lastSuccess := now
	state.ZCode = domain.ZCodeState{
		Health: domain.SourceHealth{
			Status:        domain.SourceOK,
			ObservedAt:    now,
			LastSuccessAt: &lastSuccess,
		},
		Summary: &domain.ZCodeSummary{},
		Tasks:   []domain.TaskSummary{},
	}
	return state
}
