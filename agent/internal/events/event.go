package events

import (
	"errors"
	"fmt"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
)

type Type string

const (
	TaskCompleted Type = "task.completed"
	TaskFailed    Type = "task.failed"
)

type Task struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Workspace *string           `json:"workspace"`
	Status    domain.TaskStatus `json:"status"`
}

type Event struct {
	EventID    string    `json:"eventId"`
	Type       Type      `json:"type"`
	OccurredAt time.Time `json:"occurredAt"`
	Task       Task      `json:"task"`
}

func (e Event) Validate() error {
	if len(e.EventID) < 8 {
		return errors.New("eventId is required")
	}
	if e.OccurredAt.IsZero() {
		return errors.New("occurredAt is required")
	}
	if e.Task.ID == "" || e.Task.Title == "" {
		return errors.New("event task requires id and title")
	}
	switch e.Type {
	case TaskCompleted:
		if e.Task.Status != domain.TaskCompleted {
			return fmt.Errorf("%s requires completed task status", e.Type)
		}
	case TaskFailed:
		if e.Task.Status != domain.TaskFailed {
			return fmt.Errorf("%s requires failed task status", e.Type)
		}
	default:
		return fmt.Errorf("unsupported event type %q", e.Type)
	}
	return nil
}
