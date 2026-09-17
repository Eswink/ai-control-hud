package remote

import (
	"context"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
)

const (
	outboxWarningPendingEvents = 10_000
	outboxWarningPendingAge    = 24 * time.Hour
)

func (r *Runtime) OutboxDiagnostics(ctx context.Context, now time.Time) domain.OutboxDiagnostics {
	if r == nil || r.outbox == nil {
		return domain.OutboxDiagnostics{Status: "not-configured"}
	}
	stats, err := r.outbox.Stats(ctx)
	if err != nil {
		return domain.OutboxDiagnostics{Status: "error"}
	}
	result := domain.OutboxDiagnostics{
		Status:            "ok",
		PendingEvents:     stats.PendingEvents,
		TaskBaselineRows:  stats.TaskBaselineRows,
		CompactedTaskRows: stats.CompactedTaskRows,
		ReusableBytes:     stats.ReusableBytes,
	}
	if stats.OldestPendingAt != nil {
		age := now.UTC().Sub(stats.OldestPendingAt.UTC())
		if age < 0 {
			age = 0
		}
		seconds := int64(age / time.Second)
		result.OldestPendingAgeSeconds = &seconds
		if age >= outboxWarningPendingAge {
			result.Status = "warning"
		}
	}
	if stats.PendingEvents >= outboxWarningPendingEvents {
		result.Status = "warning"
	}
	return result
}
