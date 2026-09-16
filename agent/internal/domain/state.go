package domain

import (
	"errors"
	"fmt"
	"time"
)

const SchemaVersion = 1

type SourceStatus string

const (
	SourceOK       SourceStatus = "ok"
	SourceStale    SourceStatus = "stale"
	SourceError    SourceStatus = "error"
	SourceDisabled SourceStatus = "disabled"
)

type TaskStatus string

const (
	TaskRunning   TaskStatus = "running"
	TaskWaiting   TaskStatus = "waiting"
	TaskFailed    TaskStatus = "failed"
	TaskCompleted TaskStatus = "completed"
	TaskUnknown   TaskStatus = "unknown"
)

type OverallStatusValue string

const (
	OverallLive     OverallStatusValue = "live"
	OverallDegraded OverallStatusValue = "degraded"
)

type SourceHealth struct {
	Status        SourceStatus `json:"status"`
	ObservedAt    time.Time    `json:"observedAt"`
	LastSuccessAt *time.Time   `json:"lastSuccessAt"`
	Message       *string      `json:"message"`
}

type TaskChanges struct {
	Additions *int `json:"additions"`
	Deletions *int `json:"deletions"`
}

type TaskSummary struct {
	ID              string       `json:"id"`
	Title           string       `json:"title"`
	Workspace       *string      `json:"workspace"`
	Status          TaskStatus   `json:"status"`
	StartedAt       *time.Time   `json:"startedAt"`
	UpdatedAt       *time.Time   `json:"updatedAt"`
	DurationSeconds *int         `json:"durationSeconds"`
	Activity        *string      `json:"activity"`
	Changes         *TaskChanges `json:"changes"`
}

type ZCodeSummary struct {
	Running   int `json:"running"`
	Waiting   int `json:"waiting"`
	Failed    int `json:"failed"`
	Completed int `json:"completed"`
}

type ZCodeState struct {
	Health  SourceHealth    `json:"health"`
	Summary *ZCodeSummary   `json:"summary"`
	Tasks   []TaskSummary   `json:"tasks"`
}

type CreditBalance struct {
	Remaining *float64 `json:"remaining"`
	Limit     *float64 `json:"limit"`
	Unit      *string  `json:"unit"`
}

type UsageWindow struct {
	Name        string     `json:"name"`
	UsedPercent *float64   `json:"usedPercent"`
	ResetAt     *time.Time `json:"resetAt"`
}

type UsageSummary struct {
	Plan    *string        `json:"plan"`
	Credit  *CreditBalance `json:"credit"`
	Windows []UsageWindow  `json:"windows"`
}

type CommandCodeState struct {
	Health SourceHealth  `json:"health"`
	Usage  *UsageSummary `json:"usage"`
}

type ServerInfo struct {
	Version       string    `json:"version"`
	Time          time.Time `json:"time"`
	UptimeSeconds int64     `json:"uptimeSeconds"`
}

type OverallStatus struct {
	Status OverallStatusValue `json:"status"`
}

type HudState struct {
	SchemaVersion int              `json:"schemaVersion"`
	Server        ServerInfo       `json:"server"`
	Overall       OverallStatus    `json:"overall"`
	ZCode         ZCodeState       `json:"zcode"`
	CommandCode   CommandCodeState `json:"commandCode"`
}

type HealthSources struct {
	ZCode       SourceStatus `json:"zcode"`
	CommandCode SourceStatus `json:"commandCode"`
}

type HealthResponse struct {
	Status        string        `json:"status"`
	SchemaVersion int           `json:"schemaVersion"`
	Time          time.Time     `json:"time"`
	Sources       HealthSources `json:"sources"`
}

func (s HudState) Validate() error {
	if s.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schemaVersion: %d", s.SchemaVersion)
	}
	if s.Server.Version == "" {
		return errors.New("server.version is required")
	}
	if s.Server.Time.IsZero() {
		return errors.New("server.time is required")
	}
	if s.Server.UptimeSeconds < 0 {
		return errors.New("server.uptimeSeconds must not be negative")
	}
	if err := validateHealth("zcode", s.ZCode.Health); err != nil {
		return err
	}
	if err := validateHealth("commandCode", s.CommandCode.Health); err != nil {
		return err
	}
	if err := validateZCode(s.ZCode); err != nil {
		return err
	}
	if err := validateCommandCode(s.CommandCode); err != nil {
		return err
	}

	expected := OverallDegraded
	if s.ZCode.Health.Status == SourceOK && s.CommandCode.Health.Status == SourceOK {
		expected = OverallLive
	}
	if s.Overall.Status != expected {
		return fmt.Errorf("overall.status must be %q", expected)
	}
	return nil
}

func validateHealth(name string, health SourceHealth) error {
	if !validSourceStatus(health.Status) {
		return fmt.Errorf("%s.health.status is invalid: %q", name, health.Status)
	}
	if health.ObservedAt.IsZero() {
		return fmt.Errorf("%s.health.observedAt is required", name)
	}
	if health.Status == SourceOK || health.Status == SourceStale {
		if health.LastSuccessAt == nil || health.LastSuccessAt.IsZero() {
			return fmt.Errorf("%s.health.lastSuccessAt is required for %s", name, health.Status)
		}
		if health.LastSuccessAt.After(health.ObservedAt) {
			return fmt.Errorf("%s.health.lastSuccessAt cannot be after observedAt", name)
		}
	}
	return nil
}

func validateZCode(state ZCodeState) error {
	trusted := state.Health.Status == SourceOK || state.Health.Status == SourceStale
	if trusted {
		if state.Summary == nil || state.Tasks == nil {
			return errors.New("ok/stale zcode state requires summary and tasks")
		}
		if state.Summary.Running < 0 || state.Summary.Waiting < 0 || state.Summary.Failed < 0 || state.Summary.Completed < 0 {
			return errors.New("zcode summary counts must not be negative")
		}
		for i, task := range state.Tasks {
			if task.ID == "" || task.Title == "" {
				return fmt.Errorf("zcode.tasks[%d] requires id and title", i)
			}
			if !validTaskStatus(task.Status) {
				return fmt.Errorf("zcode.tasks[%d].status is invalid: %q", i, task.Status)
			}
			if task.DurationSeconds != nil && *task.DurationSeconds < 0 {
				return fmt.Errorf("zcode.tasks[%d].durationSeconds must not be negative", i)
			}
		}
		return nil
	}
	if state.Summary != nil || state.Tasks != nil {
		return errors.New("error/disabled zcode state must not expose data")
	}
	return nil
}

func validateCommandCode(state CommandCodeState) error {
	trusted := state.Health.Status == SourceOK || state.Health.Status == SourceStale
	if trusted {
		if state.Usage == nil {
			return errors.New("ok/stale commandCode state requires usage")
		}
		if state.Usage.Credit != nil {
			credit := state.Usage.Credit
			if credit.Remaining != nil && *credit.Remaining < 0 {
				return errors.New("commandCode credit remaining must not be negative")
			}
			if credit.Limit != nil && *credit.Limit <= 0 {
				return errors.New("commandCode credit limit must be positive")
			}
			if credit.Remaining != nil && credit.Limit != nil && *credit.Remaining > *credit.Limit {
				return errors.New("commandCode credit remaining cannot exceed limit")
			}
		}
		seen := map[string]struct{}{}
		for i, window := range state.Usage.Windows {
			if window.Name == "" {
				return fmt.Errorf("commandCode.usage.windows[%d].name is required", i)
			}
			if _, exists := seen[window.Name]; exists {
				return fmt.Errorf("commandCode usage window %q is duplicated", window.Name)
			}
			seen[window.Name] = struct{}{}
			if window.UsedPercent != nil && (*window.UsedPercent < 0 || *window.UsedPercent > 100) {
				return fmt.Errorf("commandCode.usage.windows[%d].usedPercent must be within 0..100", i)
			}
		}
		return nil
	}
	if state.Usage != nil {
		return errors.New("error/disabled commandCode state must not expose usage")
	}
	return nil
}

func validSourceStatus(status SourceStatus) bool {
	switch status {
	case SourceOK, SourceStale, SourceError, SourceDisabled:
		return true
	default:
		return false
	}
}

func validTaskStatus(status TaskStatus) bool {
	switch status {
	case TaskRunning, TaskWaiting, TaskFailed, TaskCompleted, TaskUnknown:
		return true
	default:
		return false
	}
}
