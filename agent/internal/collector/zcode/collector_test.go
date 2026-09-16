package zcode

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func createRuntimeDB(t *testing.T, path string, now time.Time, heartbeat time.Time) {
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
			title TEXT NOT NULL,
			summary_additions INTEGER,
			summary_deletions INTEGER
		)`,
		`CREATE TABLE session_target (
			session_id TEXT PRIMARY KEY,
			target_id TEXT NOT NULL,
			objective TEXT NOT NULL,
			status TEXT NOT NULL,
			token_budget INTEGER,
			tokens_used INTEGER NOT NULL,
			time_used_seconds INTEGER NOT NULL,
			time_created INTEGER NOT NULL,
			time_updated INTEGER NOT NULL,
			summary_title TEXT,
			active_input_id TEXT,
			active_run_started_at INTEGER,
			active_run_last_seen_at INTEGER
		)`,
		`CREATE TABLE todo (
			session_id TEXT NOT NULL,
			content TEXT NOT NULL,
			status TEXT NOT NULL,
			priority TEXT NOT NULL,
			position INTEGER NOT NULL,
			time_created INTEGER NOT NULL,
			time_updated INTEGER NOT NULL,
			PRIMARY KEY (session_id, position)
		)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}

	nowMS := now.UnixMilli()
	if _, err := db.Exec(`INSERT INTO session VALUES (?, ?, ?, ?, ?, ?)`,
		"session-live", `D:\github\research-system`, `D:\github\research-system`,
		"Goal 模式迭代与 collector-quality 持续失败取证", 42, 7,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO session_target VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"session-live", "target-1", "持续迭代 goal 文件完善系统", "active",
		nil, 123, 33780, nowMS-40_000, nowMS, "Goal summary", "input-1",
		nowMS-30_000, heartbeat.UnixMilli(),
	); err != nil {
		t.Fatal(err)
	}
	todos := []struct {
		content string
		status  string
		pos     int
	}{
		{"completed A", "completed", 0},
		{"completed B", "completed", 1},
		{"GOAL-002 cycle 5: local gates", "running", 2},
		{"write PLAN-059", "pending", 3},
	}
	for _, todo := range todos {
		if _, err := db.Exec(`INSERT INTO todo VALUES (?, ?, ?, ?, ?, ?, ?)`,
			"session-live", todo.content, todo.status, "normal", todo.pos, nowMS, nowMS,
		); err != nil {
			t.Fatal(err)
		}
	}
}

func createTaskIndex(t *testing.T, path string, rows []taskIndexSeed) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE tasks (
		workspace_key TEXT NOT NULL,
		workspace_path TEXT NOT NULL,
		workspace_identity TEXT,
		task_id TEXT NOT NULL,
		title TEXT NOT NULL,
		task_status TEXT,
		provider TEXT,
		mode TEXT NOT NULL DEFAULT '',
		model TEXT,
		migration_source TEXT,
		forked_from_task_id TEXT,
		created_at INTEGER NOT NULL DEFAULT 0,
		updated_at INTEGER NOT NULL,
		unread_at INTEGER,
		last_unread_at INTEGER NOT NULL DEFAULT 0,
		pinned INTEGER NOT NULL,
		archived INTEGER NOT NULL,
		deleted INTEGER NOT NULL,
		title_overridden INTEGER NOT NULL DEFAULT 0,
		meta_json TEXT NOT NULL DEFAULT '{}',
		searchable_text TEXT NOT NULL DEFAULT '',
		cron_automation_id TEXT,
		off_peak_task_id TEXT,
		PRIMARY KEY (workspace_key, task_id)
	)`); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if _, err := db.Exec(`INSERT INTO tasks (
			workspace_key, workspace_path, task_id, title, task_status,
			updated_at, pinned, archived, deleted
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.workspaceKey, row.workspacePath, row.taskID, row.title, row.status,
			row.updatedAtMS, row.pinned, row.archived, row.deleted,
		); err != nil {
			t.Fatal(err)
		}
	}
}

type taskIndexSeed struct {
	workspaceKey  string
	workspacePath string
	taskID        string
	title         string
	status        string
	updatedAtMS   int64
	pinned        int
	archived      int
	deleted       int
}

func TestLiveGoalWinsAndMapsCurrentActivity(t *testing.T) {
	now := time.Date(2026, 9, 16, 4, 30, 0, 0, time.UTC)
	runtimeDB := filepath.Join(t.TempDir(), "db.sqlite")
	taskDB := filepath.Join(t.TempDir(), "tasks-index.sqlite")
	createRuntimeDB(t, runtimeDB, now, now.Add(-10*time.Second))
	createTaskIndex(t, taskDB, []taskIndexSeed{{
		workspaceKey: "old", workspacePath: `D:\github\research-system`, taskID: "old-task",
		title: "old failed task", status: "failed", updatedAtMS: now.Add(-20 * 24 * time.Hour).UnixMilli(),
	}})

	collector := New(runtimeDB, taskDB)
	collector.Now = func() time.Time { return now }
	snapshot, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Summary.Running != 1 || snapshot.Summary.Failed != 0 {
		t.Fatalf("summary = %+v", snapshot.Summary)
	}
	if len(snapshot.Tasks) != 1 {
		t.Fatalf("tasks = %+v", snapshot.Tasks)
	}
	task := snapshot.Tasks[0]
	if task.Title != "Goal 模式迭代与 collector-quality 持续失败取证" {
		t.Fatalf("title = %q", task.Title)
	}
	if task.Workspace == nil || *task.Workspace != "research-system" {
		t.Fatalf("workspace = %#v", task.Workspace)
	}
	if task.Activity == nil || *task.Activity != "GOAL-002 cycle 5: local gates" {
		t.Fatalf("activity = %#v", task.Activity)
	}
	if task.DurationSeconds == nil || *task.DurationSeconds != 33780 {
		t.Fatalf("duration = %#v", task.DurationSeconds)
	}
	if task.Changes == nil || task.Changes.Additions == nil || *task.Changes.Additions != 42 || task.Changes.Deletions == nil || *task.Changes.Deletions != 7 {
		t.Fatalf("changes = %#v", task.Changes)
	}
	if !strings.HasPrefix(task.ID, "zcode-goal-") || strings.Contains(task.ID, "session-live") {
		t.Fatalf("unsafe id = %q", task.ID)
	}
}

func TestStaleRunningGoalDoesNotResurrect(t *testing.T) {
	now := time.Date(2026, 9, 16, 4, 30, 0, 0, time.UTC)
	runtimeDB := filepath.Join(t.TempDir(), "db.sqlite")
	createRuntimeDB(t, runtimeDB, now, now.Add(-7*24*time.Hour))

	collector := New(runtimeDB, "")
	collector.Now = func() time.Time { return now }
	collector.HeartbeatSeconds = 120
	_, err := collector.Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "task sources are unavailable") {
		t.Fatalf("expected no live goal and no fallback, got %v", err)
	}
}

func TestRecentTerminalGoalIsVisibleButOldTerminalFallsBack(t *testing.T) {
	now := time.Date(2026, 9, 16, 4, 30, 0, 0, time.UTC)
	runtimeDB := filepath.Join(t.TempDir(), "db.sqlite")
	createRuntimeDB(t, runtimeDB, now, now.Add(-3*time.Hour))

	db, err := sql.Open("sqlite", runtimeDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE session_target SET status='failed', time_updated=?, active_run_last_seen_at=NULL`, now.Add(-30*time.Second).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE todo SET status='completed'`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	collector := New(runtimeDB, "")
	collector.Now = func() time.Time { return now }
	collector.HeartbeatSeconds = 60
	collector.RecentTerminalSeconds = 60
	snapshot, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Summary.Failed != 1 || len(snapshot.Tasks) != 1 || snapshot.Tasks[0].Status != "failed" {
		t.Fatalf("snapshot = %+v", snapshot)
	}

	db, err = sql.Open("sqlite", runtimeDB)
	if err != nil {
		t.Fatal(err)
	}
	old := now.Add(-3 * time.Hour).UnixMilli()
	if _, err := db.Exec(`UPDATE session_target SET time_updated=?`, old); err != nil {
		t.Fatal(err)
	}
	db.Close()

	_, err = collector.Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "task sources are unavailable") {
		t.Fatalf("old terminal should disappear, got %v", err)
	}
}

func TestTaskIndexFallbackIsRecentOnlyAndSummaryIsNotDisplayLimitBound(t *testing.T) {
	now := time.Date(2026, 9, 16, 4, 30, 0, 0, time.UTC)
	taskDB := filepath.Join(t.TempDir(), "tasks-index.sqlite")
	createTaskIndex(t, taskDB, []taskIndexSeed{
		{workspaceKey: "wk-a", workspacePath: `D:\github\alpha`, taskID: "task-1", title: "Running", status: "running", updatedAtMS: now.Add(-time.Hour).UnixMilli(), pinned: 1},
		{workspaceKey: "wk-b", workspacePath: `D:\github\beta`, taskID: "task-2", title: "Waiting", status: "queued", updatedAtMS: now.Add(-2*time.Hour).UnixMilli()},
		{workspaceKey: "wk-c", workspacePath: `D:\github\gamma`, taskID: "task-3", title: "Done", status: "completed", updatedAtMS: now.Add(-3*time.Hour).UnixMilli()},
		{workspaceKey: "wk-old", workspacePath: `D:\github\old`, taskID: "task-old", title: "Ancient failure", status: "failed", updatedAtMS: now.Add(-20*24*time.Hour).UnixMilli()},
		{workspaceKey: "wk-hidden", workspacePath: `D:\github\hidden`, taskID: "task-hidden", title: "Archived", status: "running", updatedAtMS: now.Add(-time.Hour).UnixMilli(), archived: 1},
	})

	collector := New("", taskDB)
	collector.Now = func() time.Time { return now }
	collector.TaskLimit = 2
	collector.TaskMaxAgeSeconds = 24 * 60 * 60
	snapshot, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tasks) != 2 {
		t.Fatalf("display tasks = %d", len(snapshot.Tasks))
	}
	if snapshot.Summary.Running != 1 || snapshot.Summary.Waiting != 1 || snapshot.Summary.Completed != 1 || snapshot.Summary.Failed != 0 {
		t.Fatalf("summary = %+v", snapshot.Summary)
	}
	for _, task := range snapshot.Tasks {
		if strings.Contains(task.ID, "wk-") || strings.Contains(task.ID, "task-") {
			t.Fatalf("raw id leaked: %q", task.ID)
		}
	}
}

func TestChangedTaskIndexSchemaFailsExplicitly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE tasks (task_id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	collector := New("", path)
	_, err = collector.Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Unsupported ZCode task index schema") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadOnlyCollectorDoesNotModifyDatabase(t *testing.T) {
	now := time.Date(2026, 9, 16, 4, 30, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "tasks-index.sqlite")
	createTaskIndex(t, path, []taskIndexSeed{{
		workspaceKey: "wk", workspacePath: `D:\github\repo`, taskID: "task", title: "Current",
		status: "running", updatedAtMS: now.UnixMilli(),
	}})
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	collector := New("", path)
	collector.Now = func() time.Time { return now }
	if _, err := collector.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() {
		t.Fatalf("database size changed: %d -> %d", before.Size(), after.Size())
	}
}
