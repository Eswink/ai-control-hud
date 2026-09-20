package zcode

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
)

const (
	maxTurnLogFiles = 2
	maxTurnLogBytes = int64(512 * 1024)
)

type turnLogState struct {
	Open                 bool
	StartedAt            time.Time
	UpdatedAt            time.Time
	BackgroundTasks      map[string]struct{}
	BackgroundTrackCount int
}

func defaultLogDir(runtimeDB string) string {
	if strings.TrimSpace(runtimeDB) == "" {
		return ""
	}
	path := filepath.Clean(expandHome(runtimeDB))
	parent := filepath.Dir(path)
	if strings.EqualFold(filepath.Base(parent), "db") {
		return filepath.Join(filepath.Dir(parent), "log")
	}
	return ""
}

func (c *Collector) readTurnStates() (map[string]turnLogState, error) {
	states := map[string]turnLogState{}
	logDir := strings.TrimSpace(expandHome(c.LogDir))
	if logDir == "" {
		return states, nil
	}

	entries, err := os.ReadDir(logDir)
	if err != nil {
		if os.IsNotExist(err) || os.IsPermission(err) {
			return states, nil
		}
		return nil, err
	}

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(strings.ToLower(name), "zcode-") && strings.HasSuffix(strings.ToLower(name), ".jsonl") {
			paths = append(paths, filepath.Join(logDir, name))
		}
	}
	if len(paths) == 0 {
		return states, nil
	}
	sort.Strings(paths)
	if len(paths) > maxTurnLogFiles {
		paths = paths[len(paths)-maxTurnLogFiles:]
	}

	for _, path := range paths {
		data, err := readTail(path, maxTurnLogBytes)
		if err != nil {
			continue
		}
		for _, line := range bytes.Split(data, []byte{'\n'}) {
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}

			var root map[string]json.RawMessage
			if json.Unmarshal(line, &root) != nil {
				continue
			}
			sessionID := logSessionID(root)
			if sessionID == "" {
				continue
			}
			timestamp, ok := logTimestamp(root)
			if !ok {
				continue
			}
			eventName := strings.ToLower(strings.TrimSpace(evidenceEventName(root)))
			if eventName == "" {
				continue
			}

			state := states[sessionID]
			switch eventName {
			case "turn.started":
				alreadyActive := state.Open || len(state.BackgroundTasks) > 0 || state.BackgroundTrackCount > 0
				state.Open = true
				if !alreadyActive || state.StartedAt.IsZero() {
					state.StartedAt = timestamp
				}
				state.UpdatedAt = timestamp

			case "turn.completed", "turn.failed", "turn.cancelled", "turn.canceled":
				state.Open = false
				state.UpdatedAt = timestamp

			case "session.updated":
				taskID := nestedString(root, "taskId")
				status := nestedString(root, "status")
				if taskID != "" && status != "" {
					wasActive := state.Open || len(state.BackgroundTasks) > 0 || state.BackgroundTrackCount > 0
					switch backgroundTaskStatus(status) {
					case backgroundTaskRunning:
						if state.BackgroundTasks == nil {
							state.BackgroundTasks = map[string]struct{}{}
						}
						if !wasActive {
							state.StartedAt = timestamp
						}
						state.BackgroundTasks[taskID] = struct{}{}
						state.UpdatedAt = timestamp
					case backgroundTaskTerminal:
						if state.BackgroundTasks != nil {
							delete(state.BackgroundTasks, taskID)
						}
						state.UpdatedAt = timestamp
					}
				} else if state.Open || len(state.BackgroundTasks) > 0 || state.BackgroundTrackCount > 0 {
					state.UpdatedAt = timestamp
				}

			case "background_task.tracking.started":
				wasActive := state.Open || len(state.BackgroundTasks) > 0 || state.BackgroundTrackCount > 0
				if state.BackgroundTrackCount < 1024 {
					state.BackgroundTrackCount++
				}
				if !wasActive || state.StartedAt.IsZero() {
					state.StartedAt = timestamp
				}
				state.UpdatedAt = timestamp

			case "background_task.tracking.terminal":
				if state.BackgroundTrackCount > 0 {
					state.BackgroundTrackCount--
				}
				state.UpdatedAt = timestamp

			case "background_task.notification.enqueued", "background_task.notification.runtime_enqueued":
				// Notification queueing is post-task delivery bookkeeping. It is
				// intentionally not treated as live work.
				if state.Open || len(state.BackgroundTasks) > 0 || state.BackgroundTrackCount > 0 {
					state.UpdatedAt = timestamp
				}

			default:
				if state.Open || len(state.BackgroundTasks) > 0 || state.BackgroundTrackCount > 0 {
					if state.UpdatedAt.IsZero() || timestamp.After(state.UpdatedAt) {
						state.UpdatedAt = timestamp
					}
				}
			}
			states[sessionID] = state
		}
	}
	return states, nil
}

type backgroundTaskState int

const (
	backgroundTaskUnknown backgroundTaskState = iota
	backgroundTaskRunning
	backgroundTaskTerminal
)

func backgroundTaskStatus(raw string) backgroundTaskState {
	switch normalizeStatus(raw) {
	case domain.TaskRunning, domain.TaskWaiting:
		return backgroundTaskRunning
	case domain.TaskCompleted, domain.TaskFailed:
		return backgroundTaskTerminal
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "cancelled", "canceled", "stopped", "killed", "terminated":
		return backgroundTaskTerminal
	default:
		return backgroundTaskUnknown
	}
}

func logSessionID(root map[string]json.RawMessage) string {
	for _, key := range []string{"sessionId", "session_id"} {
		if value := rawString(root[key]); value != "" {
			return value
		}
	}
	for _, container := range []string{"payload", "context"} {
		var nested map[string]json.RawMessage
		if json.Unmarshal(root[container], &nested) != nil {
			continue
		}
		for _, key := range []string{"sessionId", "session_id"} {
			if value := rawString(nested[key]); value != "" {
				return value
			}
		}
	}
	return ""
}

func logTimestamp(root map[string]json.RawMessage) (time.Time, bool) {
	if timestamp, ok := parseTurnLogTimestamp(root["timestamp"]); ok {
		return timestamp, true
	}
	for _, container := range []string{"payload", "context"} {
		var nested map[string]json.RawMessage
		if json.Unmarshal(root[container], &nested) != nil {
			continue
		}
		if timestamp, ok := parseTurnLogTimestamp(nested["timestamp"]); ok {
			return timestamp, true
		}
	}
	return time.Time{}, false
}

func (state turnLogState) active(now time.Time, freshSeconds int) bool {
	if (!state.Open && len(state.BackgroundTasks) == 0 && state.BackgroundTrackCount == 0) || state.UpdatedAt.IsZero() {
		return false
	}
	freshSeconds = bounded(freshSeconds, 1800, 4*3600)
	return ageSeconds(now.UTC(), state.UpdatedAt.UTC()) <= float64(freshSeconds)
}

func readTail(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	start := info.Size() - limit
	if start < 0 {
		start = 0
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(file, limit))
	if err != nil {
		return nil, err
	}
	if start > 0 {
		if newline := bytes.IndexByte(data, '\n'); newline >= 0 {
			data = data[newline+1:]
		} else {
			data = nil
		}
	}
	return data, nil
}

func parseTurnLogTimestamp(raw json.RawMessage) (time.Time, bool) {
	if len(raw) == 0 {
		return time.Time{}, false
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(text))
		if err == nil {
			return parsed.UTC(), true
		}
		return time.Time{}, false
	}

	var number float64
	if json.Unmarshal(raw, &number) != nil || number <= 0 {
		return time.Time{}, false
	}
	rawInt := int64(number)
	var parsed time.Time
	switch {
	case rawInt > 100_000_000_000_000:
		parsed = time.Unix(rawInt/1_000_000, (rawInt%1_000_000)*1_000)
	case rawInt > 100_000_000_000:
		parsed = time.UnixMilli(rawInt)
	default:
		parsed = time.Unix(rawInt, 0)
	}
	return parsed.UTC(), true
}
