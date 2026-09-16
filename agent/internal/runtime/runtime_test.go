package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/collector/zcode"
	"github.com/Eswink/ai-control-hud/agent/internal/domain"
	"github.com/Eswink/ai-control-hud/agent/internal/store"
)

func TestIndependentCollectorsReachLive(t *testing.T) {
	now := time.Date(2026, 9, 16, 6, 0, 0, 0, time.UTC)
	stateStore := newRuntimeStore(t, now, true, true)

	zTitle := "live goal"
	plan := "GOAT"
	remaining := 64.2
	limit := 70.0
	unit := "USD"
	collectorRuntime := New(
		stateStore,
		func(context.Context) (*zcode.Snapshot, error) {
			return &zcode.Snapshot{
				Summary: domain.ZCodeSummary{Running: 1},
				Tasks: []domain.TaskSummary{{
					ID:     "zcode-goal-test",
					Title:  zTitle,
					Status: domain.TaskRunning,
				}},
			}, nil
		},
		func(context.Context) (*domain.UsageSummary, error) {
			return &domain.UsageSummary{
				Plan: &plan,
				Credit: &domain.CreditBalance{
					Remaining: &remaining,
					Limit:     &limit,
					Unit:      &unit,
				},
				Windows: []domain.UsageWindow{},
			}, nil
		},
		DefaultConfig(),
	)
	collectorRuntime.now = func() time.Time { return now }

	collectorRuntime.CollectZCodeOnce(context.Background())
	mid := stateStore.Get()
	if mid.ZCode.Health.Status != domain.SourceOK || mid.CommandCode.Health.Status != domain.SourceError {
		t.Fatalf("unexpected intermediate health: z=%s cc=%s", mid.ZCode.Health.Status, mid.CommandCode.Health.Status)
	}
	if mid.Overall.Status != domain.OverallDegraded {
		t.Fatalf("intermediate overall = %q", mid.Overall.Status)
	}

	collectorRuntime.CollectCommandCodeOnce(context.Background())
	got := stateStore.Get()
	if got.Overall.Status != domain.OverallLive {
		t.Fatalf("overall = %q", got.Overall.Status)
	}
	if got.ZCode.Summary == nil || got.ZCode.Summary.Running != 1 || len(got.ZCode.Tasks) != 1 {
		t.Fatalf("zcode state = %+v", got.ZCode)
	}
	if got.CommandCode.Usage == nil || got.CommandCode.Usage.Plan == nil || *got.CommandCode.Usage.Plan != "GOAT" {
		t.Fatalf("commandCode state = %+v", got.CommandCode)
	}
}

func TestFirstFailureIsErrorWithoutFabricatedData(t *testing.T) {
	now := time.Date(2026, 9, 16, 6, 0, 0, 0, time.UTC)
	stateStore := newRuntimeStore(t, now, true, true)
	secret := "do-not-leak-this-secret"
	collectorRuntime := New(
		stateStore,
		func(context.Context) (*zcode.Snapshot, error) {
			return nil, errors.New(secret)
		},
		func(context.Context) (*domain.UsageSummary, error) {
			return nil, errors.New(secret)
		},
		DefaultConfig(),
	)
	collectorRuntime.now = func() time.Time { return now }

	collectorRuntime.CollectZCodeOnce(context.Background())
	collectorRuntime.CollectCommandCodeOnce(context.Background())
	got := stateStore.Get()
	if got.ZCode.Health.Status != domain.SourceError || got.ZCode.Summary != nil || got.ZCode.Tasks != nil {
		t.Fatalf("unexpected zcode failure state: %+v", got.ZCode)
	}
	if got.CommandCode.Health.Status != domain.SourceError || got.CommandCode.Usage != nil {
		t.Fatalf("unexpected commandCode failure state: %+v", got.CommandCode)
	}
	if got.ZCode.Health.Message == nil || *got.ZCode.Health.Message != "ZCode collection failed" {
		t.Fatalf("unsafe zcode message: %#v", got.ZCode.Health.Message)
	}
	if got.CommandCode.Health.Message == nil || *got.CommandCode.Health.Message != "CommandCode collection failed" {
		t.Fatalf("unsafe commandCode message: %#v", got.CommandCode.Health.Message)
	}
}

func TestLaterFailurePreservesLastKnownGoodAsStale(t *testing.T) {
	base := time.Date(2026, 9, 16, 6, 0, 0, 0, time.UTC)
	now := base
	stateStore := newRuntimeStore(t, base, true, false)
	fail := false
	collectorRuntime := New(
		stateStore,
		func(context.Context) (*zcode.Snapshot, error) {
			if fail {
				return nil, errors.New("ZCode live Goal database read failed")
			}
			return &zcode.Snapshot{
				Summary: domain.ZCodeSummary{Running: 1},
				Tasks: []domain.TaskSummary{{ID: "stable-id", Title: "goal", Status: domain.TaskRunning}},
			}, nil
		},
		nil,
		DefaultConfig(),
	)
	collectorRuntime.now = func() time.Time { return now }

	collectorRuntime.CollectZCodeOnce(context.Background())
	first := stateStore.Get()
	if first.ZCode.Health.Status != domain.SourceOK || first.ZCode.Health.LastSuccessAt == nil {
		t.Fatalf("first state = %+v", first.ZCode)
	}
	firstSuccess := *first.ZCode.Health.LastSuccessAt

	fail = true
	now = base.Add(10 * time.Second)
	collectorRuntime.CollectZCodeOnce(context.Background())
	stale := stateStore.Get()
	if stale.ZCode.Health.Status != domain.SourceStale {
		t.Fatalf("status = %q", stale.ZCode.Health.Status)
	}
	if stale.ZCode.Health.LastSuccessAt == nil || !stale.ZCode.Health.LastSuccessAt.Equal(firstSuccess) {
		t.Fatalf("lastSuccessAt changed: %#v", stale.ZCode.Health.LastSuccessAt)
	}
	if stale.ZCode.Summary == nil || stale.ZCode.Summary.Running != 1 || len(stale.ZCode.Tasks) != 1 || stale.ZCode.Tasks[0].ID != "stable-id" {
		t.Fatalf("last-known-good lost: %+v", stale.ZCode)
	}
	if stale.ZCode.Health.Message == nil || *stale.ZCode.Health.Message != "ZCode live Goal database read failed" {
		t.Fatalf("message = %#v", stale.ZCode.Health.Message)
	}
}

func TestSourceUpdatesDoNotClobberEachOther(t *testing.T) {
	now := time.Date(2026, 9, 16, 6, 0, 0, 0, time.UTC)
	stateStore := newRuntimeStore(t, now, true, true)
	plan := "GOAT"
	collectorRuntime := New(
		stateStore,
		func(context.Context) (*zcode.Snapshot, error) {
			return &zcode.Snapshot{
				Summary: domain.ZCodeSummary{Running: 1},
				Tasks: []domain.TaskSummary{{ID: "goal", Title: "goal", Status: domain.TaskRunning}},
			}, nil
		},
		func(context.Context) (*domain.UsageSummary, error) {
			return &domain.UsageSummary{Plan: &plan, Windows: []domain.UsageWindow{}}, nil
		},
		DefaultConfig(),
	)
	collectorRuntime.now = func() time.Time { return now }
	collectorRuntime.CollectCommandCodeOnce(context.Background())
	collectorRuntime.CollectZCodeOnce(context.Background())

	got := stateStore.Get()
	if got.CommandCode.Usage == nil || got.CommandCode.Usage.Plan == nil || *got.CommandCode.Usage.Plan != "GOAT" {
		t.Fatalf("commandCode update was clobbered: %+v", got.CommandCode)
	}
	if got.ZCode.Summary == nil || got.ZCode.Summary.Running != 1 {
		t.Fatalf("zcode update missing: %+v", got.ZCode)
	}
}

func TestDisabledCollectorRemainsDisabled(t *testing.T) {
	now := time.Date(2026, 9, 16, 6, 0, 0, 0, time.UTC)
	stateStore := newRuntimeStore(t, now, false, false)
	collectorRuntime := New(stateStore, nil, nil, DefaultConfig())
	collectorRuntime.CollectZCodeOnce(context.Background())
	collectorRuntime.CollectCommandCodeOnce(context.Background())
	got := stateStore.Get()
	if got.ZCode.Health.Status != domain.SourceDisabled || got.CommandCode.Health.Status != domain.SourceDisabled {
		t.Fatalf("disabled state changed: z=%s cc=%s", got.ZCode.Health.Status, got.CommandCode.Health.Status)
	}
}

func newRuntimeStore(t *testing.T, now time.Time, zEnabled, ccEnabled bool) *store.SnapshotStore {
	t.Helper()
	stateStore, err := store.New(InitialState(now, "test", zEnabled, ccEnabled))
	if err != nil {
		t.Fatal(err)
	}
	return stateStore
}
