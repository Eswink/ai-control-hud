package zcode

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
	_ "modernc.org/sqlite"
)

var statusSeparators = regexp.MustCompile(`[\s-]+`)

var runningStatuses = setOf("running", "in_progress", "inprogress", "active", "working", "executing", "streaming", "in_flight", "inflight")
var waitingStatuses = setOf("waiting", "queued", "pending", "ready", "todo")
var failedStatuses = setOf("failed", "failure", "error", "errored")
var completedStatuses = setOf("completed", "complete", "done", "success", "succeeded", "finished")

var goalTargetColumns = setOf(
	"session_id", "objective", "status", "time_used_seconds", "time_updated",
	"summary_title", "active_run_started_at", "active_run_last_seen_at",
)
var goalTodoColumns = setOf("session_id", "content", "status", "position")
var goalSessionColumns = setOf(
	"id", "directory", "path", "title", "summary_additions", "summary_deletions",
)
var taskIndexColumns = setOf(
	"workspace_key", "workspace_path", "task_id", "title", "task_status",
	"updated_at", "pinned", "archived", "deleted",
)

var errGoalSchemaUnavailable = errors.New("ZCode live Goal schema unavailable")

type Snapshot struct {
	Summary domain.ZCodeSummary
	Tasks   []domain.TaskSummary
}

type Collector struct {
	RuntimeDB             string
	TaskIndexDB           string
	HeartbeatSeconds      int
	RecentTerminalSeconds int
	TaskMaxAgeSeconds     int
	TaskLimit             int
	Now                   func() time.Time
}

func New(runtimeDB, taskIndexDB string) *Collector {
	return &Collector{
		RuntimeDB:             expandHome(runtimeDB),
		TaskIndexDB:           expandHome(taskIndexDB),
		HeartbeatSeconds:      120,
		RecentTerminalSeconds: 1800,
		TaskMaxAgeSeconds:     86400,
		TaskLimit:             20,
		Now:                   time.Now,
	}
}

func NewFromEnvironment() (*Collector, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, false
	}
	runtimeDB := envOr("HUD_ZCODE_RUNTIME_DB", filepath.Join(home, ".zcode", "cli", "db", "db.sqlite"))
	taskIndexDB := envOr("HUD_ZCODE_DB", filepath.Join(home, ".zcode", "v2", "tasks-index.sqlite"))
	if !fileExists(runtimeDB) && !fileExists(taskIndexDB) {
		return nil, false
	}
	collector := New(runtimeDB, taskIndexDB)
	collector.HeartbeatSeconds = boundedPositiveEnv("HUD_ZCODE_GOAL_HEARTBEAT_SECONDS", 120, 3600)
	collector.RecentTerminalSeconds = boundedPositiveEnv("HUD_ZCODE_GOAL_RECENT_TERMINAL_SECONDS", 1800, 86400)
	collector.TaskMaxAgeSeconds = boundedPositiveEnv("HUD_ZCODE_TASK_MAX_AGE_SECONDS", 86400, 30*86400)
	collector.TaskLimit = boundedPositiveEnv("HUD_ZCODE_TASK_LIMIT", 20, 100)
	return collector, true
}

func (c *Collector) Collect(ctx context.Context) (*Snapshot, error) {
	if c.Now == nil {
		c.Now = time.Now
	}
	if fileExists(c.RuntimeDB) {
		live, available, err := c.collectGoals(ctx)
		if err != nil {
			return nil, err
		}
		if available && live != nil && len(live.Tasks) > 0 {
			return live, nil
		}

		runtime, runtimeAvailable, err := c.collectRuntimeSessions(ctx)
		if err != nil {
			return nil, err
		}
		if runtimeAvailable && runtime != nil && len(runtime.Tasks) > 0 {
			return runtime, nil
		}
	}
	if fileExists(c.TaskIndexDB) {
		return c.collectTaskIndex(ctx)
	}
	return nil, errors.New("ZCode task sources are unavailable")
}

type goalRow struct {
	SessionID           string
	Objective           sql.NullString
	Status              sql.NullString
	TimeUsedSeconds     sql.NullInt64
	TimeUpdated         sql.NullInt64
	SummaryTitle        sql.NullString
	ActiveRunStartedAt  sql.NullInt64
	ActiveRunLastSeenAt sql.NullInt64
	Directory           sql.NullString
	Path                sql.NullString
	SessionTitle        sql.NullString
	SummaryAdditions    sql.NullInt64
	SummaryDeletions    sql.NullInt64
}

type todoRow struct {
	SessionID string
	Content   string
	Status    sql.NullString
	Position  int
}

func (c *Collector) collectGoals(ctx context.Context) (*Snapshot, bool, error) {
	db, err := openReadOnly(c.RuntimeDB)
	if err != nil {
		return nil, false, errors.New("ZCode live Goal database read failed")
	}
	defer db.Close()

	if err := verifyGoalSchema(ctx, db); err != nil {
		if errors.Is(err, errGoalSchemaUnavailable) {
			return nil, false, nil
		}
		return nil, false, errors.New("ZCode live Goal database read failed")
	}

	limit := bounded(c.TaskLimit, 20, 100)
	rows, err := db.QueryContext(ctx, `
		SELECT st.session_id, st.objective, st.status,
		       st.time_used_seconds, st.time_updated,
		       st.summary_title, st.active_run_started_at,
		       st.active_run_last_seen_at,
		       s.directory, s.path, s.title,
		       s.summary_additions, s.summary_deletions
		FROM session_target AS st
		LEFT JOIN session AS s ON s.id = st.session_id
		ORDER BY COALESCE(st.active_run_last_seen_at, st.time_updated) DESC
		LIMIT ?`, limit*4)
	if err != nil {
		return nil, false, errors.New("ZCode live Goal database read failed")
	}
	defer rows.Close()

	goalRows := make([]goalRow, 0, limit*4)
	for rows.Next() {
		var row goalRow
		if err := rows.Scan(
			&row.SessionID, &row.Objective, &row.Status,
			&row.TimeUsedSeconds, &row.TimeUpdated,
			&row.SummaryTitle, &row.ActiveRunStartedAt,
			&row.ActiveRunLastSeenAt,
			&row.Directory, &row.Path, &row.SessionTitle,
			&row.SummaryAdditions, &row.SummaryDeletions,
		); err != nil {
			return nil, false, errors.New("ZCode live Goal database read failed")
		}
		goalRows = append(goalRows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, false, errors.New("ZCode live Goal database read failed")
	}
	if len(goalRows) == 0 {
		return nil, true, nil
	}

	ids := make([]string, 0, len(goalRows))
	for _, row := range goalRows {
		ids = append(ids, row.SessionID)
	}
	todos, err := loadTodos(ctx, db, ids)
	if err != nil {
		return nil, false, errors.New("ZCode live Goal database read failed")
	}

	now := c.Now().UTC()
	tasks := make([]domain.TaskSummary, 0, limit)
	for _, row := range goalRows {
		if task := c.goalTask(row, todos[row.SessionID], now); task != nil {
			tasks = append(tasks, *task)
		}
		if len(tasks) >= limit {
			break
		}
	}
	if len(tasks) == 0 {
		return nil, true, nil
	}
	return &Snapshot{Summary: summarizeTasks(tasks), Tasks: tasks}, true, nil
}

func (c *Collector) goalTask(row goalRow, todos []todoRow, now time.Time) *domain.TaskSummary {
	heartbeat := timestamp(row.ActiveRunLastSeenAt)
	updatedAt := heartbeat
	if updatedAt == nil {
		updatedAt = timestamp(row.TimeUpdated)
	}
	startedAt := timestamp(row.ActiveRunStartedAt)
	heartbeatFresh := heartbeat != nil && ageSeconds(now, *heartbeat) <= float64(bounded(c.HeartbeatSeconds, 120, 3600))

	todoStatuses := make([]domain.TaskStatus, len(todos))
	for i, todo := range todos {
		todoStatuses[i] = normalizeStatus(todo.Status.String)
	}
	targetStatus := normalizeStatus(row.Status.String)

	var status domain.TaskStatus
	if heartbeatFresh {
		switch {
		case targetStatus == domain.TaskFailed:
			status = domain.TaskFailed
		case targetStatus == domain.TaskCompleted && (len(todos) == 0 || allCompleted(todoStatuses)):
			status = domain.TaskCompleted
		case containsStatus(todoStatuses, domain.TaskWaiting) && !containsStatus(todoStatuses, domain.TaskRunning):
			status = domain.TaskWaiting
		default:
			status = domain.TaskRunning
		}
	} else {
		if targetStatus != domain.TaskFailed && targetStatus != domain.TaskCompleted {
			return nil
		}
		if updatedAt == nil || ageSeconds(now, *updatedAt) > float64(bounded(c.RecentTerminalSeconds, 1800, 86400)) {
			return nil
		}
		status = targetStatus
	}

	var activity *string
	for _, wanted := range []domain.TaskStatus{domain.TaskRunning, domain.TaskWaiting} {
		for i, todo := range todos {
			if todoStatuses[i] == wanted {
				if text := truncate(strings.TrimSpace(todo.Content), 500); text != "" {
					activity = &text
					break
				}
			}
		}
		if activity != nil {
			break
		}
	}

	title := firstNonEmpty(500, row.SessionTitle.String, row.SummaryTitle.String, row.Objective.String)
	if title == "" {
		title = "Goal mode"
	}

	additions := nonnegativeInt(row.SummaryAdditions)
	deletions := nonnegativeInt(row.SummaryDeletions)
	var changes *domain.TaskChanges
	if additions != nil || deletions != nil {
		changes = &domain.TaskChanges{Additions: additions, Deletions: deletions}
	}

	duration := nonnegativeInt(row.TimeUsedSeconds)
	if duration == nil && startedAt != nil {
		seconds := int(ageSeconds(now, *startedAt))
		duration = &seconds
	}
	workspace := workspaceLabel(row.Path.String, row.Directory.String)

	return &domain.TaskSummary{
		ID:              goalID(row.SessionID),
		Title:           title,
		Workspace:       optionalString(workspace),
		Status:          status,
		StartedAt:       startedAt,
		UpdatedAt:       updatedAt,
		DurationSeconds: duration,
		Activity:        activity,
		Changes:         changes,
	}
}

func (c *Collector) collectTaskIndex(ctx context.Context) (*Snapshot, error) {
	db, err := openReadOnly(c.TaskIndexDB)
	if err != nil {
		return nil, errors.New("ZCode task index read failed")
	}
	defer db.Close()
	if err := verifyTableColumns(ctx, db, "tasks", taskIndexColumns); err != nil {
		return nil, errors.New("Unsupported ZCode task index schema")
	}

	limit := bounded(c.TaskLimit, 20, 100)
	maxAge := bounded(c.TaskMaxAgeSeconds, 86400, 30*86400)
	cutoffMS := c.Now().UTC().Add(-time.Duration(maxAge) * time.Second).UnixMilli()

	rows, err := db.QueryContext(ctx, `
		SELECT workspace_key, workspace_path, task_id, title, task_status, updated_at
		FROM tasks
		WHERE archived = 0 AND deleted = 0 AND updated_at >= ?
		ORDER BY pinned DESC, updated_at DESC
		LIMIT ?`, cutoffMS, limit)
	if err != nil {
		return nil, errors.New("ZCode task index read failed")
	}
	defer rows.Close()

	tasks := make([]domain.TaskSummary, 0, limit)
	for rows.Next() {
		var workspaceKey, workspacePath, taskID, title string
		var status sql.NullString
		var updatedAt sql.NullInt64
		if err := rows.Scan(&workspaceKey, &workspacePath, &taskID, &title, &status, &updatedAt); err != nil {
			return nil, errors.New("ZCode task index read failed")
		}
		workspace := workspaceLabel(workspacePath, workspaceKey)
		taskTitle := truncate(strings.TrimSpace(title), 500)
		if taskTitle == "" {
			taskTitle = "(untitled task)"
		}
		tasks = append(tasks, domain.TaskSummary{
			ID:        taskIDHash(workspaceKey, taskID),
			Title:     taskTitle,
			Workspace: optionalString(workspace),
			Status:    normalizeStatus(status.String),
			UpdatedAt: timestamp(updatedAt),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("ZCode task index read failed")
	}

	countRows, err := db.QueryContext(ctx, `
		SELECT task_status, COUNT(*)
		FROM tasks
		WHERE archived = 0 AND deleted = 0 AND updated_at >= ?
		GROUP BY task_status`, cutoffMS)
	if err != nil {
		return nil, errors.New("ZCode task index read failed")
	}
	defer countRows.Close()
	counts := domain.ZCodeSummary{}
	for countRows.Next() {
		var raw sql.NullString
		var count int
		if err := countRows.Scan(&raw, &count); err != nil {
			return nil, errors.New("ZCode task index read failed")
		}
		addStatusCount(&counts, normalizeStatus(raw.String), count)
	}
	if err := countRows.Err(); err != nil {
		return nil, errors.New("ZCode task index read failed")
	}
	return &Snapshot{Summary: counts, Tasks: tasks}, nil
}

func loadTodos(ctx context.Context, db *sql.DB, sessionIDs []string) (map[string][]todoRow, error) {
	result := make(map[string][]todoRow, len(sessionIDs))
	if len(sessionIDs) == 0 {
		return result, nil
	}
	placeholders := make([]string, len(sessionIDs))
	args := make([]any, len(sessionIDs))
	for i, id := range sessionIDs {
		placeholders[i] = "?"
		args[i] = id
		result[id] = nil
	}
	rows, err := db.QueryContext(ctx,
		`SELECT session_id, content, status, position FROM todo WHERE session_id IN (`+strings.Join(placeholders, ",")+`) ORDER BY session_id, position`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var row todoRow
		if err := rows.Scan(&row.SessionID, &row.Content, &row.Status, &row.Position); err != nil {
			return nil, err
		}
		result[row.SessionID] = append(result[row.SessionID], row)
	}
	return result, rows.Err()
}

func verifyGoalSchema(ctx context.Context, db *sql.DB) error {
	for table, required := range map[string]map[string]struct{}{
		"session_target": goalTargetColumns,
		"todo":           goalTodoColumns,
		"session":        goalSessionColumns,
	} {
		if err := verifyTableColumns(ctx, db, table, required); err != nil {
			return errGoalSchemaUnavailable
		}
	}
	return nil
}

func verifyTableColumns(ctx context.Context, db *sql.DB, table string, required map[string]struct{}) error {
	var found string
	if err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&found); err != nil {
		return err
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info("%s")`, table))
	if err != nil {
		return err
	}
	defer rows.Close()
	columns := map[string]struct{}{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		columns[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for name := range required {
		if _, ok := columns[name]; !ok {
			return fmt.Errorf("missing column %s.%s", table, name)
		}
	}
	return nil
}

func openReadOnly(path string) (*sql.DB, error) {
	absolute, err := filepath.Abs(expandHome(path))
	if err != nil {
		return nil, err
	}

	// RFC 8089 absolute file URLs require a leading slash before a Windows
	// drive letter. Without it `D:/...` becomes a drive-relative URI and the
	// SQLite driver can open a different database than the file we inspected.
	urlPath := filepath.ToSlash(absolute)
	if !strings.HasPrefix(urlPath, "/") {
		urlPath = "/" + urlPath
	}
	u := &url.URL{Scheme: "file", Path: urlPath}
	query := u.Query()
	query.Set("mode", "ro")
	u.RawQuery = query.Encode()

	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func normalizeStatus(value string) domain.TaskStatus {
	normalized := statusSeparators.ReplaceAllString(strings.ToLower(strings.TrimSpace(value)), "_")
	if _, ok := runningStatuses[normalized]; ok {
		return domain.TaskRunning
	}
	if _, ok := waitingStatuses[normalized]; ok {
		return domain.TaskWaiting
	}
	if _, ok := failedStatuses[normalized]; ok {
		return domain.TaskFailed
	}
	if _, ok := completedStatuses[normalized]; ok {
		return domain.TaskCompleted
	}
	return domain.TaskUnknown
}

func summarizeTasks(tasks []domain.TaskSummary) domain.ZCodeSummary {
	summary := domain.ZCodeSummary{}
	for _, task := range tasks {
		addStatusCount(&summary, task.Status, 1)
	}
	return summary
}

func addStatusCount(summary *domain.ZCodeSummary, status domain.TaskStatus, count int) {
	switch status {
	case domain.TaskRunning:
		summary.Running += count
	case domain.TaskWaiting:
		summary.Waiting += count
	case domain.TaskFailed:
		summary.Failed += count
	case domain.TaskCompleted:
		summary.Completed += count
	}
}

func timestamp(value sql.NullInt64) *time.Time {
	if !value.Valid || value.Int64 <= 0 {
		return nil
	}
	raw := value.Int64
	var parsed time.Time
	switch {
	case raw > 100_000_000_000_000:
		parsed = time.Unix(raw/1_000_000, (raw%1_000_000)*1_000).UTC()
	case raw > 100_000_000_000:
		parsed = time.UnixMilli(raw).UTC()
	default:
		parsed = time.Unix(raw, 0).UTC()
	}
	return &parsed
}

func nonnegativeInt(value sql.NullInt64) *int {
	if !value.Valid || value.Int64 < 0 {
		return nil
	}
	converted := int(value.Int64)
	return &converted
}

func workspaceLabel(candidates ...string) string {
	for _, raw := range candidates {
		raw = strings.Trim(strings.TrimSpace(raw), `/\\`)
		if raw == "" {
			continue
		}
		parts := strings.FieldsFunc(raw, func(r rune) bool { return r == '/' || r == '\\' })
		if len(parts) > 0 {
			return truncate(parts[len(parts)-1], 500)
		}
	}
	return ""
}

func goalID(sessionID string) string {
	digest := sha256.Sum256([]byte(sessionID))
	return fmt.Sprintf("zcode-goal-%x", digest[:16])
}

func taskIDHash(workspaceKey, taskID string) string {
	digest := sha256.Sum256([]byte(workspaceKey + "\x00" + taskID))
	return fmt.Sprintf("zcode-%x", digest[:16])
}

func ageSeconds(now, then time.Time) float64 {
	if then.After(now) {
		return 0
	}
	return now.Sub(then).Seconds()
}

func containsStatus(values []domain.TaskStatus, target domain.TaskStatus) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func allCompleted(values []domain.TaskStatus) bool {
	for _, value := range values {
		if value != domain.TaskCompleted {
			return false
		}
	}
	return true
}

func firstNonEmpty(limit int, values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return truncate(value, limit)
		}
	}
	return ""
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func setOf(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func expandHome(path string) string {
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if len(path) > 1 && (path[1] == '/' || path[1] == '\\') {
		return filepath.Join(home, path[2:])
	}
	return path
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(expandHome(path))
	return err == nil && !info.IsDir()
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return expandHome(value)
	}
	return fallback
}

func boundedPositiveEnv(name string, fallback, maximum int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	if parsed > maximum {
		return maximum
	}
	return parsed
}

func bounded(value, fallback, maximum int) int {
	if value <= 0 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
