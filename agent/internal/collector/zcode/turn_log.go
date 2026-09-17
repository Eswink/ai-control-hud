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
)

const (
	maxTurnLogFiles = 2
	maxTurnLogBytes = int64(512 * 1024)
)

type turnLogState struct {
	Open      bool
	StartedAt time.Time
	UpdatedAt time.Time
}

type turnLogEvent struct {
	Event          string          `json:"event"`
	SessionID      string          `json:"sessionId"`
	SessionIDSnake string          `json:"session_id"`
	Timestamp      json.RawMessage `json:"timestamp"`
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
			var event turnLogEvent
			if json.Unmarshal(line, &event) != nil {
				continue
			}
			sessionID := strings.TrimSpace(event.SessionID)
			if sessionID == "" {
				sessionID = strings.TrimSpace(event.SessionIDSnake)
			}
			if sessionID == "" {
				continue
			}
			timestamp, ok := parseTurnLogTimestamp(event.Timestamp)
			if !ok {
				continue
			}

			state := states[sessionID]
			switch strings.ToLower(strings.TrimSpace(event.Event)) {
			case "turn.started":
				state.Open = true
				state.StartedAt = timestamp
				state.UpdatedAt = timestamp
			case "turn.completed", "turn.failed", "turn.cancelled", "turn.canceled":
				state.Open = false
				state.UpdatedAt = timestamp
			default:
				if state.Open && (state.UpdatedAt.IsZero() || timestamp.After(state.UpdatedAt)) {
					state.UpdatedAt = timestamp
				}
			}
			states[sessionID] = state
		}
	}
	return states, nil
}

func (state turnLogState) active(now time.Time, freshSeconds int) bool {
	if !state.Open || state.UpdatedAt.IsZero() {
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
