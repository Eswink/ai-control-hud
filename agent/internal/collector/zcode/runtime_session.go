package zcode

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
)

var runtimeSessionColumns = setOf("id", "directory", "path", "title")
var runtimeUsageColumns = setOf("session_id", "status", "started_at")

// collectRuntimeSessions closes the gap between live Goal mode and the historical
// task index. ZCode records model requests for ordinary sessions in model_usage.
// We only surface a session when its newest request explicitly carries a
// running/waiting-like status and is fresh enough to be trustworthy.
//
// This deliberately does not infer activity from source health, a recent
// completed request, or an older running row shadowed by a newer terminal row.
func (c *Collector) collectRuntimeSessions(ctx context.Context) (*Snapshot, bool, error) {
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

	now := c.Now().UTC()
	freshSeconds := bounded(c.HeartbeatSeconds, 120, 3600)
	if freshSeconds < 600 {
		freshSeconds = 600
	}

	seen := make(map[string]struct{}, limit*2)
	tasks := make([]domain.TaskSummary, 0, limit)
	for rows.Next() {
		var sessionID string
		var status sql.NullString
		var startedRaw sql.NullInt64
		var directory, path, title sql.NullString
		if err := rows.Scan(&sessionID, &status, &startedRaw, &directory, &path, &title); err != nil {
			return nil, true, errors.New("ZCode runtime session database read failed")
		}
		if _, ok := seen[sessionID]; ok {
			continue
		}
		// The first row encountered for a session is its newest request. Mark it
		// seen before filtering so an older stale "running" row cannot resurrect
		// after a newer completed/failed request.
		seen[sessionID] = struct{}{}

		startedAt := timestamp(startedRaw)
		if startedAt == nil || ageSeconds(now, *startedAt) > float64(freshSeconds) {
			continue
		}
		normalized := normalizeStatus(status.String)
		if normalized != domain.TaskRunning && normalized != domain.TaskWaiting {
			continue
		}

		taskTitle := firstNonEmpty(500, title.String)
		if taskTitle == "" {
			taskTitle = "ZCode session"
		}
		workspace := workspaceLabel(path.String, directory.String)
		duration := int(ageSeconds(now, *startedAt))
		tasks = append(tasks, domain.TaskSummary{
			ID:              taskIDHash("runtime-session", sessionID),
			Title:           taskTitle,
			Workspace:       optionalString(workspace),
			Status:          normalized,
			StartedAt:       startedAt,
			UpdatedAt:       startedAt,
			DurationSeconds: &duration,
		})
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

	// Keep the source contract deliberately small: no raw session IDs, prompt
	// content, provider credentials, or full filesystem paths leave the adapter.
	for i := range tasks {
		tasks[i].Title = truncate(strings.TrimSpace(tasks[i].Title), 500)
	}
	return &Snapshot{Summary: summarizeTasks(tasks), Tasks: tasks}, true, nil
}
