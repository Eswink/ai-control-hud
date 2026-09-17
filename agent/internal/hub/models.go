package hub

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
	"github.com/Eswink/ai-control-hud/agent/internal/events"
)

const EventSchemaVersion = 1

type StateEnvelope struct {
	AgentID string          `json:"agentId"`
	SentAt  time.Time       `json:"sentAt"`
	State   domain.HudState `json:"state"`
}

func (e StateEnvelope) Validate() error {
	if err := validateAgentID(e.AgentID); err != nil {
		return err
	}
	if e.SentAt.IsZero() {
		return errors.New("sentAt is required")
	}
	if err := e.State.Validate(); err != nil {
		return fmt.Errorf("state: %w", err)
	}
	return nil
}

type Heartbeat struct {
	AgentID      string    `json:"agentId"`
	SentAt       time.Time `json:"sentAt"`
	AgentVersion *string   `json:"agentVersion"`
}

func (h Heartbeat) Validate() error {
	if err := validateAgentID(h.AgentID); err != nil {
		return err
	}
	if h.SentAt.IsZero() {
		return errors.New("sentAt is required")
	}
	if h.AgentVersion != nil && len(*h.AgentVersion) > 64 {
		return errors.New("agentVersion exceeds 64 characters")
	}
	return nil
}

type EventBatch struct {
	AgentID string         `json:"agentId"`
	SentAt  time.Time      `json:"sentAt"`
	Events  []events.Event `json:"events"`
}

func (b EventBatch) Validate() error {
	if err := validateAgentID(b.AgentID); err != nil {
		return err
	}
	if b.SentAt.IsZero() {
		return errors.New("sentAt is required")
	}
	if len(b.Events) < 1 || len(b.Events) > 100 {
		return errors.New("events must contain 1..100 items")
	}
	for i, event := range b.Events {
		if err := validateEvent(event); err != nil {
			return fmt.Errorf("events[%d]: %w", i, err)
		}
	}
	return nil
}

type AgentReceipt struct {
	Status     string    `json:"status"`
	AgentID    string    `json:"agentId"`
	ReceivedAt time.Time `json:"receivedAt"`
}

type EventReceipt struct {
	Status     string    `json:"status"`
	AgentID    string    `json:"agentId"`
	ReceivedAt time.Time `json:"receivedAt"`
	Accepted   int       `json:"accepted"`
	Duplicates int       `json:"duplicates"`
}

type HubEvent struct {
	Seq        int64     `json:"seq"`
	AgentID    string    `json:"agentId"`
	ReceivedAt time.Time `json:"receivedAt"`
	events.Event
}

type EventPage struct {
	SchemaVersion int        `json:"schemaVersion"`
	Events        []HubEvent `json:"events"`
	NextAfter     int64      `json:"nextAfter"`
	LatestSeq     int64      `json:"latestSeq"`
	OldestSeq     int64      `json:"oldestSeq"`
}

func validateAgentID(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("agentId is required")
	}
	if len(value) > 128 {
		return errors.New("agentId exceeds 128 characters")
	}
	return nil
}

func validateEvent(event events.Event) error {
	if err := event.Validate(); err != nil {
		return err
	}
	if len(event.EventID) > 128 {
		return errors.New("eventId exceeds 128 characters")
	}
	if len(event.Task.ID) > 256 {
		return errors.New("task.id exceeds 256 characters")
	}
	if len(event.Task.Title) > 500 {
		return errors.New("task.title exceeds 500 characters")
	}
	if event.Task.Workspace != nil && len(*event.Task.Workspace) > 500 {
		return errors.New("task.workspace exceeds 500 characters")
	}
	return nil
}
