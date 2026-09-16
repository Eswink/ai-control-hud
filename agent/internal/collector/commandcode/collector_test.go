package commandcode

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testSecret = "cc-test-secret-do-not-log"

func writeProviderConfig(t *testing.T, baseURL, apiKey string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	root := map[string]any{
		"provider": map[string]any{
			"command": map[string]any{
				"name":    "command",
				"kind":    "openai",
				"enabled": true,
				"options": map[string]any{
					"baseURL": baseURL,
					"apiKey":  apiKey,
				},
				"models": map[string]any{
					"deepseek/deepseek-v4.1-flash": map[string]any{},
				},
			},
		},
	}
	data, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCollectsCreditsWindowsAndPlan(t *testing.T) {
	var authorizationHeaders []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorizationHeaders = append(authorizationHeaders, r.Header.Get("Authorization"))
		switch r.URL.Path {
		case creditsPath:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"credits":{"monthlyCredits":53.72,"purchasedCredits":5.0,"freeCredits":0.5},
				"windowLimits":{
					"fiveHour":{"used":11.34,"cap":14.0,"resetAt":1789490000000},
					"weekly":{"used":22.05,"cap":35.0,"resetAt":"2026-09-19T06:00:00Z"}
				}
			}`))
		case subscriptionsPath:
			_, _ = w.Write([]byte(`{"success":true,"data":{"planId":"individual-goat","status":"active"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configPath := writeProviderConfig(t, "https://api.commandcode.ai/provider/v1", testSecret)
	collector := New([]string{configPath})
	collector.BillingBaseURL = server.URL

	usage, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if usage.Plan == nil || *usage.Plan != "GOAT" {
		t.Fatalf("plan = %#v", usage.Plan)
	}
	if usage.Credit == nil || usage.Credit.Remaining == nil {
		t.Fatal("credit missing")
	}
	if math.Abs(*usage.Credit.Remaining-59.22) > 1e-9 {
		t.Fatalf("remaining = %v", *usage.Credit.Remaining)
	}
	if usage.Credit.Limit == nil || *usage.Credit.Limit != 70 {
		t.Fatalf("limit = %#v", usage.Credit.Limit)
	}
	if len(usage.Windows) != 2 {
		t.Fatalf("windows = %#v", usage.Windows)
	}
	if math.Abs(*usage.Windows[0].UsedPercent-81.0) > 1e-9 {
		t.Fatalf("5h percent = %v", *usage.Windows[0].UsedPercent)
	}
	if math.Abs(*usage.Windows[1].UsedPercent-63.0) > 1e-9 {
		t.Fatalf("weekly percent = %v", *usage.Windows[1].UsedPercent)
	}
	if usage.Windows[0].ResetAt == nil || usage.Windows[1].ResetAt == nil {
		t.Fatal("reset timestamps missing")
	}
	if len(authorizationHeaders) != 2 {
		t.Fatalf("requests = %d", len(authorizationHeaders))
	}
	for _, header := range authorizationHeaders {
		if header != "Bearer "+testSecret {
			t.Fatalf("unexpected auth header")
		}
	}
}

func TestSubscriptionFailureKeepsCredits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == creditsPath {
			_, _ = w.Write([]byte(`{"credits":{"monthlyCredits":10}}`))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	collector := New([]string{writeProviderConfig(t, "https://api.commandcode.ai/provider/v1", testSecret)})
	collector.BillingBaseURL = server.URL
	usage, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if usage.Plan != nil {
		t.Fatalf("plan should be absent: %#v", usage.Plan)
	}
	if usage.Credit == nil || usage.Credit.Remaining == nil || *usage.Credit.Remaining != 10 {
		t.Fatalf("unexpected credit: %#v", usage.Credit)
	}
}

func TestCreditsAuthFailureIsNotZeroBalance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	collector := New([]string{writeProviderConfig(t, "https://api.commandcode.ai/provider/v1", testSecret)})
	collector.BillingBaseURL = server.URL
	_, err := collector.Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), testSecret) {
		t.Fatal("secret leaked in error")
	}
}

func TestMalformedCreditsFailExplicitly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"unexpected":true}`))
	}))
	defer server.Close()

	collector := New([]string{writeProviderConfig(t, "https://api.commandcode.ai/provider/v1", testSecret)})
	collector.BillingBaseURL = server.URL
	_, err := collector.Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Unsupported CommandCode credits response") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMissingKeyAndNonOfficialProviderFailClosed(t *testing.T) {
	missingKey := New([]string{writeProviderConfig(t, "https://api.commandcode.ai/provider/v1", "")})
	if _, err := missingKey.Collect(context.Background()); err == nil || !strings.Contains(err.Error(), "API key missing") {
		t.Fatalf("unexpected missing-key error: %v", err)
	}

	nonOfficial := New([]string{writeProviderConfig(t, "http://127.0.0.1:7788/v1", testSecret)})
	nonOfficial.ProviderID = "command"
	if _, err := nonOfficial.Collect(context.Background()); err == nil || !strings.Contains(err.Error(), "endpoint is not verified") {
		t.Fatalf("unexpected endpoint error: %v", err)
	}
}

func TestUnknownPlanRemainsVisibleWithoutInventingLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case creditsPath:
			_, _ = w.Write([]byte(`{"credits":{"monthlyCredits":12}}`))
		case subscriptionsPath:
			_, _ = w.Write([]byte(`{"success":true,"data":{"planId":"future-plan"}}`))
		}
	}))
	defer server.Close()

	collector := New([]string{writeProviderConfig(t, "https://api.commandcode.ai/provider/v1", testSecret)})
	collector.BillingBaseURL = server.URL
	usage, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if usage.Plan == nil || *usage.Plan != "future-plan" {
		t.Fatalf("plan = %#v", usage.Plan)
	}
	if usage.Credit == nil || usage.Credit.Limit != nil {
		t.Fatalf("limit should remain unknown: %#v", usage.Credit)
	}
}
