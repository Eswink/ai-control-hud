package hub

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
	"github.com/Eswink/ai-control-hud/agent/internal/events"
)

func TestHeartbeatRefreshesFreshnessWithoutReplacingSnapshot(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "hub.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 16, 11, 0, 0, 0, time.UTC)
	clock := now
	config := testConfig(filepath.Join(t.TempDir(), "unused.sqlite3"))
	config.DatabasePath = ":memory:"
	server, err := NewServer(config, store, "go-hub-test", func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	state := healthyState(now)
	if code := postJSON(t, httpServer.URL+"/api/v1/agent/state", StateEnvelope{
		AgentID: config.PrimaryAgentID,
		SentAt:  now,
		State:   state,
	}, config.AgentToken); code != http.StatusOK {
		t.Fatalf("state ingest status=%d", code)
	}

	clock = now.Add(50 * time.Second)
	var stale domain.HudState
	getJSON(t, httpServer.URL+"/api/v1/state", &stale)
	if stale.Overall.Status != domain.OverallDegraded {
		t.Fatalf("expected stale state, got %#v", stale.Overall)
	}

	version := "0.3.0"
	if code := postJSON(t, httpServer.URL+"/api/v1/agent/heartbeat", Heartbeat{
		AgentID:      config.PrimaryAgentID,
		SentAt:       clock,
		AgentVersion: &version,
	}, config.AgentToken); code != http.StatusOK {
		t.Fatalf("heartbeat status=%d", code)
	}
	var fresh domain.HudState
	getJSON(t, httpServer.URL+"/api/v1/state", &fresh)
	if fresh.Overall.Status != domain.OverallLive || fresh.ZCode.Health.Status != domain.SourceOK || fresh.CommandCode.Health.Status != domain.SourceOK {
		t.Fatalf("heartbeat did not restore freshness: %#v", fresh)
	}
}

func TestEventFeedFiltersToPrimaryAgentAndKeepsGlobalSeq(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	config := testConfig(":memory:")
	server, err := NewServer(config, store, "go-hub-test", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	other := EventBatch{AgentID: "other-machine", SentAt: now, Events: []events.Event{completedEvent("other-0001", "other-task", now)}}
	primary := EventBatch{AgentID: config.PrimaryAgentID, SentAt: now, Events: []events.Event{completedEvent("primary-01", "primary-task", now)}}
	if code := postJSON(t, httpServer.URL+"/api/v1/agent/events", other, config.AgentToken); code != http.StatusOK {
		t.Fatalf("other event status=%d", code)
	}
	if code := postJSON(t, httpServer.URL+"/api/v1/agent/events", primary, config.AgentToken); code != http.StatusOK {
		t.Fatalf("primary event status=%d", code)
	}
	var page EventPage
	getJSON(t, httpServer.URL+"/api/v1/events", &page)
	if len(page.Events) != 1 || page.Events[0].EventID != "primary-01" || page.Events[0].AgentID != config.PrimaryAgentID {
		t.Fatalf("unexpected primary event feed %#v", page)
	}
	if page.Events[0].Seq != 2 || page.LatestSeq != 2 || page.NextAfter != 2 {
		t.Fatalf("global sequence parity mismatch %#v", page)
	}
}

func TestEventTypeMustAgreeWithTerminalStatus(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	config := testConfig(":memory:")
	server, err := NewServer(config, store, "go-hub-test", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	invalid := completedEvent("event-0001", "task-1", now)
	invalid.Task.Status = domain.TaskFailed
	body, err := json.Marshal(EventBatch{AgentID: config.PrimaryAgentID, SentAt: now, Events: []events.Event{invalid}})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, httpServer.URL+"/api/v1/agent/events", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+config.AgentToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("invalid event status=%d", response.StatusCode)
	}
}

func completedEvent(eventID, taskID string, when time.Time) events.Event {
	return events.Event{
		EventID:    eventID,
		Type:       events.TaskCompleted,
		OccurredAt: when,
		Task: events.Task{
			ID:     taskID,
			Title:  taskID,
			Status: domain.TaskCompleted,
		},
	}
}
