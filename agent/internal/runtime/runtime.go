package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/collector/zcode"
	"github.com/Eswink/ai-control-hud/agent/internal/domain"
	"github.com/Eswink/ai-control-hud/agent/internal/store"
)

type ZCodeCollectFunc func(context.Context) (*zcode.Snapshot, error)
type CommandCodeCollectFunc func(context.Context) (*domain.UsageSummary, error)

type Config struct {
	ZCodeInterval       time.Duration
	CommandCodeInterval time.Duration
	ZCodeTimeout        time.Duration
	CommandCodeTimeout  time.Duration
}

func DefaultConfig() Config {
	return Config{
		ZCodeInterval:       time.Second,
		CommandCodeInterval: 60 * time.Second,
		ZCodeTimeout:        3 * time.Second,
		CommandCodeTimeout:  10 * time.Second,
	}
}

type Runtime struct {
	store       *store.SnapshotStore
	zcode       ZCodeCollectFunc
	commandCode CommandCodeCollectFunc
	config      Config
	now         func() time.Time

	startOnce sync.Once
	wg        sync.WaitGroup
}

func New(
	store *store.SnapshotStore,
	zcodeCollect ZCodeCollectFunc,
	commandCodeCollect CommandCodeCollectFunc,
	config Config,
) *Runtime {
	defaults := DefaultConfig()
	if config.ZCodeInterval <= 0 {
		config.ZCodeInterval = defaults.ZCodeInterval
	}
	if config.CommandCodeInterval <= 0 {
		config.CommandCodeInterval = defaults.CommandCodeInterval
	}
	if config.ZCodeTimeout <= 0 {
		config.ZCodeTimeout = defaults.ZCodeTimeout
	}
	if config.CommandCodeTimeout <= 0 {
		config.CommandCodeTimeout = defaults.CommandCodeTimeout
	}
	return &Runtime{
		store:       store,
		zcode:       zcodeCollect,
		commandCode: commandCodeCollect,
		config:      config,
		now:         time.Now,
	}
}

func InitialState(now time.Time, version string, zcodeEnabled, commandCodeEnabled bool) domain.HudState {
	now = now.UTC()
	return domain.HudState{
		SchemaVersion: domain.SchemaVersion,
		Server: domain.ServerInfo{
			Version: version,
			Time:    now,
		},
		Overall: domain.OverallStatus{Status: domain.OverallDegraded},
		ZCode: domain.ZCodeState{
			Health: initialHealth(now, zcodeEnabled, "ZCode collector is not configured"),
		},
		CommandCode: domain.CommandCodeState{
			Health: initialHealth(now, commandCodeEnabled, "CommandCode collector is not configured"),
		},
	}
}

func initialHealth(now time.Time, enabled bool, disabledMessage string) domain.SourceHealth {
	if enabled {
		message := "collector is waiting for the first snapshot"
		return domain.SourceHealth{Status: domain.SourceError, ObservedAt: now, Message: &message}
	}
	message := disabledMessage
	return domain.SourceHealth{Status: domain.SourceDisabled, ObservedAt: now, Message: &message}
}

func (r *Runtime) Start(ctx context.Context) {
	r.startOnce.Do(func() {
		if r.zcode != nil {
			r.wg.Add(1)
			go r.zcodeLoop(ctx)
		}
		if r.commandCode != nil {
			r.wg.Add(1)
			go r.commandCodeLoop(ctx)
		}
	})
}

func (r *Runtime) Wait() {
	r.wg.Wait()
}

func (r *Runtime) CollectZCodeOnce(parent context.Context) {
	if r.zcode == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, r.config.ZCodeTimeout)
	defer cancel()

	snapshot, err := r.zcode(ctx)
	observedAt := r.utcnow()
	if err != nil || snapshot == nil {
		if err == nil {
			err = fmt.Errorf("ZCode collection returned no snapshot")
		}
		r.recordZCodeFailure(observedAt, err)
		return
	}

	lastSuccess := observedAt
	summary := snapshot.Summary
	tasks := snapshot.Tasks
	if tasks == nil {
		tasks = []domain.TaskSummary{}
	}
	_ = r.store.UpdateZCode(domain.ZCodeState{
		Health: domain.SourceHealth{
			Status:        domain.SourceOK,
			ObservedAt:    observedAt,
			LastSuccessAt: &lastSuccess,
		},
		Summary: &summary,
		Tasks:   tasks,
	})
}

func (r *Runtime) CollectCommandCodeOnce(parent context.Context) {
	if r.commandCode == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, r.config.CommandCodeTimeout)
	defer cancel()

	usage, err := r.commandCode(ctx)
	observedAt := r.utcnow()
	if err != nil || usage == nil {
		if err == nil {
			err = fmt.Errorf("CommandCode collection returned no usage")
		}
		r.recordCommandCodeFailure(observedAt, err)
		return
	}

	lastSuccess := observedAt
	_ = r.store.UpdateCommandCode(domain.CommandCodeState{
		Health: domain.SourceHealth{
			Status:        domain.SourceOK,
			ObservedAt:    observedAt,
			LastSuccessAt: &lastSuccess,
		},
		Usage: usage,
	})
}

func (r *Runtime) recordZCodeFailure(observedAt time.Time, err error) {
	previous := r.store.Get().ZCode
	message := publicMessage("zcode", err)
	if previous.Summary != nil && previous.Tasks != nil && previous.Health.LastSuccessAt != nil {
		_ = r.store.UpdateZCode(domain.ZCodeState{
			Health: domain.SourceHealth{
				Status:        domain.SourceStale,
				ObservedAt:    observedAt,
				LastSuccessAt: previous.Health.LastSuccessAt,
				Message:       &message,
			},
			Summary: previous.Summary,
			Tasks:   previous.Tasks,
		})
		return
	}
	_ = r.store.UpdateZCode(domain.ZCodeState{
		Health: domain.SourceHealth{
			Status:     domain.SourceError,
			ObservedAt: observedAt,
			Message:    &message,
		},
	})
}

func (r *Runtime) recordCommandCodeFailure(observedAt time.Time, err error) {
	previous := r.store.Get().CommandCode
	message := publicMessage("commandcode", err)
	if previous.Usage != nil && previous.Health.LastSuccessAt != nil {
		_ = r.store.UpdateCommandCode(domain.CommandCodeState{
			Health: domain.SourceHealth{
				Status:        domain.SourceStale,
				ObservedAt:    observedAt,
				LastSuccessAt: previous.Health.LastSuccessAt,
				Message:       &message,
			},
			Usage: previous.Usage,
		})
		return
	}
	_ = r.store.UpdateCommandCode(domain.CommandCodeState{
		Health: domain.SourceHealth{
			Status:     domain.SourceError,
			ObservedAt: observedAt,
			Message:    &message,
		},
	})
}

func (r *Runtime) zcodeLoop(ctx context.Context) {
	defer r.wg.Done()
	ticker := time.NewTicker(r.config.ZCodeInterval)
	defer ticker.Stop()
	for {
		r.CollectZCodeOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Runtime) commandCodeLoop(ctx context.Context) {
	defer r.wg.Done()
	ticker := time.NewTicker(r.config.CommandCodeInterval)
	defer ticker.Stop()
	for {
		r.CollectCommandCodeOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Runtime) utcnow() time.Time {
	if r.now == nil {
		return time.Now().UTC()
	}
	return r.now().UTC()
}

func publicMessage(source string, err error) string {
	if err == nil {
		return source + " collection failed"
	}
	message := strings.TrimSpace(err.Error())
	if source == "zcode" {
		allowed := []string{
			"ZCode live Goal database read failed",
			"ZCode task index read failed",
			"Unsupported ZCode task index schema",
			"ZCode task sources are unavailable",
		}
		for _, value := range allowed {
			if message == value {
				return value
			}
		}
		return "ZCode collection failed"
	}

	allowedPrefixes := []string{
		"ZCode provider config could not be read",
		"CommandCode provider not found in ZCode",
		"CommandCode provider API key missing in ZCode",
		"CommandCode provider endpoint is not verified",
		"CommandCode billing request failed",
		"CommandCode billing response too large",
		"CommandCode authentication failed",
		"CommandCode billing access denied",
		"CommandCode billing API unavailable",
		"Unsupported CommandCode credits response",
		"Unsupported CommandCode subscription response",
	}
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(message, prefix) {
			return truncateMessage(message)
		}
	}
	return "CommandCode collection failed"
}

func truncateMessage(message string) string {
	const limit = 240
	if len(message) <= limit {
		return message
	}
	return message[:limit]
}
