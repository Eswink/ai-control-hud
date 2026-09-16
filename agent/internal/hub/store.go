package hub

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
	"github.com/Eswink/ai-control-hud/agent/internal/events"
	_ "modernc.org/sqlite"
)

type Store struct {
	mu sync.Mutex
	db *sql.DB
}

func OpenStore(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("hub database path is required")
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, fmt.Errorf("create hub database directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open hub database: %w", err)
	}
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("configure hub database: %w", err)
		}
	}
	if path != ":memory:" {
		if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("configure hub WAL: %w", err)
		}
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS agents (
			agent_id TEXT PRIMARY KEY,
			agent_version TEXT,
			last_seen_at TEXT NOT NULL,
			last_sent_at TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS snapshots (
			agent_id TEXT PRIMARY KEY,
			received_at TEXT NOT NULL,
			sent_at TEXT NOT NULL,
			state_json TEXT NOT NULL,
			FOREIGN KEY(agent_id) REFERENCES agents(agent_id) ON DELETE CASCADE
		);

		CREATE TABLE IF NOT EXISTS events (
			seq INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id TEXT NOT NULL UNIQUE,
			agent_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			occurred_at TEXT NOT NULL,
			received_at TEXT NOT NULL,
			event_json TEXT NOT NULL,
			FOREIGN KEY(agent_id) REFERENCES agents(agent_id) ON DELETE CASCADE
		);

		CREATE INDEX IF NOT EXISTS idx_events_agent_seq ON events(agent_id, seq);
	`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize hub database: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}

func (s *Store) RecordState(ctx context.Context, agentID string, sentAt time.Time, state domain.HudState, receivedAt time.Time) error {
	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode hub state: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin state transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := upsertAgent(ctx, tx, agentID, sentAt, receivedAt, nil); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO snapshots(agent_id, received_at, sent_at, state_json)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			received_at = excluded.received_at,
			sent_at = excluded.sent_at,
			state_json = excluded.state_json
	`, agentID, formatTime(receivedAt), formatTime(sentAt), string(payload)); err != nil {
		return fmt.Errorf("persist hub snapshot: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit state transaction: %w", err)
	}
	return nil
}

func (s *Store) RecordHeartbeat(ctx context.Context, agentID string, sentAt, receivedAt time.Time, agentVersion *string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := upsertAgent(ctx, s.db, agentID, sentAt, receivedAt, agentVersion); err != nil {
		return err
	}
	return nil
}

func (s *Store) RecordEvents(ctx context.Context, agentID string, sentAt time.Time, batch []events.Event, receivedAt time.Time) (int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("begin event transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := upsertAgent(ctx, tx, agentID, sentAt, receivedAt, nil); err != nil {
		return 0, 0, err
	}
	accepted := 0
	duplicates := 0
	for _, event := range batch {
		payload, err := json.Marshal(event)
		if err != nil {
			return 0, 0, fmt.Errorf("encode hub event: %w", err)
		}
		result, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO events(
				event_id, agent_id, event_type, occurred_at, received_at, event_json
			) VALUES (?, ?, ?, ?, ?, ?)
		`, event.EventID, agentID, event.Type, formatTime(event.OccurredAt), formatTime(receivedAt), string(payload))
		if err != nil {
			return 0, 0, fmt.Errorf("persist hub event: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return 0, 0, fmt.Errorf("read hub event insert result: %w", err)
		}
		if rows == 1 {
			accepted++
		} else {
			duplicates++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit event transaction: %w", err)
	}
	return accepted, duplicates, nil
}

func (s *Store) LoadState(ctx context.Context, agentID string) (domain.HudState, time.Time, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var payload string
	var lastSeenRaw string
	err := s.db.QueryRowContext(ctx, `
		SELECT snapshots.state_json, agents.last_seen_at
		FROM snapshots
		JOIN agents ON agents.agent_id = snapshots.agent_id
		WHERE snapshots.agent_id = ?
	`, agentID).Scan(&payload, &lastSeenRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.HudState{}, time.Time{}, false, nil
	}
	if err != nil {
		return domain.HudState{}, time.Time{}, false, fmt.Errorf("load hub snapshot: %w", err)
	}
	var state domain.HudState
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return domain.HudState{}, time.Time{}, false, fmt.Errorf("decode hub snapshot: %w", err)
	}
	if err := state.Validate(); err != nil {
		return domain.HudState{}, time.Time{}, false, fmt.Errorf("stored hub snapshot is invalid: %w", err)
	}
	lastSeen, err := time.Parse(time.RFC3339Nano, lastSeenRaw)
	if err != nil {
		return domain.HudState{}, time.Time{}, false, fmt.Errorf("decode hub last-seen time: %w", err)
	}
	return state, lastSeen, true, nil
}

func (s *Store) ListEvents(ctx context.Context, agentID string, after int64, limit int) ([]HubEvent, int64, error) {
	if after < 0 {
		return nil, 0, errors.New("event cursor must be non-negative")
	}
	if limit < 1 || limit > 100 {
		return nil, 0, errors.New("event page limit must be within 1..100")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx, `
		SELECT seq, agent_id, received_at, event_json
		FROM events
		WHERE agent_id = ? AND seq > ?
		ORDER BY seq ASC
		LIMIT ?
	`, agentID, after, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("query hub events: %w", err)
	}
	defer rows.Close()
	result := make([]HubEvent, 0, limit)
	for rows.Next() {
		var seq int64
		var rowAgentID, receivedRaw, payload string
		if err := rows.Scan(&seq, &rowAgentID, &receivedRaw, &payload); err != nil {
			return nil, 0, fmt.Errorf("scan hub event: %w", err)
		}
		var event events.Event
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return nil, 0, fmt.Errorf("decode hub event: %w", err)
		}
		if err := validateEvent(event); err != nil {
			return nil, 0, fmt.Errorf("stored hub event is invalid: %w", err)
		}
		receivedAt, err := time.Parse(time.RFC3339Nano, receivedRaw)
		if err != nil {
			return nil, 0, fmt.Errorf("decode hub event received time: %w", err)
		}
		result = append(result, HubEvent{Seq: seq, AgentID: rowAgentID, ReceivedAt: receivedAt, Event: event})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate hub events: %w", err)
	}
	var latest int64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM events WHERE agent_id = ?`, agentID).Scan(&latest); err != nil {
		return nil, 0, fmt.Errorf("query hub event high-water mark: %w", err)
	}
	return result, latest, nil
}

type sqlExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func upsertAgent(ctx context.Context, exec sqlExecutor, agentID string, sentAt, receivedAt time.Time, agentVersion *string) error {
	var version any
	if agentVersion != nil {
		version = *agentVersion
	}
	if _, err := exec.ExecContext(ctx, `
		INSERT INTO agents(agent_id, agent_version, last_seen_at, last_sent_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			agent_version = COALESCE(excluded.agent_version, agents.agent_version),
			last_seen_at = excluded.last_seen_at,
			last_sent_at = excluded.last_sent_at
	`, agentID, version, formatTime(receivedAt), formatTime(sentAt)); err != nil {
		return fmt.Errorf("persist hub agent heartbeat: %w", err)
	}
	return nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
