package zcode

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func createRuntimeSessionDB(t *testing.T, path string, rows []struct {
	status    string
	startedAt int64
}) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	statements := []string{
		`CREATE TABLE session (
			id TEXT PRIMARY KEY,
			directory TEXT NOT NULL,
			path TEXT,
			title TEXT NOT NULL
		)`,
		`CREATE TABLE model_usage (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			provider_id TEXT,
			model_id TEXT,
			status TEXT NOT NULL,
			started_at INTEGER NOT NULL
		)`,
		`INSERT INTO session VALUES ('session-normal', 'D:\\github\\research-system', 'D:\\github\\research-system', '项目中 Fake 实现是否应重构')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
	for i, row := range rows {
		if _, err := db.Exec(`INSERT INTO model_usage (id, session_id, provider_id, model_id, status, started_at) VALUES (?, ?, ?, ?, ?, ?)`,
			"usage-"+string(rune('a'+i)), "session-normal", "provider", "model", row.status, row.startedAt); err != nil {
			t.Fatal(err)
		}
	}
}

func TestActiveOrdinaryRuntimeSessionBecomesRunningTask(t *testing.T) {
	now := time.Date(2026, 9, 18, 0, 30, 0, 0, time.UTC)
	runtimeDB := filepath.Join(t.TempDir(), "db.sqlite")
	createRuntimeSessionDB(t, runtimeDB, []struct {
		status    string
		startedAt int64
	}{{status: "working", startedAt: now.Add(-101 * time.Second).UnixMilli()}})

	collector := New(runtimeDB, "")
	collector.Now = func() time.Time { return now }
	snapshot, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Summary.Running != 1 || len(snapshot.Tasks) != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	task := snapshot.Tasks[0]
	if task.Title != "项目中 Fake 实现是否应重构" {
		t.Fatalf("title = %q", task.Title)
	}
	if task.Workspace == nil || *task.Workspace != "research-system" {
		t.Fatalf("workspace = %#v", task.Workspace)
	}
	if task.Status != "running" {
		t.Fatalf("status = %q", task.Status)
	}
	if task.DurationSeconds == nil || *task.DurationSeconds != 101 {
		t.Fatalf("duration = %#v", task.DurationSeconds)
	}
	if strings.Contains(task.ID, "session-normal") || !strings.HasPrefix(task.ID, "zcode-") {
		t.Fatalf("unsafe id = %q", task.ID)
	}
}

func TestNewestTerminalRuntimeUsageSuppressesOlderRunningRow(t *testing.T) {
	now := time.Date(2026, 9, 18, 0, 30, 0, 0, time.UTC)
	runtimeDB := filepath.Join(t.TempDir(), "db.sqlite")
	createRuntimeSessionDB(t, runtimeDB, []struct {
		status    string
		startedAt int64
	}{
		{status: "running", startedAt: now.Add(-90 * time.Second).UnixMilli()},
		{status: "completed", startedAt: now.Add(-10 * time.Second).UnixMilli()},
	})

	collector := New(runtimeDB, "")
	collector.Now = func() time.Time { return now }
	_, err := collector.Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "task sources are unavailable") {
		t.Fatalf("expected no active runtime task, got %v", err)
	}
}

func TestStaleRuntimeRunningRowDoesNotResurrect(t *testing.T) {
	now := time.Date(2026, 9, 18, 0, 30, 0, 0, time.UTC)
	runtimeDB := filepath.Join(t.TempDir(), "db.sqlite")
	createRuntimeSessionDB(t, runtimeDB, []struct {
		status    string
		startedAt int64
	}{{status: "running", startedAt: now.Add(-11 * time.Minute).UnixMilli()}})

	collector := New(runtimeDB, "")
	collector.Now = func() time.Time { return now }
	_, err := collector.Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "task sources are unavailable") {
		t.Fatalf("expected stale row rejection, got %v", err)
	}
}

func TestOpenTurnOverridesTerminalModelUsage(t *testing.T) {
	now := time.Date(2026, 9, 18, 1, 30, 0, 0, time.UTC)
	root := t.TempDir()
	runtimeDB := filepath.Join(root, "db.sqlite")
	createRuntimeSessionDB(t, runtimeDB, []struct {
		status    string
		startedAt int64
	}{{status: "completed", startedAt: now.Add(-10 * time.Second).UnixMilli()}})
	logDir := filepath.Join(root, "log")
	writeTurnLog(t, logDir,
		turnLine("turn.started", "session-normal", now.Add(-75*time.Second)),
		turnLine("model.streaming", "session-normal", now.Add(-2*time.Second)),
	)

	collector := New(runtimeDB, "")
	collector.LogDir = logDir
	collector.TurnFreshSeconds = 1800
	collector.Now = func() time.Time { return now }
	snapshot, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Summary.Running != 1 || len(snapshot.Tasks) != 1 || snapshot.Tasks[0].Status != "running" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.Tasks[0].DurationSeconds == nil || *snapshot.Tasks[0].DurationSeconds != 75 {
		t.Fatalf("duration = %#v", snapshot.Tasks[0].DurationSeconds)
	}
	if strings.Contains(snapshot.Tasks[0].ID, "session-normal") {
		t.Fatalf("raw session id leaked: %q", snapshot.Tasks[0].ID)
	}
}

func TestTerminalTurnSuppressesOlderRunningUsage(t *testing.T) {
	now := time.Date(2026, 9, 18, 1, 30, 0, 0, time.UTC)
	root := t.TempDir()
	runtimeDB := filepath.Join(root, "db.sqlite")
	createRuntimeSessionDB(t, runtimeDB, []struct {
		status    string
		startedAt int64
	}{{status: "running", startedAt: now.Add(-90 * time.Second).UnixMilli()}})
	logDir := filepath.Join(root, "log")
	writeTurnLog(t, logDir,
		turnLine("turn.started", "session-normal", now.Add(-100*time.Second)),
		turnLine("turn.failed", "session-normal", now.Add(-5*time.Second)),
	)

	collector := New(runtimeDB, "")
	collector.LogDir = logDir
	collector.Now = func() time.Time { return now }
	_, err := collector.Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "task sources are unavailable") {
		t.Fatalf("terminal turn should suppress old running usage, got %v", err)
	}
}
