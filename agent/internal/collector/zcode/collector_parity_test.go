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
