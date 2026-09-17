package hub

import (
	"context"
	"sync"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
)

type primaryStateCache struct {
	mu       sync.RWMutex
	state    domain.HudState
	lastSeen time.Time
	valid    bool
}

func (c *primaryStateCache) load() (domain.HudState, time.Time, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.valid {
		return domain.HudState{}, time.Time{}, false
	}
	return cloneHudState(c.state), c.lastSeen, true
}

func (c *primaryStateCache) store(state domain.HudState, lastSeen time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = cloneHudState(state)
	c.lastSeen = lastSeen
	c.valid = true
}

func (c *primaryStateCache) heartbeat(lastSeen time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.valid {
		c.lastSeen = lastSeen
	}
}

func (s *Server) loadPrimaryState(ctx context.Context) (domain.HudState, time.Time, bool, error) {
	if state, lastSeen, ok := s.stateCache.load(); ok {
		return state, lastSeen, true, nil
	}
	state, lastSeen, found, err := s.store.LoadState(ctx, s.config.PrimaryAgentID)
	if err != nil || !found {
		return state, lastSeen, found, err
	}
	s.stateCache.store(state, lastSeen)
	return cloneHudState(state), lastSeen, true, nil
}

// cloneHudState prevents projection/render code from mutating the cache through
// slices or pointer fields while avoiding JSON marshal/unmarshal on every
// Android poll. The cache contains only one primary-Agent snapshot.
func cloneHudState(source domain.HudState) domain.HudState {
	result := source
	result.ZCode.Health = cloneHealth(source.ZCode.Health)
	if source.ZCode.Summary != nil {
		summary := *source.ZCode.Summary
		result.ZCode.Summary = &summary
	}
	if source.ZCode.Tasks != nil {
		result.ZCode.Tasks = make([]domain.TaskSummary, len(source.ZCode.Tasks))
		for i, task := range source.ZCode.Tasks {
			result.ZCode.Tasks[i] = cloneTask(task)
		}
	}
	result.CommandCode.Health = cloneHealth(source.CommandCode.Health)
	if source.CommandCode.Usage != nil {
		usage := *source.CommandCode.Usage
		usage.Plan = cloneString(source.CommandCode.Usage.Plan)
		if source.CommandCode.Usage.Credit != nil {
			credit := *source.CommandCode.Usage.Credit
			credit.Remaining = cloneFloat64(source.CommandCode.Usage.Credit.Remaining)
			credit.Limit = cloneFloat64(source.CommandCode.Usage.Credit.Limit)
			credit.Unit = cloneString(source.CommandCode.Usage.Credit.Unit)
			usage.Credit = &credit
		}
		if source.CommandCode.Usage.Windows != nil {
			usage.Windows = make([]domain.UsageWindow, len(source.CommandCode.Usage.Windows))
			for i, window := range source.CommandCode.Usage.Windows {
				usage.Windows[i] = window
				usage.Windows[i].UsedPercent = cloneFloat64(window.UsedPercent)
				usage.Windows[i].ResetAt = cloneTime(window.ResetAt)
			}
		}
		result.CommandCode.Usage = &usage
	}
	return result
}

func cloneHealth(source domain.SourceHealth) domain.SourceHealth {
	result := source
	result.LastSuccessAt = cloneTime(source.LastSuccessAt)
	result.Message = cloneString(source.Message)
	return result
}

func cloneTask(source domain.TaskSummary) domain.TaskSummary {
	result := source
	result.Workspace = cloneString(source.Workspace)
	result.StartedAt = cloneTime(source.StartedAt)
	result.UpdatedAt = cloneTime(source.UpdatedAt)
	result.DurationSeconds = cloneInt(source.DurationSeconds)
	result.Activity = cloneString(source.Activity)
	if source.Changes != nil {
		changes := *source.Changes
		changes.Additions = cloneInt(source.Changes.Additions)
		changes.Deletions = cloneInt(source.Changes.Deletions)
		result.Changes = &changes
	}
	return result
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
