package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	agentruntime "github.com/Eswink/ai-control-hud/agent/internal/runtime"
)

func TestClientUploadsStateAndHeartbeat(t *testing.T) {
	var mu sync.Mutex
	paths := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["agentId"] != "desktop-main" {
			t.Fatalf("unexpected agent id %#v", body["agentId"])
		}
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"accepted"}`))
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
	now := time.Date(2026, 9, 16, 11, 0, 0, 0, time.UTC)
	client.now = func() time.Time { return now }
	state := agentruntime.InitialState(now, "test", false, false)

	if err := client.UploadState(context.Background(), state); err != nil {
		t.Fatalf("upload state: %v", err)
	}
	if err := client.Heartbeat(context.Background(), "0.3.0"); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 2 || paths[0] != "/api/v1/agent/state" || paths[1] != "/api/v1/agent/heartbeat" {
		t.Fatalf("unexpected request paths %#v", paths)
	}
}

func TestClientAutoDiscoveryRediscoverAfterAddressFailure(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer live.Close()

	config, err := NewConfig("auto", "desktop-main", "secret")
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	client.discover = func(context.Context) (string, error) {
		calls++
		if calls == 1 {
			return deadURL, nil
		}
		return live.URL, nil
	}

	if err := client.Heartbeat(context.Background(), "test"); err != nil {
		t.Fatalf("heartbeat after rediscovery: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected two discovery attempts, got %d", calls)
	}
	if got := client.ResolvedBaseURL(); got != live.URL {
		t.Fatalf("unexpected resolved URL %q", got)
	}
}

func TestClientRejectsNonSuccessStatusWithoutEchoingResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "sensitive server diagnostic", http.StatusUnauthorized)
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
	err = client.Heartbeat(context.Background(), "test")
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("expected HTTP 401 error, got %v", err)
	}
	if strings.Contains(err.Error(), "sensitive server diagnostic") {
		t.Fatalf("response body leaked into error: %v", err)
	}
}

func TestFromEnvironmentIsDisabledWhenHubVariablesAreAbsent(t *testing.T) {
	t.Setenv("AI_CONTROL_HUB_URL", "")
	t.Setenv("AI_CONTROL_HUB_TOKEN", "")
	_, enabled, err := FromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("remote hub should be disabled")
	}
}

func TestFromEnvironmentRequiresTokenWhenURLIsConfigured(t *testing.T) {
	t.Setenv("AI_CONTROL_HUB_URL", "https://hub.example.test")
	t.Setenv("AI_CONTROL_HUB_TOKEN", "")
	_, enabled, err := FromEnvironment()
	if err == nil || enabled {
		t.Fatalf("expected invalid partial configuration, enabled=%t err=%v", enabled, err)
	}
}

func TestNewConfigAcceptsAutoDiscoveryAlias(t *testing.T) {
	config, err := NewConfig("auto", "desktop-main", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if config.BaseURL != AutoBaseURL || !config.IsAutoDiscover() {
		t.Fatalf("auto discovery not normalized: %#v", config)
	}
}

func TestBackoffIsBounded(t *testing.T) {
	base := 5 * time.Second
	maximum := 30 * time.Second
	cases := []struct {
		failures int
		want     time.Duration
	}{
		{1, 5 * time.Second},
		{2, 10 * time.Second},
		{3, 20 * time.Second},
		{4, 30 * time.Second},
		{10, 30 * time.Second},
	}
	for _, test := range cases {
		if got := backoff(base, test.failures, maximum); got != test.want {
			t.Fatalf("failures=%d got %s want %s", test.failures, got, test.want)
		}
	}
}
