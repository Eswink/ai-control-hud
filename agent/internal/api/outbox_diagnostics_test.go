package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	apihttp "github.com/Eswink/ai-control-hud/agent/internal/api"
	"github.com/Eswink/ai-control-hud/agent/internal/domain"
	agentruntime "github.com/Eswink/ai-control-hud/agent/internal/runtime"
	"github.com/Eswink/ai-control-hud/agent/internal/store"
)

func TestDiagnosticsIncludesOptionalSanitizedOutboxMetadata(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	state := agentruntime.InitialState(now, "test", false, false)
	stateStore, err := store.New(state)
	if err != nil {
		t.Fatal(err)
	}

	server := apihttp.New(stateStore, "go-test", now.Add(-time.Minute))
	age := int64(90000)
	server.SetOutboxDiagnosticsProvider(func(context.Context, time.Time) domain.OutboxDiagnostics {
		return domain.OutboxDiagnostics{
			Status:                  "warning",
			PendingEvents:           12,
			TaskBaselineRows:        500,
			CompactedTaskRows:       450,
			OldestPendingAgeSeconds: &age,
			ReusableBytes:           32768,
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
	if diagnostics.Outbox == nil {
		t.Fatal("outbox diagnostics missing")
	}
	if diagnostics.Outbox.Status != "warning" || diagnostics.Outbox.PendingEvents != 12 || diagnostics.Outbox.TaskBaselineRows != 500 || diagnostics.Outbox.CompactedTaskRows != 450 {
		t.Fatalf("unexpected outbox diagnostics %#v", diagnostics.Outbox)
	}
	if diagnostics.Outbox.OldestPendingAgeSeconds == nil || *diagnostics.Outbox.OldestPendingAgeSeconds != age || diagnostics.Outbox.ReusableBytes != 32768 {
		t.Fatalf("unexpected outbox ages/storage %#v", diagnostics.Outbox)
	}
}

func TestDiagnosticsOmitsOutboxWhenRemoteUploadIsNotConfigured(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	state := agentruntime.InitialState(now, "test", false, false)
	stateStore, err := store.New(state)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(apihttp.New(stateStore, "go-test", now).Handler())
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
	if diagnostics.Outbox != nil {
		t.Fatalf("unexpected outbox diagnostics %#v", diagnostics.Outbox)
	}
}
