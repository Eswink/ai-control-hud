package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
	"github.com/Eswink/ai-control-hud/agent/internal/events"
)

func TestGoHubStateEventsStaleAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.sqlite3")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 16, 14, 30, 0, 0, time.UTC)
	clock := now
	config := testConfig(path)
	server, err := NewServer(config, store, "go-hub-test", func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	state := healthyState(now)
	envelope := StateEnvelope{AgentID: config.PrimaryAgentID, SentAt: now, State: state}
	if code := postJSON(t, httpServer.URL+"/api/v1/agent/state", envelope, "wrong"); code != http.StatusUnauthorized {
		t.Fatalf("wrong-token state status=%d", code)
	}
	if code := postJSON(t, httpServer.URL+"/api/v1/agent/state", envelope, config.AgentToken); code != http.StatusOK {
		t.Fatalf("state ingest status=%d", code)
	}

	var live domain.HudState
	getJSON(t, httpServer.URL+"/api/v1/state", &live)
	if live.Overall.Status != domain.OverallLive || live.Server.Version != "go-hub-test" {
		t.Fatalf("unexpected live projection %#v", live)
	}

	event := events.Event{
		EventID:    "event-0001",
		Type:       events.TaskCompleted,
		OccurredAt: now.Add(time.Second),
		Task: events.Task{
			ID:     "task-1",
			Title:  "Build Hub",
			Status: domain.TaskCompleted,
		},
	}
	batch := EventBatch{AgentID: config.PrimaryAgentID, SentAt: now.Add(time.Second), Events: []events.Event{event}}
	var first EventReceipt
	postJSONInto(t, httpServer.URL+"/api/v1/agent/events", batch, config.AgentToken, &first)
	if first.Accepted != 1 || first.Duplicates != 0 {
		t.Fatalf("first event receipt %#v", first)
	}
	var second EventReceipt
	postJSONInto(t, httpServer.URL+"/api/v1/agent/events", batch, config.AgentToken, &second)
	if second.Accepted != 0 || second.Duplicates != 1 {
		t.Fatalf("duplicate event receipt %#v", second)
	}
	var page EventPage
	getJSON(t, httpServer.URL+"/api/v1/events?after=0&limit=100", &page)
	if page.SchemaVersion != 1 || len(page.Events) != 1 || page.NextAfter != 1 || page.LatestSeq != 1 {
		t.Fatalf("event page %#v", page)
	}
	var resetPage EventPage
	getJSON(t, httpServer.URL+"/api/v1/events?after=99&limit=1", &resetPage)
	if len(resetPage.Events) != 0 || resetPage.NextAfter != 99 || resetPage.LatestSeq != 1 {
		t.Fatalf("reset page %#v", resetPage)
	}

	clock = now.Add(46 * time.Second)
	var stale domain.HudState
	getJSON(t, httpServer.URL+"/api/v1/state", &stale)
	if stale.Overall.Status != domain.OverallDegraded || stale.ZCode.Health.Status != domain.SourceStale || stale.CommandCode.Health.Status != domain.SourceStale {
		t.Fatalf("unexpected stale projection %#v", stale)
	}

	httpServer.Close()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	loaded, _, found, err := reopened.LoadState(context.Background(), config.PrimaryAgentID)
	if err != nil || !found || loaded.SchemaVersion != domain.SchemaVersion {
		t.Fatalf("reopened state found=%t err=%v state=%#v", found, err, loaded)
	}
	items, latest, err := reopened.ListEvents(context.Background(), config.PrimaryAgentID, 0, 100)
	if err != nil || len(items) != 1 || latest != 1 {
		t.Fatalf("reopened events len=%d latest=%d err=%v", len(items), latest, err)
	}
}

func TestGoHubHealthBeforeFirstSnapshot(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	config := testConfig(":memory:")
	now := time.Date(2026, 9, 16, 14, 30, 0, 0, time.UTC)
	server, err := NewServer(config, store, "test", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	var health domain.HealthResponse
	getJSON(t, httpServer.URL+"/api/v1/health", &health)
	if health.Status != "ok" || health.Sources.ZCode != domain.SourceError || health.Sources.CommandCode != domain.SourceError {
		t.Fatalf("unexpected empty health %#v", health)
	}
	response, err := http.Get(httpServer.URL + "/api/v1/state")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("empty state status=%d", response.StatusCode)
	}
}

func TestGoHubOnlineBackupWhileStoreOpen(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "hub.sqlite3")
	destination := filepath.Join(dir, "backups", "hub-backup.sqlite3")
	store, err := OpenStore(source)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 16, 14, 30, 0, 0, time.UTC)
	state := healthyState(now)
	if err := store.RecordState(context.Background(), "desktop-main", now, state, now); err != nil {
		t.Fatal(err)
	}
	if err := BackupDatabase(context.Background(), source, destination); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("backup is empty")
	}
	backup, err := OpenStore(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	loaded, _, found, err := backup.LoadState(context.Background(), "desktop-main")
	if err != nil || !found || loaded.Overall.Status != domain.OverallLive {
		t.Fatalf("backup state found=%t err=%v state=%#v", found, err, loaded)
	}
	if err := BackupDatabase(context.Background(), source, destination); err == nil {
		t.Fatal("backup unexpectedly overwrote existing destination")
	}
}

func TestDiscoveryResponderAdvertisesHTTPPort(t *testing.T) {
	probe, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	port := probe.LocalAddr().(*net.UDPAddr).Port
	_ = probe.Close()
	config := testConfig(":memory:")
	config.DiscoveryEnabled = true
	config.DiscoveryPort = port
	config.HTTPPort = 8787
	config.HubID = "dorm-hub"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	responder, err := StartDiscovery(ctx, config, "go-hub-test")
	if err != nil {
		t.Fatal(err)
	}
	defer responder.Close()

	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.WriteToUDP([]byte(discoveryProbe), &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port}); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	buffer := make([]byte, 1024)
	n, _, err := client.ReadFromUDP(buffer)
	if err != nil {
		t.Fatal(err)
	}
	var announcement DiscoveryAnnouncement
	if err := json.Unmarshal(buffer[:n], &announcement); err != nil {
		t.Fatal(err)
	}
	if announcement.HubID != "dorm-hub" || announcement.HTTPPort != 8787 || announcement.HubVersion != "go-hub-test" {
		t.Fatalf("unexpected discovery announcement %#v", announcement)
	}
}

func TestConfigFromEnvironment(t *testing.T) {
	t.Setenv("HUD_HUB_DB", filepath.Join(t.TempDir(), "hub.sqlite3"))
	t.Setenv("HUD_HUB_AGENT_ID", "desktop-main")
	t.Setenv("HUD_HUB_AGENT_TOKEN", "secret")
	t.Setenv("HUD_HUB_STALE_AFTER_SECONDS", "45")
	t.Setenv("HUD_HUB_ID", "dorm-hub")
	t.Setenv("HUD_HUB_DISCOVERY_ENABLED", "true")
	t.Setenv("HUD_HUB_DISCOVERY_PORT", "8788")
	t.Setenv("HUD_HUB_HTTP_SCHEME", "http")
	t.Setenv("HUD_HUB_HTTP_PORT", "8787")
	config, err := FromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if !config.DiscoveryEnabled || config.HubID != "dorm-hub" || config.HTTPPort != 8787 {
		t.Fatalf("unexpected config %#v", config)
	}
}

func testConfig(path string) Config {
	return Config{
		DatabasePath:     path,
		PrimaryAgentID:   "desktop-main",
		AgentToken:       "secret-token",
		StaleAfter:       45 * time.Second,
		HubID:            "central-hub",
		DiscoveryEnabled: false,
		DiscoveryPort:    8788,
		HTTPScheme:       "http",
		HTTPPort:         8787,
	}
}

func healthyState(now time.Time) domain.HudState {
	lastSuccess := now
	plan := "test"
	remaining := 50.0
	limit := 100.0
	unit := "credits"
	return domain.HudState{
		SchemaVersion: domain.SchemaVersion,
		Server: domain.ServerInfo{Version: "agent-test", Time: now, UptimeSeconds: 10},
		Overall: domain.OverallStatus{Status: domain.OverallLive},
		ZCode: domain.ZCodeState{
			Health: domain.SourceHealth{Status: domain.SourceOK, ObservedAt: now, LastSuccessAt: &lastSuccess},
			Summary: &domain.ZCodeSummary{},
			Tasks:   []domain.TaskSummary{},
		},
		CommandCode: domain.CommandCodeState{
			Health: domain.SourceHealth{Status: domain.SourceOK, ObservedAt: now, LastSuccessAt: &lastSuccess},
			Usage: &domain.UsageSummary{
				Plan: &plan,
				Credit: &domain.CreditBalance{Remaining: &remaining, Limit: &limit, Unit: &unit},
				Windows: []domain.UsageWindow{},
			},
		},
	}
}

func postJSON(t *testing.T, url string, value any, token string) int {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	return response.StatusCode
}

func postJSONInto(t *testing.T, url string, value any, token string, output any) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST %s status=%d", url, response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(output); err != nil {
		t.Fatal(err)
	}
}

func getJSON(t *testing.T, url string, output any) {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status=%d", url, response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(output); err != nil {
		t.Fatal(err)
	}
}

func ExampleConfig() {
	config := testConfig("/var/lib/ai-control-hud/hub.sqlite3")
	fmt.Println(config.PrimaryAgentID, config.HTTPPort)
	// Output: desktop-main 8787
}
