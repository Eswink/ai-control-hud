package hub

import (
	"context"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
	"github.com/Eswink/ai-control-hud/agent/internal/events"
)

func TestEventRetentionAgeKeepsMinimumAndUsesBatches(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "hub.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 8; i++ {
		received := now.Add(-48 * time.Hour)
		if i >= 7 {
			received = now.Add(-time.Hour)
		}
		if err := recordRetentionEvent(t.Context(), store, "desktop-main", i, received); err != nil {
			t.Fatal(err)
		}
	}

	result, err := store.PruneEvents(context.Background(), now, RetentionPolicy{
		MaxAge:    24 * time.Hour,
		MinEvents: 3,
		MaxEvents: 100,
		BatchSize: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Deleted != 5 || result.Agents != 1 {
		t.Fatalf("unexpected prune result %#v", result)
	}
	oldest, latest, count, err := store.EventBounds(context.Background(), "desktop-main")
	if err != nil {
		t.Fatal(err)
	}
	if oldest != 6 || latest != 8 || count != 3 {
		t.Fatalf("unexpected retained bounds oldest=%d latest=%d count=%d", oldest, latest, count)
	}
}

func TestEventRetentionMaximumOverridesAge(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 7; i++ {
		if err := recordRetentionEvent(t.Context(), store, "desktop-main", i, now.Add(-time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	result, err := store.PruneEvents(context.Background(), now, RetentionPolicy{
		MaxAge:    90 * 24 * time.Hour,
		MinEvents: 3,
		MaxEvents: 5,
		BatchSize: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Deleted != 2 {
		t.Fatalf("unexpected maximum prune result %#v", result)
	}
	oldest, latest, count, err := store.EventBounds(context.Background(), "desktop-main")
	if err != nil {
		t.Fatal(err)
	}
	if oldest != 3 || latest != 7 || count != 5 {
		t.Fatalf("unexpected maximum retained bounds oldest=%d latest=%d count=%d", oldest, latest, count)
	}
}

func TestEventAPIExposesRetentionFloor(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 5; i++ {
		if err := recordRetentionEvent(t.Context(), store, "desktop-main", i, now.Add(-48*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.PruneEvents(context.Background(), now, RetentionPolicy{
		MaxAge:    24 * time.Hour,
		MinEvents: 2,
		MaxEvents: 100,
		BatchSize: 1,
	}); err != nil {
		t.Fatal(err)
	}

	server, err := NewServer(testConfig(":memory:"), store, "retention-test", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	var page EventPage
	getJSON(t, httpServer.URL+"/api/v1/events?after=0&limit=100", &page)
	if page.OldestSeq != 4 || page.LatestSeq != 5 || page.NextAfter != 5 || len(page.Events) != 2 {
		t.Fatalf("unexpected retention-aware page %#v", page)
	}
}

func TestRetentionConfigEnvironmentAndLegacyDefaults(t *testing.T) {
	t.Setenv("HUD_HUB_DB", filepath.Join(t.TempDir(), "hub.sqlite3"))
	t.Setenv("HUD_HUB_AGENT_ID", "desktop-main")
	t.Setenv("HUD_HUB_AGENT_TOKEN", "secret")
	t.Setenv("HUD_HUB_EVENT_RETENTION_DAYS", "30")
	t.Setenv("HUD_HUB_EVENT_RETENTION_MIN", "0")
	t.Setenv("HUD_HUB_EVENT_RETENTION_MAX", "5000")
	t.Setenv("HUD_HUB_MAINTENANCE_INTERVAL_HOURS", "2")
	config, err := FromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if config.EventRetentionDays != 30 || config.EventRetentionMin != 0 || config.EventRetentionMax != 5000 || config.MaintenanceInterval != 2*time.Hour {
		t.Fatalf("unexpected retention config %#v", config)
	}

	legacy := testConfig(":memory:")
	if legacy.EventRetentionDays != 0 || legacy.EventRetentionMax != 0 {
		t.Fatalf("test precondition changed: %#v", legacy)
	}
	if err := legacy.Validate(); err != nil {
		t.Fatalf("legacy programmatic config should receive default retention: %v", err)
	}
	policy := RetentionPolicyFromConfig(legacy)
	expectedAge := time.Duration(DefaultEventRetentionDays) * 24 * time.Hour
	if policy.MaxAge != expectedAge || policy.MinEvents != DefaultEventRetentionMin || policy.MaxEvents != DefaultEventRetentionMax {
		t.Fatalf("unexpected legacy retention policy %#v", policy)
	}
}

func TestMaintenanceWorkerStopsWithContext(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	config := testConfig(":memory:")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunMaintenance(ctx, store, config, nil)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("maintenance worker did not stop after context cancellation")
	}
}

func recordRetentionEvent(ctx context.Context, store *Store, agentID string, index int, receivedAt time.Time) error {
	event := events.Event{
		EventID:    fmt.Sprintf("retention-event-%04d", index),
		Type:       events.TaskCompleted,
		OccurredAt: receivedAt.Add(-time.Second),
		Task: events.Task{
			ID:     fmt.Sprintf("retention-task-%04d", index),
			Title:  fmt.Sprintf("Retention task %d", index),
			Status: domain.TaskCompleted,
		},
	}
	_, _, err := store.RecordEvents(ctx, agentID, receivedAt, []events.Event{event}, receivedAt)
	return err
}
