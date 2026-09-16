package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	apihttp "github.com/Eswink/ai-control-hud/agent/internal/api"
	"github.com/Eswink/ai-control-hud/agent/internal/domain"
	"github.com/Eswink/ai-control-hud/agent/internal/mock"
	"github.com/Eswink/ai-control-hud/agent/internal/store"
)

func TestStateAndHealthEndpointsPreserveSchemaV1(t *testing.T) {
	fixturePath := filepath.Join("..", "..", "..", "server", "tests", "fixtures", "healthy.json")
	fixture, err := mock.LoadFixture(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	stateStore, err := store.New(fixture)
	if err != nil {
		t.Fatal(err)
	}

	started := time.Now().UTC().Add(-3 * time.Second)
	server := httptest.NewServer(apihttp.New(stateStore, "go-test", started).Handler())
	defer server.Close()

	stateResponse, err := http.Get(server.URL + "/api/v1/state")
	if err != nil {
		t.Fatal(err)
	}
	defer stateResponse.Body.Close()
	if stateResponse.StatusCode != http.StatusOK {
		t.Fatalf("state status = %d", stateResponse.StatusCode)
	}
	var state domain.HudState
	if err := json.NewDecoder(stateResponse.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if err := state.Validate(); err != nil {
		t.Fatalf("state validation: %v", err)
	}
	if state.SchemaVersion != 1 || state.Server.Version != "go-test" {
		t.Fatalf("unexpected state metadata: schema=%d version=%q", state.SchemaVersion, state.Server.Version)
	}
	if state.Overall.Status != domain.OverallLive {
		t.Fatalf("overall status = %q", state.Overall.Status)
	}
	if state.Server.UptimeSeconds < 2 {
		t.Fatalf("uptime too small: %d", state.Server.UptimeSeconds)
	}

	healthResponse, err := http.Get(server.URL + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer healthResponse.Body.Close()
	if healthResponse.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", healthResponse.StatusCode)
	}
	var health domain.HealthResponse
	if err := json.NewDecoder(healthResponse.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if health.Status != "ok" || health.SchemaVersion != 1 {
		t.Fatalf("unexpected health envelope: %+v", health)
	}
	if health.Sources.ZCode != domain.SourceOK || health.Sources.CommandCode != domain.SourceOK {
		t.Fatalf("unexpected source health: %+v", health.Sources)
	}
}
