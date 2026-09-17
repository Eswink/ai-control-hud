package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apihttp "github.com/Eswink/ai-control-hud/agent/internal/api"
	"github.com/Eswink/ai-control-hud/agent/internal/domain"
	"github.com/Eswink/ai-control-hud/agent/internal/mock"
	agentruntime "github.com/Eswink/ai-control-hud/agent/internal/runtime"
	"github.com/Eswink/ai-control-hud/agent/internal/store"
)

func TestStateHealthAndDiagnosticsEndpointsPreserveContracts(t *testing.T) {
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

	diagnosticsResponse, err := http.Get(server.URL + "/api/v1/diagnostics")
	if err != nil {
		t.Fatal(err)
	}
	defer diagnosticsResponse.Body.Close()
	if diagnosticsResponse.StatusCode != http.StatusOK {
		t.Fatalf("diagnostics status = %d", diagnosticsResponse.StatusCode)
	}
	var diagnostics domain.DiagnosticsResponse
	if err := json.NewDecoder(diagnosticsResponse.Body).Decode(&diagnostics); err != nil {
		t.Fatal(err)
	}
	if diagnostics.DiagnosticsVersion != 1 || diagnostics.StateSchemaVersion != 1 {
		t.Fatalf("unexpected diagnostics versions: %+v", diagnostics)
	}
	if diagnostics.Role != "agent" || diagnostics.Version != "go-test" {
		t.Fatalf("unexpected diagnostics identity: role=%q version=%q", diagnostics.Role, diagnostics.Version)
	}
	if diagnostics.UptimeSeconds < 2 {
		t.Fatalf("diagnostics uptime too small: %d", diagnostics.UptimeSeconds)
	}
	if !diagnostics.Sources.ZCode.Enabled || diagnostics.Sources.ZCode.AdapterKind != "zcode.sqlite" {
		t.Fatalf("unexpected zcode diagnostics: %+v", diagnostics.Sources.ZCode)
	}
	if diagnostics.Sources.ZCode.SchemaSupport != domain.SchemaSupported || diagnostics.Sources.ZCode.LastSuccessAgeSeconds == nil {
		t.Fatalf("unexpected zcode schema diagnostics: %+v", diagnostics.Sources.ZCode)
	}
	if !diagnostics.Sources.CommandCode.Enabled || diagnostics.Sources.CommandCode.AdapterKind != "commandcode.provider-http" {
		t.Fatalf("unexpected commandcode diagnostics: %+v", diagnostics.Sources.CommandCode)
	}
	if diagnostics.Sources.CommandCode.SchemaSupport != domain.SchemaSupported || diagnostics.Sources.CommandCode.LastSuccessAgeSeconds == nil {
		t.Fatalf("unexpected commandcode schema diagnostics: %+v", diagnostics.Sources.CommandCode)
	}
}

func TestDiagnosticsDoesNotExposeSourceErrorTextOrPaths(t *testing.T) {
	now := time.Now().UTC()
	state := agentruntime.InitialState(now, "go-test", true, false)
	privateError := `Unsupported ZCode task index schema at C:\Users\private\workspace\tasks-index.sqlite`
	state.ZCode.Health = domain.SourceHealth{
		Status:     domain.SourceError,
		ObservedAt: now,
		Message:    &privateError,
	}
	stateStore, err := store.New(state)
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(apihttp.New(stateStore, "go-test", now.Add(-time.Second)).Handler())
	defer server.Close()

	response, err := http.Get(server.URL + "/api/v1/diagnostics")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("diagnostics status = %d body=%s", response.StatusCode, body)
	}
	if strings.Contains(string(body), "Users") || strings.Contains(string(body), "Unsupported ZCode") {
		t.Fatalf("diagnostics leaked source error text: %s", body)
	}

	var diagnostics domain.DiagnosticsResponse
	if err := json.Unmarshal(body, &diagnostics); err != nil {
		t.Fatal(err)
	}
	if !diagnostics.Sources.ZCode.Enabled || diagnostics.Sources.ZCode.Status != domain.SourceError {
		t.Fatalf("unexpected zcode diagnostics: %+v", diagnostics.Sources.ZCode)
	}
	if diagnostics.Sources.ZCode.SchemaSupport != domain.SchemaUnsupported {
		t.Fatalf("zcode schema support = %q", diagnostics.Sources.ZCode.SchemaSupport)
	}
	if diagnostics.Sources.ZCode.LastSuccessAgeSeconds != nil {
		t.Fatalf("unexpected zcode last-success age: %v", *diagnostics.Sources.ZCode.LastSuccessAgeSeconds)
	}
	if diagnostics.Sources.CommandCode.Enabled {
		t.Fatalf("commandcode unexpectedly enabled: %+v", diagnostics.Sources.CommandCode)
	}
	if diagnostics.Sources.CommandCode.SchemaSupport != domain.SchemaNotConfigured {
		t.Fatalf("commandcode schema support = %q", diagnostics.Sources.CommandCode.SchemaSupport)
	}
}
