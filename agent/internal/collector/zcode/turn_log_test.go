package zcode

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTurnLog(t *testing.T, dir string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "zcode-2026-09-18.jsonl")
	body := ""
	for _, line := range lines {
		body += line + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func turnLine(event, sessionID string, at time.Time) string {
	return fmt.Sprintf(`{"event":%q,"sessionId":%q,"timestamp":%q}`, event, sessionID, at.UTC().Format(time.RFC3339Nano))
}

func TestTurnLogTracksOpenAndTerminalLifecycle(t *testing.T) {
	now := time.Date(2026, 9, 18, 1, 0, 0, 0, time.UTC)
	logDir := t.TempDir()
	writeTurnLog(t, logDir,
		turnLine("turn.started", "session-complete", now.Add(-90*time.Second)),
		turnLine("model.streaming", "session-complete", now.Add(-80*time.Second)),
		turnLine("turn.completed", "session-complete", now.Add(-70*time.Second)),
		turnLine("turn.started", "session-open", now.Add(-60*time.Second)),
		turnLine("model.streaming", "session-open", now.Add(-2*time.Second)),
		turnLine("turn.started", "session-failed", now.Add(-50*time.Second)),
		turnLine("turn.failed", "session-failed", now.Add(-40*time.Second)),
		turnLine("turn.started", "session-cancelled", now.Add(-30*time.Second)),
		turnLine("turn.cancelled", "session-cancelled", now.Add(-20*time.Second)),
	)

	collector := New("", "")
	collector.LogDir = logDir
	collector.Now = func() time.Time { return now }
	states, err := collector.readTurnStates()
	if err != nil {
		t.Fatal(err)
	}
	if states["session-complete"].Open || states["session-failed"].Open || states["session-cancelled"].Open {
		t.Fatalf("terminal turns remained open: %+v", states)
	}
	open := states["session-open"]
	if !open.Open || !open.active(now, 1800) {
		t.Fatalf("open turn not active: %+v", open)
	}
	if !open.StartedAt.Equal(now.Add(-60*time.Second)) || !open.UpdatedAt.Equal(now.Add(-2*time.Second)) {
		t.Fatalf("unexpected timestamps: %+v", open)
	}
}

func TestTurnLogNewStartReopensSessionAfterTerminal(t *testing.T) {
	now := time.Date(2026, 9, 18, 1, 0, 0, 0, time.UTC)
	logDir := t.TempDir()
	writeTurnLog(t, logDir,
		turnLine("turn.started", "session-resumed", now.Add(-5*time.Minute)),
		turnLine("turn.cancelled", "session-resumed", now.Add(-4*time.Minute)),
		turnLine("turn.started", "session-resumed", now.Add(-30*time.Second)),
	)
	collector := New("", "")
	collector.LogDir = logDir
	states, err := collector.readTurnStates()
	if err != nil {
		t.Fatal(err)
	}
	state := states["session-resumed"]
	if !state.Open || !state.StartedAt.Equal(now.Add(-30*time.Second)) {
		t.Fatalf("resumed state = %+v", state)
	}
}

func TestDefaultLogDirFollowsRuntimeDatabase(t *testing.T) {
	root := t.TempDir()
	runtimeDB := filepath.Join(root, ".zcode", "cli", "db", "db.sqlite")
	expected := filepath.Join(root, ".zcode", "cli", "log")
	if actual := defaultLogDir(runtimeDB); actual != expected {
		t.Fatalf("log dir = %q, want %q", actual, expected)
	}
}


func typedTurnLine(event, sessionID string, at time.Time, payload string) string {
	if payload == "" {
		payload = "{}"
	}
	return fmt.Sprintf(`{"type":%q,"sessionId":%q,"timestamp":%q,"payload":%s}`,
		event, sessionID, at.UTC().Format(time.RFC3339Nano), payload)
}

func TestTurnLogTypedEnvelopeTracksBackgroundTaskAfterForegroundTurnCompletes(t *testing.T) {
	now := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	logDir := t.TempDir()
	writeTurnLog(t, logDir,
		typedTurnLine("turn.started", "session-background", now.Add(-4*time.Minute), `{"inputSource":"user"}`),
		typedTurnLine("session.updated", "session-background", now.Add(-3*time.Minute), `{"taskId":"agent_private","status":"running"}`),
		typedTurnLine("turn.completed", "session-background", now.Add(-2*time.Minute), `{}`),
	)

	collector := New("", "")
	collector.LogDir = logDir
	states, err := collector.readTurnStates()
	if err != nil {
		t.Fatal(err)
	}
	state := states["session-background"]
	if state.Open {
		t.Fatalf("foreground turn unexpectedly open: %+v", state)
	}
	if len(state.BackgroundTasks) != 1 {
		t.Fatalf("background task not retained: %+v", state)
	}
	if !state.active(now, 1800) {
		t.Fatalf("background task did not keep session active: %+v", state)
	}
	if !state.StartedAt.Equal(now.Add(-4 * time.Minute)) {
		t.Fatalf("active span start changed unexpectedly: %+v", state)
	}
}

func TestTurnLogBackgroundTaskTerminalStatusClosesSession(t *testing.T) {
	now := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	logDir := t.TempDir()
	writeTurnLog(t, logDir,
		typedTurnLine("session.updated", "session-background", now.Add(-90*time.Second), `{"taskId":"exec_private","status":"running"}`),
		typedTurnLine("session.updated", "session-background", now.Add(-30*time.Second), `{"taskId":"exec_private","status":"completed"}`),
	)

	collector := New("", "")
	collector.LogDir = logDir
	states, err := collector.readTurnStates()
	if err != nil {
		t.Fatal(err)
	}
	state := states["session-background"]
	if len(state.BackgroundTasks) != 0 || state.Open || state.active(now, 1800) {
		t.Fatalf("completed background task left session active: %+v", state)
	}
}

func TestTurnLogMultipleBackgroundTasksRemainActiveUntilAllTerminal(t *testing.T) {
	now := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	logDir := t.TempDir()
	writeTurnLog(t, logDir,
		typedTurnLine("session.updated", "session-multi", now.Add(-4*time.Minute), `{"taskId":"task_a","status":"running"}`),
		typedTurnLine("session.updated", "session-multi", now.Add(-3*time.Minute), `{"taskId":"task_b","status":"queued"}`),
		typedTurnLine("session.updated", "session-multi", now.Add(-2*time.Minute), `{"taskId":"task_a","status":"failed"}`),
	)

	collector := New("", "")
	collector.LogDir = logDir
	states, err := collector.readTurnStates()
	if err != nil {
		t.Fatal(err)
	}
	state := states["session-multi"]
	if len(state.BackgroundTasks) != 1 || !state.active(now, 1800) {
		t.Fatalf("remaining background task was lost: %+v", state)
	}

	writeTurnLog(t, logDir,
		typedTurnLine("session.updated", "session-multi", now.Add(-4*time.Minute), `{"taskId":"task_a","status":"running"}`),
		typedTurnLine("session.updated", "session-multi", now.Add(-3*time.Minute), `{"taskId":"task_b","status":"queued"}`),
		typedTurnLine("session.updated", "session-multi", now.Add(-2*time.Minute), `{"taskId":"task_a","status":"failed"}`),
		typedTurnLine("session.updated", "session-multi", now.Add(-time.Minute), `{"taskId":"task_b","status":"cancelled"}`),
	)
	states, err = collector.readTurnStates()
	if err != nil {
		t.Fatal(err)
	}
	state = states["session-multi"]
	if len(state.BackgroundTasks) != 0 || state.active(now, 1800) {
		t.Fatalf("terminal background tasks left session active: %+v", state)
	}
}

func TestTurnLogBackgroundNotificationTurnDoesNotCreateAnotherSession(t *testing.T) {
	now := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	logDir := t.TempDir()
	writeTurnLog(t, logDir,
		typedTurnLine("session.updated", "session-parent", now.Add(-4*time.Minute), `{"taskId":"agent_private","status":"running"}`),
		typedTurnLine("turn.started", "session-parent", now.Add(-2*time.Minute), `{"inputSource":"background_task"}`),
		typedTurnLine("session.updated", "session-parent", now.Add(-90*time.Second), `{"taskId":"agent_private","status":"completed"}`),
		typedTurnLine("turn.completed", "session-parent", now.Add(-time.Minute), `{}`),
	)

	collector := New("", "")
	collector.LogDir = logDir
	states, err := collector.readTurnStates()
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("background notification created extra sessions: %+v", states)
	}
	state := states["session-parent"]
	if state.Open || len(state.BackgroundTasks) != 0 || state.active(now, 1800) {
		t.Fatalf("notification lifecycle did not settle: %+v", state)
	}
}

func TestTurnLogReadsNestedSessionAndTimestampEnvelope(t *testing.T) {
	now := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	logDir := t.TempDir()
	line := fmt.Sprintf(`{"type":"turn.started","payload":{"sessionId":"session-nested","timestamp":%q}}`,
		now.Add(-10*time.Second).Format(time.RFC3339Nano))
	writeTurnLog(t, logDir, line)

	collector := New("", "")
	collector.LogDir = logDir
	states, err := collector.readTurnStates()
	if err != nil {
		t.Fatal(err)
	}
	state := states["session-nested"]
	if !state.Open || !state.active(now, 1800) {
		t.Fatalf("nested envelope was not parsed: %+v", state)
	}
}
