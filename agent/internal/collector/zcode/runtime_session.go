package zcode

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
)

var runtimeSessionColumns = setOf("id", "directory", "path", "title")
var runtimeUsageColumns = setOf("session_id", "status", "started_at")

type runtimeSessionMeta struct {
	Directory sql.NullString
	Path      sql.NullString
	Title     sql.NullString
}

// collectRuntimeSessions closes the gap between live Goal mode and the
// historical task index. The ZCode turn log is the strongest signal for an
// ordinary session: an unmatched fresh turn.started stays running through
// model/tool phases even when the newest individual model_usage row is already
// terminal. model_usage remains a conservative fallback when no usable turn
// lifecycle signal exists.
func (c *Collector) collectRuntimeSessions(ctx context.Context) (*Snapshot, bool, error) {
	return c.collectRuntimeSessionsExcluding(ctx, nil)
}

// collectRuntimeSessionsExcluding keeps runtime liveness session-scoped. Goal
// rows passed in exclude remain authoritative for those exact sessions, while
// independent ordinary turns continue to surface concurrently.
func (c *Collector) collectRuntimeSessionsExcluding(ctx context.Context, exclude map[string]struct{}) (*Snapshot, bool, error) {
	db, err := openReadOnly(c.RuntimeDB)
	if err != nil {
		return nil, false, errors.New("ZCode runtime session database read failed")
	}
	defer db.Close()

	if verifyTableColumns(ctx, db, "session", runtimeSessionColumns) != nil ||
		verifyTableColumns(ctx, db, "model_usage", runtimeUsageColumns) != nil {
		return nil, false, nil
	}

	limit := bounded(c.TaskLimit, 20, 100)
	now := c.Now().UTC()
	turnStates, _ := c.readTurnStates() // Logs are supplemental; DB fallback stays available.
	tasks := make([]domain.TaskSummary, 0, limit)
	sessionIDs := make(map[string]struct{}, limit)
	seen := make(map[string]struct{}, limit*2)

	// Materialize fresh open turns first. This covers the common ZCode shape in
	// which a request row has completed while tools/thinking continue inside the
	// same still-open turn.
	type activeTurn struct {
		SessionID string
		State     turnLogState
	}
	activeTurns := make([]activeTurn, 0, len(turnStates))
	for sessionID, state := range turnStates {
		if _, skip := exclude[sessionID]; skip {
			continue
		}
		if state.active(now, c.TurnFreshSeconds) {
			activeTurns = append(activeTurns, activeTurn{SessionID: sessionID, State: state})
		}
	}
	sort.Slice(activeTurns, func(i, j int) bool {
		return activeTurns[i].State.UpdatedAt.After(activeTurns[j].State.UpdatedAt)
	})
	for _, active := range activeTurns {
		meta, ok, err := loadRuntimeSessionMeta(ctx, db, active.SessionID)
		if err != nil {
			return nil, true, errors.New("ZCode runtime session database read failed")
		}
		if !ok {
			continue
		}
		startedAt := active.State.StartedAt
		if startedAt.IsZero() {
			startedAt = active.State.UpdatedAt
		}
		updatedAt := active.State.UpdatedAt
		duration := int(ageSeconds(now, startedAt))
		tasks = append(tasks, runtimeSessionTask(active.SessionID, meta, domain.TaskRunning, &startedAt, &updatedAt, &duration))
		seen[active.SessionID] = struct{}{}
		sessionIDs[active.SessionID] = struct{}{}
		if len(tasks) >= limit {
			return &Snapshot{
				Summary:          summarizeTasks(tasks),
				Tasks:            tasks,
				sessionIDs:       sessionIDs,
				activeSessionIDs: sessionIDs,
			}, true, nil
		}
	}

	rows, err := db.QueryContext(ctx, `
		SELECT mu.session_id, mu.status, mu.started_at,
		       s.directory, s.path, s.title
		FROM model_usage AS mu
		JOIN session AS s ON s.id = mu.session_id
		WHERE mu.started_at IS NOT NULL
		ORDER BY mu.started_at DESC
		LIMIT ?`, limit*8)
	if err != nil {
		return nil, true, errors.New("ZCode runtime session database read failed")
	}
	defer rows.Close()

	freshSeconds := bounded(c.HeartbeatSeconds, 120, 3600)
	if freshSeconds < 600 {
		freshSeconds = 600
	}
	for rows.Next() {
		var sessionID string
		var status sql.NullString
		var startedRaw sql.NullInt64
		var meta runtimeSessionMeta
		if err := rows.Scan(&sessionID, &status, &startedRaw, &meta.Directory, &meta.Path, &meta.Title); err != nil {
			return nil, true, errors.New("ZCode runtime session database read failed")
		}
		if _, skip := exclude[sessionID]; skip {
			continue
		}
		if _, ok := seen[sessionID]; ok {
			continue
		}
		// The first model_usage row encountered for a session is its newest. Mark
		// it seen before filtering so an older running row cannot resurrect it.
		seen[sessionID] = struct{}{}

		startedAt := timestamp(startedRaw)
		if state, ok := turnStates[sessionID]; ok && !state.UpdatedAt.IsZero() {
			// A terminal turn event closes any older model request. Likewise, a
			// stale open turn must not be resurrected by an older request row. A
			// genuinely newer request is still allowed to use the DB fallback.
			if startedAt == nil || !startedAt.After(state.UpdatedAt) {
				continue
			}
		}
		if startedAt == nil || ageSeconds(now, *startedAt) > float64(freshSeconds) {
			continue
		}
		normalized := normalizeStatus(status.String)
		if normalized != domain.TaskRunning && normalized != domain.TaskWaiting {
			continue
		}

		duration := int(ageSeconds(now, *startedAt))
		tasks = append(tasks, runtimeSessionTask(sessionID, meta, normalized, startedAt, startedAt, &duration))
		sessionIDs[sessionID] = struct{}{}
		if len(tasks) >= limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, true, errors.New("ZCode runtime session database read failed")
	}
	if len(tasks) == 0 {
		return nil, true, nil
	}
	return &Snapshot{
				Summary:          summarizeTasks(tasks),
				Tasks:            tasks,
				sessionIDs:       sessionIDs,
				activeSessionIDs: sessionIDs,
			}, true, nil
}

func loadRuntimeSessionMeta(ctx context.Context, db *sql.DB, sessionID string) (runtimeSessionMeta, bool, error) {
	var meta runtimeSessionMeta
	err := db.QueryRowContext(ctx,
		`SELECT directory, path, title FROM session WHERE id = ?`, sessionID,
	).Scan(&meta.Directory, &meta.Path, &meta.Title)
	if errors.Is(err, sql.ErrNoRows) {
		return runtimeSessionMeta{}, false, nil
	}
	if err != nil {
		return runtimeSessionMeta{}, false, err
	}
	return meta, true, nil
}

func runtimeSessionTask(
	sessionID string,
	meta runtimeSessionMeta,
	status domain.TaskStatus,
	startedAt *time.Time,
	updatedAt *time.Time,
	duration *int,
) domain.TaskSummary {
	taskTitle := firstNonEmpty(500, meta.Title.String)
	if taskTitle == "" {
		taskTitle = "ZCode session"
	}
	workspace := workspaceLabel(meta.Path.String, meta.Directory.String)
	return domain.TaskSummary{
		ID:              taskIDHash("runtime-session", sessionID),
		Title:           truncate(strings.TrimSpace(taskTitle), 500),
		Workspace:       optionalString(workspace),
		Status:          status,
		StartedAt:       startedAt,
		UpdatedAt:       updatedAt,
		DurationSeconds: duration,
	}
}
