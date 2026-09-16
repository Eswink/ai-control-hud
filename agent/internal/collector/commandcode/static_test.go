package commandcode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCollectProviderDoesNotRequireConfigFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case creditsPath:
			_, _ = w.Write([]byte(`{"credits":{"monthlyCredits":42}}`))
		case subscriptionsPath:
			_, _ = w.Write([]byte(`{"success":true,"data":{"planId":"individual-goat"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	collector := New(nil)
	collector.BillingBaseURL = server.URL
	provider := &Provider{
		ID:      "command",
		Enabled: true,
		BaseURL: "https://api.commandcode.ai/provider/v1",
		APIKey:  testSecret,
	}
	usage, err := collector.CollectProvider(context.Background(), provider)
	if err != nil {
		t.Fatal(err)
	}
	if usage.Plan == nil || *usage.Plan != "GOAT" {
		t.Fatalf("plan = %#v", usage.Plan)
	}
	if usage.Credit == nil || usage.Credit.Remaining == nil || *usage.Credit.Remaining != 42 {
		t.Fatalf("credit = %#v", usage.Credit)
	}
}
