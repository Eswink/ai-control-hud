package zcode

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const CompatibilityEvidenceVersion = 1

var safeEvidenceEventName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

type CompatibilityEvidence struct {
	EvidenceVersion            int            `json:"evidenceVersion"`
	LayoutSource               string         `json:"layoutSource"`
	RuntimeDatabasePresent     bool           `json:"runtimeDatabasePresent"`
	RuntimeDatabaseReadable    bool           `json:"runtimeDatabaseReadable"`
	GoalSchemaCompatible       bool           `json:"goalSchemaCompatible"`
	RuntimeSessionCompatible   bool           `json:"runtimeSessionCompatible"`
	TaskIndexPresent           bool           `json:"taskIndexPresent"`
	TaskIndexReadable          bool           `json:"taskIndexReadable"`
	TaskIndexSchemaCompatible  bool           `json:"taskIndexSchemaCompatible"`
	TurnLogDirectoryReadable   bool           `json:"turnLogDirectoryReadable"`
	TurnLogFilesSampled        int            `json:"turnLogFilesSampled"`
	TurnLogLinesSampled        int            `json:"turnLogLinesSampled"`
	TurnLogRecordsParsed       int            `json:"turnLogRecordsParsed"`
	EventCounts                map[string]int `json:"eventCounts"`
	OtherEventRecords          int            `json:"otherEventRecords"`
	SessionUpdatedTaskSignals  int            `json:"sessionUpdatedTaskSignals"`
	BackgroundTaskTurnStarts   int            `json:"backgroundTaskTurnStarts"`
	ProviderConfigCandidates   int            `json:"providerConfigCandidates"`
	ProviderConfigsReadable    int            `json:"providerConfigsReadable"`
}

// CollectCompatibilityEvidence inspects only bounded schema/event metadata.
// It never returns paths, session IDs, prompt/message bodies, tool payloads,
// provider URLs or credentials.
func (c *Collector) CollectCompatibilityEvidence(ctx context.Context) CompatibilityEvidence {
	evidence := CompatibilityEvidence{
		EvidenceVersion: CompatibilityEvidenceVersion,
		EventCounts:     map[string]int{},
	}

	if fileExists(c.RuntimeDB) {
		evidence.RuntimeDatabasePresent = true
		if db, err := openReadOnly(c.RuntimeDB); err == nil {
			evidence.RuntimeDatabaseReadable = true
			evidence.GoalSchemaCompatible = verifyGoalSchema(ctx, db) == nil
			evidence.RuntimeSessionCompatible =
				verifyTableColumns(ctx, db, "session", runtimeSessionColumns) == nil &&
					verifyTableColumns(ctx, db, "model_usage", runtimeUsageColumns) == nil
			_ = db.Close()
		}
	}

	if fileExists(c.TaskIndexDB) {
		evidence.TaskIndexPresent = true
		if db, err := openReadOnly(c.TaskIndexDB); err == nil {
			evidence.TaskIndexReadable = true
			evidence.TaskIndexSchemaCompatible = verifyTableColumns(ctx, db, "tasks", taskIndexColumns) == nil
			_ = db.Close()
		}
	}

	c.collectTurnLogEvidence(&evidence)
	return evidence
}

func (c *Collector) collectTurnLogEvidence(evidence *CompatibilityEvidence) {
	logDir := strings.TrimSpace(expandHome(c.LogDir))
	if logDir == "" {
		return
	}
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return
	}
	evidence.TurnLogDirectoryReadable = true

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if strings.HasPrefix(name, "zcode-") && strings.HasSuffix(name, ".jsonl") {
			paths = append(paths, filepath.Join(logDir, entry.Name()))
		}
	}
	sort.Strings(paths)
	if len(paths) > maxTurnLogFiles {
		paths = paths[len(paths)-maxTurnLogFiles:]
	}
	evidence.TurnLogFilesSampled = len(paths)

	for _, path := range paths {
		data, err := readTail(path, maxTurnLogBytes)
		if err != nil {
			continue
		}
		for _, rawLine := range bytes.Split(data, []byte{'\n'}) {
			line := bytes.TrimSpace(rawLine)
			if len(line) == 0 {
				continue
			}
			evidence.TurnLogLinesSampled++

			var root map[string]json.RawMessage
			if json.Unmarshal(line, &root) != nil {
				continue
			}
			evidence.TurnLogRecordsParsed++

			eventName := evidenceEventName(root)
			if eventName == "" || !safeEvidenceEventName.MatchString(eventName) {
				evidence.OtherEventRecords++
			} else {
				evidence.EventCounts[eventName]++
			}

			if eventName == "session.updated" && hasTaskStatusSignal(root) {
				evidence.SessionUpdatedTaskSignals++
			}
			if eventName == "turn.started" && nestedString(root, "inputSource") == "background_task" {
				evidence.BackgroundTaskTurnStarts++
			}
		}
	}
}

func evidenceEventName(root map[string]json.RawMessage) string {
	for _, key := range []string{"event", "type"} {
		if value := rawString(root[key]); value != "" {
			return value
		}
	}
	for _, container := range []string{"payload", "context"} {
		var nested map[string]json.RawMessage
		if json.Unmarshal(root[container], &nested) != nil {
			continue
		}
		for _, key := range []string{"event", "type"} {
			if value := rawString(nested[key]); value != "" {
				return value
			}
		}
	}
	return ""
}

func hasTaskStatusSignal(root map[string]json.RawMessage) bool {
	for _, container := range []string{"payload", "context"} {
		var nested map[string]json.RawMessage
		if json.Unmarshal(root[container], &nested) != nil {
			continue
		}
		if rawString(nested["taskId"]) != "" && rawString(nested["status"]) != "" {
			return true
		}
	}
	return false
}

func nestedString(root map[string]json.RawMessage, key string) string {
	for _, container := range []string{"payload", "context"} {
		var nested map[string]json.RawMessage
		if json.Unmarshal(root[container], &nested) != nil {
			continue
		}
		if value := rawString(nested[key]); value != "" {
			return value
		}
	}
	return ""
}

func rawString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

// keep database/sql referenced here so future capability expansion does not
// accidentally re-open a second connection type through another helper.
var _ *sql.DB
