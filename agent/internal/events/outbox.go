package events

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
	_ "modernc.org/sqlite"
)

const initializedKey = "task-baseline-initialized"

type Outbox struct {
	mu sync.Mutex
	db *sql.DB
}

type observedTask struct {
	Status domain.TaskStatus
}

func Open(path string) (*Outbox, error) {
	if path == "" {
		return nil, fmt.Errorf("event outbox path is required")
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create event outbox directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open event outbox: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure event outbox foreign keys: %w", err)
	}
	if path != ":memory:" {
		if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("configure event outbox WAL: %w", err)
		}
	}
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure event outbox timeout: %w", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS event_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS task_state (
			task_id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			title TEXT NOT NULL,
			workspace TEXT,
			updated_at TEXT,
			last_seen_at TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS event_outbox (
			local_seq INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id TEXT NOT NULL UNIQUE,
			event_type TEXT NOT NULL,
			occurred_at TEXT NOT NULL,
			event_json TEXT NOT NULL,
			created_at TEXT NOT NULL
		);
	`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize event outbox schema: %w", err)
	}
	return &Outbox{db: db}, nil
}

func (o *Outbox) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.db.Close()
}

// Observe atomically advances the durable task baseline and appends terminal
// transitions to the outbox. The first trustworthy snapshot only establishes
// a baseline so agent startup never replays historical completions/failures.
func (o *Outbox) Observe(ctx context.Context, state domain.HudState, observedAt time.Time) error {
	if state.ZCode.Health.Status != domain.SourceOK || state.ZCode.Tasks == nil {
		return nil
	}
	observedAt = observedAt.UTC()

	o.mu.Lock()
	defer o.mu.Unlock()

	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin event observation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	initialized, err := baselineInitialized(ctx, tx)
	if err != nil {
		return err
	}
	previous, err := loadTaskState(ctx, tx)
	if err != nil {
		return err
	}

	for _, task := range state.ZCode.Tasks {
		prior, seen := previous[task.ID]
		if initialized && terminal(task.Status) && (!seen || prior.Status != task.Status) {
			event, err := eventForTask(task, observedAt)
			if err != nil {
				return err
			}
			payload, err := json.Marshal(event)
			if err != nil {
				return fmt.Errorf("encode task event: %w", err)
			}
			if _, err := tx.ExecContext(
				ctx,
				`INSERT INTO event_outbox(event_id, event_type, occurred_at, event_json, created_at)
				 VALUES (?, ?, ?, ?, ?)`,
				event.EventID,
				event.Type,
				event.OccurredAt.Format(time.RFC3339Nano),
				string(payload),
				observedAt.Format(time.RFC3339Nano),
			); err != nil {
				return fmt.Errorf("append task event: %w", err)
			}
		}

		var workspace any
		if task.Workspace != nil {
			workspace = *task.Workspace
		}
		var updatedAt any
		if task.UpdatedAt != nil {
			updatedAt = task.UpdatedAt.UTC().Format(time.RFC3339Nano)
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO task_state(task_id, status, title, workspace, updated_at, last_seen_at)
			 VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT(task_id) DO UPDATE SET
			   status = excluded.status,
			   title = excluded.title,
			   workspace = excluded.workspace,
			   updated_at = excluded.updated_at,
			   last_seen_at = excluded.last_seen_at`,
			task.ID,
			task.Status,
			task.Title,
			workspace,
			updatedAt,
			observedAt.Format(time.RFC3339Nano),
		); err != nil {
			return fmt.Errorf("persist task event baseline: %w", err)
		}
	}

	if !initialized {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO event_meta(key, value) VALUES (?, ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			initializedKey,
			"1",
		); err != nil {
			return fmt.Errorf("persist event baseline initialization: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit event observation: %w", err)
	}
	return nil
}

func (o *Outbox) Pending(ctx context.Context, limit int) ([]Event, error) {
	if limit <= 0 || limit > 100 {
		return nil, fmt.Errorf("event pending limit must be within 1..100")
	}
	o.mu.Lock()
	defer o.mu.Unlock()

	rows, err := o.db.QueryContext(
		ctx,
		`SELECT event_json FROM event_outbox ORDER BY local_seq ASC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query event outbox: %w", err)
	}
	defer rows.Close()

	result := make([]Event, 0, limit)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan event outbox: %w", err)
		}
		var event Event
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return nil, fmt.Errorf("decode event outbox row: %w", err)
		}
		if err := event.Validate(); err != nil {
			return nil, fmt.Errorf("invalid event outbox row: %w", err)
		}
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate event outbox: %w", err)
	}
	return result, nil
}

func (o *Outbox) Ack(ctx context.Context, eventIDs []string) error {
	if len(eventIDs) == 0 {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()

	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin event ack: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, eventID := range eventIDs {
		if _, err := tx.ExecContext(
			ctx,
			"DELETE FROM event_outbox WHERE event_id = ?",
			eventID,
		); err != nil {
			return fmt.Errorf("ack event %s: %w", eventID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit event ack: %w", err)
	}
	return nil
}

func (o *Outbox) Count(ctx context.Context) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	var count int
	if err := o.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM event_outbox").Scan(&count); err != nil {
		return 0, fmt.Errorf("count event outbox: %w", err)
	}
	return count, nil
}

func baselineInitialized(ctx context.Context, tx *sql.Tx) (bool, error) {
	var value string
	err := tx.QueryRowContext(
		ctx,
		"SELECT value FROM event_meta WHERE key = ?",
		initializedKey,
	).Scan(&value)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read event baseline state: %w", err)
	}
	return value == "1", nil
}

func loadTaskState(ctx context.Context, tx *sql.Tx) (map[string]observedTask, error) {
	rows, err := tx.QueryContext(ctx, "SELECT task_id, status FROM task_state")
	if err != nil {
		return nil, fmt.Errorf("read task event baseline: %w", err)
	}
	defer rows.Close()

	result := map[string]observedTask{}
	for rows.Next() {
		var id string
		var status string
		if err := rows.Scan(&id, &status); err != nil {
			return nil, fmt.Errorf("scan task event baseline: %w", err)
		}
		result[id] = observedTask{Status: domain.TaskStatus(status)}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate task event baseline: %w", err)
	}
	return result, nil
}

func eventForTask(task domain.TaskSummary, fallback time.Time) (Event, error) {
	eventType := Type("")
	switch task.Status {
	case domain.TaskCompleted:
		eventType = TaskCompleted
	case domain.TaskFailed:
		eventType = TaskFailed
	default:
		return Event{}, fmt.Errorf("task %s is not terminal", task.ID)
	}

	occurredAt := fallback.UTC()
	if task.UpdatedAt != nil && !task.UpdatedAt.IsZero() {
		occurredAt = task.UpdatedAt.UTC()
	}
	event := Event{
		EventID:    newEventID(),
		Type:       eventType,
		OccurredAt: occurredAt,
		Task: Task{
			ID:        task.ID,
			Title:     task.Title,
			Workspace: task.Workspace,
			Status:    task.Status,
		},
	}
	if err := event.Validate(); err != nil {
		return Event{}, err
	}
	return event, nil
}

func terminal(status domain.TaskStatus) bool {
	return status == domain.TaskCompleted || status == domain.TaskFailed
}

func newEventID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand event id failed: %v", err))
	}
	return hex.EncodeToString(raw[:])
}
