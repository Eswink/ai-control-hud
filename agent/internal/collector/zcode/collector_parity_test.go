package zcode

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestResumedGoalActiveTargetBeatsPendingTodo(t *testing.T) {
	now := time.Date(2026, 9, 17, 17, 13, 12, 0, time.UTC)
	runtimeDB := filepath.Join(t.TempDir(), "db.sqlite")
	createRuntimeDB(t, runtimeDB, now, now.Add(-3*time.Second))

	db, err := sql.Open("sqlite", runtimeDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE todo SET status='completed'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE todo SET status='pending' WHERE position=3`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	collector := New(runtimeDB, "")
	collector.Now = func() time.Time { return now }
	snapshot, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Summary.Running != 1 || snapshot.Summary.Waiting != 0 {
		t.Fatalf("summary = %+v", snapshot.Summary)
	}
	if len(snapshot.Tasks) != 1 || snapshot.Tasks[0].Status != "running" {
		t.Fatalf("tasks = %+v", snapshot.Tasks)
	}
	if snapshot.Tasks[0].Activity == nil || *snapshot.Tasks[0].Activity != "write PLAN-059" {
		t.Fatalf("activity = %#v", snapshot.Tasks[0].Activity)
	}
}

func TestLiveGoalMergesRecentTaskIndexHistoryWithoutDuplicate(t *testing.T) {
	now := time.Date(2026, 9, 17, 17, 13, 12, 0, time.UTC)
	root := t.TempDir()
	runtimeDB := filepath.Join(root, "db.sqlite")
	taskDB := filepath.Join(root, "tasks-index.sqlite")
	createRuntimeDB(t, runtimeDB, now, now.Add(-3*time.Second))
	createTaskIndex(t, taskDB, []taskIndexSeed{
		{
			workspaceKey: "research-system", workspacePath: `D:\github\research-system`, taskID: "session-live",
			title: "stale task-index projection of active Goal", status: "waiting", updatedAtMS: now.Add(-time.Second).UnixMilli(), pinned: 1,
		},
		{
			workspaceKey: "research-system", workspacePath: `D:\github\research-system`, taskID: "completed-a",
			title: "扫描一下我们的项目", status: "completed", updatedAtMS: now.Add(-time.Minute).UnixMilli(),
		},
		{
			workspaceKey: "research-system", workspacePath: `D:\github\research-system`, taskID: "completed-b",
			title: "项目中 Fake 实现是否应重构", status: "completed", updatedAtMS: now.Add(-35*time.Minute).UnixMilli(),
		},
	})

	collector := New(runtimeDB, taskDB)
	collector.Now = func() time.Time { return now }
	snapshot, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Summary.Running != 1 || snapshot.Summary.Waiting != 0 || snapshot.Summary.Completed != 2 {
		t.Fatalf("summary = %+v", snapshot.Summary)
	}
	if len(snapshot.Tasks) != 3 {
		t.Fatalf("tasks = %+v", snapshot.Tasks)
	}
	if snapshot.Tasks[0].Status != "running" {
		t.Fatalf("live Goal did not stay first: %+v", snapshot.Tasks)
	}
	for _, task := range snapshot.Tasks {
		if task.Title == "stale task-index projection of active Goal" {
			t.Fatalf("active session was duplicated from task index: %+v", snapshot.Tasks)
		}
	}
}

func TestLiveGoalMergesConcurrentOrdinaryTurnWithoutDuplicates(t *testing.T) {
	now := time.Date(2026, 9, 17, 18, 49, 8, 0, time.UTC)
	root := t.TempDir()
	runtimeDB := filepath.Join(root, "db.sqlite")
	taskDB := filepath.Join(root, "tasks-index.sqlite")
	logDir := filepath.Join(root, "log")
	createRuntimeDB(t, runtimeDB, now, now.Add(-2*time.Second))

	db, err := sql.Open("sqlite", runtimeDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE model_usage (session_id TEXT NOT NULL, status TEXT NOT NULL, started_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO session VALUES (?, ?, ?, ?, ?, ?)`,
		"session-ordinary", `D:\github\animation`, `D:\github\animation`,
		"生成一个鹅骑白天骑自行车的网页动画", nil, nil,
	); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	writeTurnLog(t, logDir,
		turnLine("turn.started", "session-live", now.Add(-90*time.Second)),
		turnLine("turn.started", "session-ordinary", now.Add(-61*time.Second)),
		turnLine("model.streaming", "session-ordinary", now.Add(-time.Second)),
	)
	createTaskIndex(t, taskDB, []taskIndexSeed{
		{
			workspaceKey: "research-system", workspacePath: `D:\github\research-system`, taskID: "session-live",
			title: "stale Goal projection", status: "waiting", updatedAtMS: now.Add(-time.Second).UnixMilli(), pinned: 1,
		},
		{
			workspaceKey: "animation", workspacePath: `D:\github\animation`, taskID: "session-ordinary",
			title: "stale ordinary projection", status: "waiting", updatedAtMS: now.Add(-time.Second).UnixMilli(), pinned: 1,
		},
		{
			workspaceKey: "research-system", workspacePath: `D:\github\research-system`, taskID: "completed-recent",
			title: "recent completed task", status: "completed", updatedAtMS: now.Add(-5*time.Minute).UnixMilli(),
		},
	})

	collector := New(runtimeDB, taskDB)
	collector.LogDir = logDir
	collector.Now = func() time.Time { return now }
	snapshot, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Summary.Running != 2 || snapshot.Summary.Waiting != 0 || snapshot.Summary.Completed != 1 {
		t.Fatalf("summary = %+v", snapshot.Summary)
	}
	if len(snapshot.Tasks) != 3 {
		t.Fatalf("tasks = %+v", snapshot.Tasks)
	}
	if snapshot.Tasks[0].Status != "running" || snapshot.Tasks[1].Status != "running" {
		t.Fatalf("expected Goal and ordinary task to be live: %+v", snapshot.Tasks)
	}
	if snapshot.Tasks[1].Title != "生成一个鹅骑白天骑自行车的网页动画" {
		t.Fatalf("ordinary title = %q", snapshot.Tasks[1].Title)
	}
	if snapshot.Tasks[1].DurationSeconds == nil || *snapshot.Tasks[1].DurationSeconds != 61 {
		t.Fatalf("ordinary duration = %#v", snapshot.Tasks[1].DurationSeconds)
	}
	for _, task := range snapshot.Tasks {
		if task.Title == "stale Goal projection" || task.Title == "stale ordinary projection" {
			t.Fatalf("live session duplicated from task index: %+v", snapshot.Tasks)
		}
	}
}


func TestBackgroundWorkflowKeepsTerminalGoalSessionRunning(t *testing.T) {
	now := time.Date(2026, 9, 20, 10, 30, 0, 0, time.UTC)
	root := t.TempDir()
	runtimeDB := filepath.Join(root, "db.sqlite")
	logDir := filepath.Join(root, "log")
	createRuntimeDB(t, runtimeDB, now, now.Add(-2*time.Second))

	db, err := sql.Open("sqlite", runtimeDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE session_target SET status='completed'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE todo SET status='completed'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	writeTurnLog(t, logDir,
		typedTurnLine("session.updated", "session-live", now.Add(-20*time.Second), `{"taskId":"background-private","status":"running"}`),
	)

	collector := New(runtimeDB, "")
	collector.LogDir = logDir
	collector.Now = func() time.Time { return now }
	snapshot, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Summary.Running != 1 || snapshot.Summary.Completed != 0 {
		t.Fatalf("background workflow did not override terminal Goal projection: %+v", snapshot.Summary)
	}
	if len(snapshot.Tasks) != 1 || snapshot.Tasks[0].Status != "running" {
		t.Fatalf("unexpected tasks: %+v", snapshot.Tasks)
	}
	if snapshot.Tasks[0].Title != "Goal 模式迭代与 collector-quality 持续失败取证" {
		t.Fatalf("Goal metadata enrichment was lost: %+v", snapshot.Tasks[0])
	}
	if snapshot.Tasks[0].DurationSeconds == nil || *snapshot.Tasks[0].DurationSeconds != 20 {
		t.Fatalf("background lifecycle duration = %#v", snapshot.Tasks[0].DurationSeconds)
	}
}
