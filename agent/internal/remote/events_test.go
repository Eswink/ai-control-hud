package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
	"github.com/Eswink/ai-control-hud/agent/internal/events"
)

func TestClientUploadsTaskEvents(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agent/events" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		var body struct {
			AgentID string         `json:"agentId"`
			SentAt  time.Time      `json:"sentAt"`
			Events  []events.Event `json:"events"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode event request: %v", err)
		}
		if body.AgentID != "desktop-main" {
			t.Fatalf("agentId=%q", body.AgentID)
		}
		if !body.SentAt.Equal(now) {
			t.Fatalf("sentAt=%s want %s", body.SentAt, now)
		}
		if len(body.Events) != 1 {
			t.Fatalf("events=%d want 1", len(body.Events))
		}
		event := body.Events[0]
		if event.EventID != "0123456789abcdef0123456789abcdef" ||
			event.Type != events.TaskCompleted ||
			event.Task.ID != "task-1" ||
			event.Task.Status != domain.TaskCompleted {
			t.Fatalf("unexpected event %#v", event)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"accepted","accepted":1,"duplicates":0}`))
	}))
	defer server.Close()

	config, err := NewConfig(server.URL, "desktop-main", "secret")
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	client.now = func() time.Time { return now }
	workspace := "backend"
	event := events.Event{
		EventID:    "0123456789abcdef0123456789abcdef",
		Type:       events.TaskCompleted,
		OccurredAt: now,
		Task: events.Task{
			ID:        "task-1",
			Title:     "Refactor agent pipeline",
			Workspace: &workspace,
			Status:    domain.TaskCompleted,
		},
	}
	if err := client.UploadEvents(context.Background(), []events.Event{event}); err != nil {
		t.Fatalf("upload events: %v", err)
	}
}

func TestClientRejectsOversizedEventBatch(t *testing.T) {
	config, err := NewConfig("https://hub.example.test", "desktop-main", "secret")
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	batch := make([]events.Event, 101)
	if err := client.UploadEvents(context.Background(), batch); err == nil {
		t.Fatal("expected oversized event batch to be rejected")
	}
}
