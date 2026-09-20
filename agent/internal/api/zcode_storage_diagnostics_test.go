package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apihttp "github.com/Eswink/ai-control-hud/agent/internal/api"
	"github.com/Eswink/ai-control-hud/agent/internal/domain"
	agentruntime "github.com/Eswink/ai-control-hud/agent/internal/runtime"
	"github.com/Eswink/ai-control-hud/agent/internal/store"
)

func TestDiagnosticsIncludesPathFreeZCodeStorageMetadata(t *testing.T) {
	now := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	state := agentruntime.InitialState(now, "test", true, false)
	stateStore, err := store.New(state)
	if err != nil {
		t.Fatal(err)
	}

	server := apihttp.New(stateStore, "go-test", now.Add(-time.Minute))
	server.SetZCodeStorageDiagnosticsProvider(func() domain.ZCodeStorageDiagnostics {
		return domain.ZCodeStorageDiagnostics{
			BindingMode:             "machine-config",
			LayoutSource:            "machine-config",
			RuntimeDatabaseReadable: false,
			TaskIndexReadable:       true,
			TurnLogReadable:         false,
			RefreshRecommended:      true,
		}
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	response, err := http.Get(httpServer.URL + "/api/v1/diagnostics")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	var diagnostics domain.DiagnosticsResponse
	if err := json.NewDecoder(response.Body).Decode(&diagnostics); err != nil {
		t.Fatal(err)
	}
	if diagnostics.ZCodeStorage == nil {
		t.Fatal("zcode storage diagnostics missing")
	}
	got := diagnostics.ZCodeStorage
	if got.BindingMode != "machine-config" || got.LayoutSource != "machine-config" ||
		got.RuntimeDatabaseReadable || !got.TaskIndexReadable || got.TurnLogReadable ||
		!got.RefreshRecommended {
		t.Fatalf("unexpected zcode storage diagnostics: %#v", got)
	}

	serialized, err := json.Marshal(diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serialized), "Users") || strings.Contains(string(serialized), "C:\\\\") {
		t.Fatalf("zcode storage diagnostics leaked a path: %s", serialized)
	}
}
