package store

import (
	"sync"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
)

type SnapshotStore struct {
	mu    sync.RWMutex
	state domain.HudState
}

func New(initial domain.HudState) (*SnapshotStore, error) {
	if err := initial.Validate(); err != nil {
		return nil, err
	}
	return &SnapshotStore{state: clone(initial)}, nil
}

func (s *SnapshotStore) Get() domain.HudState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.state)
}

func (s *SnapshotStore) Replace(next domain.HudState) error {
	if err := next.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	s.state = clone(next)
	s.mu.Unlock()
	return nil
}

func (s *SnapshotStore) UpdateZCode(next domain.ZCodeState) error {
	return s.mutate(func(state *domain.HudState) {
		state.ZCode = next
	})
}

func (s *SnapshotStore) UpdateCommandCode(next domain.CommandCodeState) error {
	return s.mutate(func(state *domain.HudState) {
		state.CommandCode = next
	})
}

func (s *SnapshotStore) mutate(update func(*domain.HudState)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := clone(s.state)
	update(&next)
	if next.ZCode.Health.Status == domain.SourceOK && next.CommandCode.Health.Status == domain.SourceOK {
		next.Overall.Status = domain.OverallLive
	} else {
		next.Overall.Status = domain.OverallDegraded
	}
	if err := next.Validate(); err != nil {
		return err
	}
	s.state = clone(next)
	return nil
}

func clone(in domain.HudState) domain.HudState {
	out := in

	if in.ZCode.Summary != nil {
		summary := *in.ZCode.Summary
		out.ZCode.Summary = &summary
	}
	if in.ZCode.Tasks != nil {
		out.ZCode.Tasks = make([]domain.TaskSummary, len(in.ZCode.Tasks))
		copy(out.ZCode.Tasks, in.ZCode.Tasks)
		for i := range out.ZCode.Tasks {
			task := &out.ZCode.Tasks[i]
			if task.Workspace != nil {
				value := *task.Workspace
				task.Workspace = &value
			}
			if task.StartedAt != nil {
				value := *task.StartedAt
				task.StartedAt = &value
			}
			if task.UpdatedAt != nil {
				value := *task.UpdatedAt
				task.UpdatedAt = &value
			}
			if task.DurationSeconds != nil {
				value := *task.DurationSeconds
				task.DurationSeconds = &value
			}
			if task.Activity != nil {
				value := *task.Activity
				task.Activity = &value
			}
			if task.Changes != nil {
				changes := *task.Changes
				if changes.Additions != nil {
					value := *changes.Additions
					changes.Additions = &value
				}
				if changes.Deletions != nil {
					value := *changes.Deletions
					changes.Deletions = &value
				}
				task.Changes = &changes
			}
		}
	}

	if in.CommandCode.Usage != nil {
		usage := *in.CommandCode.Usage
		if usage.Plan != nil {
			value := *usage.Plan
			usage.Plan = &value
		}
		if usage.Credit != nil {
			credit := *usage.Credit
			if credit.Remaining != nil {
				value := *credit.Remaining
				credit.Remaining = &value
			}
			if credit.Limit != nil {
				value := *credit.Limit
				credit.Limit = &value
			}
			if credit.Unit != nil {
				value := *credit.Unit
				credit.Unit = &value
			}
			usage.Credit = &credit
		}
		if usage.Windows != nil {
			usage.Windows = make([]domain.UsageWindow, len(in.CommandCode.Usage.Windows))
			copy(usage.Windows, in.CommandCode.Usage.Windows)
			for i := range usage.Windows {
				window := &usage.Windows[i]
				if window.UsedPercent != nil {
					value := *window.UsedPercent
					window.UsedPercent = &value
				}
				if window.ResetAt != nil {
					value := *window.ResetAt
					window.ResetAt = &value
				}
			}
		}
		out.CommandCode.Usage = &usage
	}

	if in.ZCode.Health.LastSuccessAt != nil {
		value := *in.ZCode.Health.LastSuccessAt
		out.ZCode.Health.LastSuccessAt = &value
	}
	if in.ZCode.Health.Message != nil {
		value := *in.ZCode.Health.Message
		out.ZCode.Health.Message = &value
	}
	if in.CommandCode.Health.LastSuccessAt != nil {
		value := *in.CommandCode.Health.LastSuccessAt
		out.CommandCode.Health.LastSuccessAt = &value
	}
	if in.CommandCode.Health.Message != nil {
		value := *in.CommandCode.Health.Message
		out.CommandCode.Health.Message = &value
	}

	return out
}
