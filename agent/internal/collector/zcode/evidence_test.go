package zcode

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompatibilityEvidenceIsBoundedAndDoesNotExposePrivatePayloads(t *testing.T) {
	now := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	root := t.TempDir()
	runtimeDB := filepath.Join(root, "db.sqlite")
	taskDB := filepath.Join(root, "tasks-index.sqlite")
	logDir := filepath.Join(root, "log")

	createRuntimeDB(t, runtimeDB, now, now.Add(-5*time.Second))
	db, err := sql.Open("sqlite", runtimeDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE model_usage (
		session_id TEXT NOT NULL,
		status TEXT NOT NULL,
		started_at INTEGER
	)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	createTaskIndex(t, taskDB, []taskIndexSeed{{
		workspaceKey: "private-workspace",
		workspacePath: `D:\Users\private\repo`,
		taskID: "secret-session",
		title: "private title",
		status: "running",
		updatedAtMS: now.UnixMilli(),
	}})

	writeTurnLog(t, logDir,
		`{"event":"turn.started","sessionId":"secret-session","timestamp":"2026-09-20T08:59:00Z","context":{"inputSource":"background_task","prompt":"PRIVATE PROMPT"}}`,
		`{"event":"session.updated","sessionId":"secret-session","timestamp":"2026-09-20T08:59:01Z","payload":{"taskId":"private-task-id","status":"running","message":"PRIVATE MESSAGE"}}`,
		`{"event":"model.streaming","sessionId":"secret-session","timestamp":"2026-09-20T08:59:02Z"}`,
		`{"event":"C:\\Users\\private\\not-an-event","sessionId":"secret-session","timestamp":"2026-09-20T08:59:03Z"}`,
		`not json`,
	)

	collector := New(runtimeDB, taskDB)
	collector.LogDir = logDir
	evidence := collector.CollectCompatibilityEvidence(context.Background())
	if evidence.EvidenceVersion != 1 {
		t.Fatalf("version=%d", evidence.EvidenceVersion)
	}
	if !evidence.RuntimeDatabasePresent || !evidence.RuntimeDatabaseReadable ||
		!evidence.GoalSchemaCompatible || !evidence.RuntimeSessionCompatible {
		t.Fatalf("runtime evidence=%+v", evidence)
	}
	if !evidence.TaskIndexPresent || !evidence.TaskIndexReadable || !evidence.TaskIndexSchemaCompatible {
		t.Fatalf("task-index evidence=%+v", evidence)
	}
	if !evidence.TurnLogDirectoryReadable || evidence.TurnLogFilesSampled != 1 ||
		evidence.TurnLogLinesSampled != 5 || evidence.TurnLogRecordsParsed != 4 {
		t.Fatalf("turn-log evidence=%+v", evidence)
	}
	if evidence.EventCounts["turn.started"] != 1 ||
		evidence.EventCounts["session.updated"] != 1 ||
		evidence.EventCounts["model.streaming"] != 1 {
		t.Fatalf("event counts=%v", evidence.EventCounts)
	}
	if evidence.OtherEventRecords != 1 || evidence.SessionUpdatedTaskSignals != 1 ||
		evidence.BackgroundTaskTurnStarts != 1 {
		t.Fatalf("workflow signals=%+v", evidence)
	}

	encoded, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, private := range []string{
		"secret-session",
		"private-task-id",
		"PRIVATE PROMPT",
		"PRIVATE MESSAGE",
		"Users",
		"private title",
		"private-workspace",
	} {
		if strings.Contains(text, private) {
			t.Fatalf("evidence leaked %q: %s", private, text)
		}
	}
}

func TestCompatibilityEvidenceHandlesMissingSources(t *testing.T) {
	collector := New(filepath.Join(t.TempDir(), "missing.sqlite"), filepath.Join(t.TempDir(), "missing-tasks.sqlite"))
	collector.LogDir = filepath.Join(t.TempDir(), "missing-log")
	evidence := collector.CollectCompatibilityEvidence(context.Background())
	if evidence.RuntimeDatabasePresent || evidence.RuntimeDatabaseReadable ||
		evidence.TaskIndexPresent || evidence.TaskIndexReadable ||
		evidence.TurnLogDirectoryReadable {
		t.Fatalf("missing sources reported present: %+v", evidence)
	}
	if len(evidence.EventCounts) != 0 {
		t.Fatalf("unexpected event counts: %v", evidence.EventCounts)
	}
}

func TestEvidenceEventNameAcceptsTypedNestedEnvelope(t *testing.T) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(`{"type":"session.updated","payload":{"taskId":"x","status":"running","inputSource":"background_task"}}`), &root); err != nil {
		t.Fatal(err)
	}
	if got := evidenceEventName(root); got != "session.updated" {
		t.Fatalf("event=%q", got)
	}
	if !hasTaskStatusSignal(root) {
		t.Fatal("nested task status signal missing")
	}
	if got := nestedString(root, "inputSource"); got != "background_task" {
		t.Fatalf("inputSource=%q", got)
	}
}

func TestCompatibilityEvidenceDoesNotWriteSourceFiles(t *testing.T) {
	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "zcode-2026-09-20.jsonl")
	if err := os.WriteFile(logPath, []byte("{\"event\":\"turn.started\",\"timestamp\":\"2026-09-20T09:00:00Z\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	collector := New("", "")
	collector.LogDir = logDir
	_ = collector.CollectCompatibilityEvidence(context.Background())
	after, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("evidence probe modified log file")
	}
}
