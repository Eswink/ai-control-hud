package hub

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"
)

const DefaultRetentionBatchSize = 1000

type RetentionPolicy struct {
	MaxAge    time.Duration
	MinEvents int
	MaxEvents int
	BatchSize int
}

type PruneResult struct {
	Deleted int
	Agents  int
}

func (p RetentionPolicy) Validate() error {
	if p.MaxAge < 24*time.Hour {
		return errors.New("event retention max age must be at least 24 hours")
	}
	if p.MinEvents < 0 {
		return errors.New("event retention minimum must be non-negative")
	}
	if p.MaxEvents < 1 {
		return errors.New("event retention maximum must be positive")
	}
	if p.MaxEvents < p.MinEvents {
		return errors.New("event retention maximum must be greater than or equal to minimum")
	}
	if p.BatchSize < 1 || p.BatchSize > 10_000 {
		return errors.New("event retention batch size must be within 1..10000")
	}
	return nil
}

func RetentionPolicyFromConfig(config Config) RetentionPolicy {
	return RetentionPolicy{
		MaxAge:    time.Duration(config.EventRetentionDays) * 24 * time.Hour,
		MinEvents: config.EventRetentionMin,
		MaxEvents: config.EventRetentionMax,
		BatchSize: DefaultRetentionBatchSize,
	}
}

// EventBounds returns the currently retained sequence range for one Agent.
// A zero oldest/latest pair means that Agent has no retained events.
func (s *Store) EventBounds(ctx context.Context, agentID string) (oldest, latest int64, count int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(MIN(seq), 0), COALESCE(MAX(seq), 0), COUNT(*)
		FROM events
		WHERE agent_id = ?
	`, agentID).Scan(&oldest, &latest, &count)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("query hub event bounds: %w", err)
	}
	return oldest, latest, count, nil
}

// PruneEvents removes retained Hub events in small transactions. Age-based
// pruning never removes the newest MinEvents rows for an Agent. MaxEvents is a
// hard safety cap and may remove younger events if an Agent generates events
// unusually quickly. Pending Agent-side outbox rows are outside this database
// and are never touched by this method.
func (s *Store) PruneEvents(ctx context.Context, now time.Time, policy RetentionPolicy) (PruneResult, error) {
	if err := policy.Validate(); err != nil {
		return PruneResult{}, err
	}
	agentIDs, err := s.eventAgentIDs(ctx)
	if err != nil {
		return PruneResult{}, err
	}
	result := PruneResult{Agents: len(agentIDs)}
	cutoff := now.UTC().Add(-policy.MaxAge)
	for _, agentID := range agentIDs {
		for {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			deleted, err := s.pruneAgentBatch(ctx, agentID, cutoff, policy)
			if err != nil {
				return result, err
			}
			result.Deleted += deleted
			if deleted < policy.BatchSize {
				break
			}
			// Let request-serving goroutines run between bounded delete batches.
			runtime.Gosched()
		}
	}
	return result, nil
}

func (s *Store) eventAgentIDs(ctx context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT agent_id FROM events ORDER BY agent_id`)
	if err != nil {
		return nil, fmt.Errorf("query event Agent IDs: %w", err)
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var agentID string
		if err := rows.Scan(&agentID); err != nil {
			return nil, fmt.Errorf("scan event Agent ID: %w", err)
		}
		result = append(result, agentID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate event Agent IDs: %w", err)
	}
	return result, nil
}

func (s *Store) pruneAgentBatch(ctx context.Context, agentID string, cutoff time.Time, policy RetentionPolicy) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin event retention transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE agent_id = ?`, agentID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count retained events: %w", err)
	}
	if count == 0 {
		return 0, nil
	}

	var forceDeleteThrough int64
	if count > policy.MaxEvents {
		excess := count - policy.MaxEvents
		if err := tx.QueryRowContext(ctx, `
			SELECT seq FROM events
			WHERE agent_id = ?
			ORDER BY seq ASC
			LIMIT 1 OFFSET ?
		`, agentID, excess-1).Scan(&forceDeleteThrough); err != nil {
			return 0, fmt.Errorf("resolve event maximum retention floor: %w", err)
		}
	}

	var minKeepSeq int64
	switch {
	case policy.MinEvents == 0:
		minKeepSeq = int64(^uint64(0) >> 1)
	case count > policy.MinEvents:
		if err := tx.QueryRowContext(ctx, `
			SELECT seq FROM events
			WHERE agent_id = ?
			ORDER BY seq DESC
			LIMIT 1 OFFSET ?
		`, agentID, policy.MinEvents-1).Scan(&minKeepSeq); err != nil {
			return 0, fmt.Errorf("resolve event minimum retention floor: %w", err)
		}
	}

	result, err := tx.ExecContext(ctx, `
		DELETE FROM events
		WHERE seq IN (
			SELECT seq FROM events
			WHERE agent_id = ?
			  AND (
				(? > 0 AND seq <= ?)
				OR
				(? > 0 AND seq < ? AND received_at < ?)
			  )
			ORDER BY seq ASC
			LIMIT ?
		)
	`,
		agentID,
		forceDeleteThrough, forceDeleteThrough,
		minKeepSeq, minKeepSeq, formatTime(cutoff),
		policy.BatchSize,
	)
	if err != nil {
		return 0, fmt.Errorf("prune retained events: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read event retention result: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit event retention transaction: %w", err)
	}
	return int(rows), nil
}
