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
)

func TestPrimaryStateCacheReturnsDeepClone(t *testing.T) {
	now := time.Date(2026, 9, 17, 13, 0, 0, 0, time.UTC)
	state := healthyState(now)
	workspace := "repo"
	activity := "go test ./..."
	duration := 42
	additions := 7
	deletions := 3
	state.ZCode.Summary.Running = 1
	state.ZCode.Tasks = []domain.TaskSummary{{
		ID:              "task-1",
		Title:           "Cached task",
		Workspace:       &workspace,
		Status:          domain.TaskRunning,
		DurationSeconds: &duration,
		Activity:        &activity,
		Changes:         &domain.TaskChanges{Additions: &additions, Deletions: &deletions},
	}}
	used := 25.0
	reset := now.Add(time.Hour)
	state.CommandCode.Usage.Windows = []domain.UsageWindow{{Name: "5h", UsedPercent: &used, ResetAt: &reset}}

	var cache primaryStateCache
	cache.store(state, now)
	first, _, ok := cache.load()
	if !ok {
		t.Fatal("cache unexpectedly empty")
	}
	first.ZCode.Summary.Running = 99
	first.ZCode.Tasks[0].Title = "mutated"
	*first.ZCode.Tasks[0].Workspace = "mutated-workspace"
	*first.ZCode.Tasks[0].Changes.Additions = 999
	*first.CommandCode.Usage.Plan = "mutated-plan"
	*first.CommandCode.Usage.Credit.Remaining = 1
	*first.CommandCode.Usage.Windows[0].UsedPercent = 99

	second, _, ok := cache.load()
	if !ok {
		t.Fatal("cache unexpectedly empty after first load")
	}
	if second.ZCode.Summary.Running != 1 || second.ZCode.Tasks[0].Title != "Cached task" || *second.ZCode.Tasks[0].Workspace != "repo" {
		t.Fatalf("cached ZCode state was mutated: %#v", second.ZCode)
	}
	if *second.ZCode.Tasks[0].Changes.Additions != 7 || *second.CommandCode.Usage.Credit.Remaining != 50 || *second.CommandCode.Usage.Windows[0].UsedPercent != 25 {
		t.Fatalf("cached nested values were mutated: %#v", second)
	}
	if *second.CommandCode.Usage.Plan != "test" {
		t.Fatalf("cached plan was mutated: %q", *second.CommandCode.Usage.Plan)
	}
}

func TestPrimaryStateCacheHeartbeatUpdatesLastSeen(t *testing.T) {
	now := time.Date(2026, 9, 17, 13, 0, 0, 0, time.UTC)
	var cache primaryStateCache
	cache.store(healthyState(now), now)
	later := now.Add(30 * time.Second)
	cache.heartbeat(later)
	_, lastSeen, ok := cache.load()
	if !ok || !lastSeen.Equal(later) {
		t.Fatalf("heartbeat did not update cache lastSeen=%v ok=%t", lastSeen, ok)
	}
}

func TestServerSeedsCacheFromSQLiteThenServesWithoutDatabaseRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.sqlite3")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 13, 0, 0, 0, time.UTC)
	if err := store.RecordState(t.Context(), "desktop-main", now, healthyState(now), now); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(testConfig(path), store, "cache-test", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	var first domain.HudState
	getJSON(t, httpServer.URL+"/api/v1/state", &first)
	if first.Server.Version != "cache-test" {
		t.Fatalf("unexpected first cached state %#v", first)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	// A second read still succeeds because the primary snapshot was seeded into
	// the bounded cache by the first request. This test intentionally closes the
	// underlying DB to prove the steady-state read path no longer depends on it.
	var second domain.HudState
	getJSON(t, httpServer.URL+"/api/v1/state", &second)
	if second.Overall.Status != domain.OverallLive || second.Server.Version != "cache-test" {
		t.Fatalf("unexpected cache-only state %#v", second)
	}
}

func TestPrimaryStatePOSTUpdatesCacheAfterCommit(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 17, 13, 0, 0, 0, time.UTC)
	config := testConfig(":memory:")
	server, err := NewServer(config, store, "cache-post-test", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	envelope := StateEnvelope{AgentID: config.PrimaryAgentID, SentAt: now, State: healthyState(now)}
	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, httpServer.URL+"/api/v1/agent/state", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+config.AgentToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("state POST status=%d", response.StatusCode)
	}
	cached, lastSeen, ok := server.stateCache.load()
	if !ok || !lastSeen.Equal(now) || cached.SchemaVersion != domain.SchemaVersion {
		t.Fatalf("primary POST did not seed cache ok=%t lastSeen=%v state=%#v", ok, lastSeen, cached)
	}
}
