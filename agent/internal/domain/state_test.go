package domain_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
)

func TestSchemaV1GoldenFixturesRoundTrip(t *testing.T) {
	fixtures := []string{
		"healthy.json",
		"zcode_stale.json",
		"commandcode_auth_error.json",
		"backend_degraded.json",
		"task_failed.json",
	}

	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("..", "..", "..", "server", "tests", "fixtures", name)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}

			var state domain.HudState
			if err := json.Unmarshal(raw, &state); err != nil {
				t.Fatalf("decode typed fixture: %v", err)
			}
			if err := state.Validate(); err != nil {
				t.Fatalf("validate typed fixture: %v", err)
			}

			encoded, err := json.Marshal(state)
			if err != nil {
				t.Fatalf("encode typed fixture: %v", err)
			}

			var want, got any
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatalf("decode original JSON: %v", err)
			}
			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatalf("decode round-trip JSON: %v", err)
			}
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("schema-v1 round trip changed fixture\nwant: %s\n got: %s", raw, encoded)
			}
		})
	}
}

func TestOverallStatusMustMatchSourceHealth(t *testing.T) {
	path := filepath.Join("..", "..", "..", "server", "tests", "fixtures", "healthy.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state domain.HudState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	state.Overall.Status = domain.OverallDegraded
	if err := state.Validate(); err == nil {
		t.Fatal("expected invalid overall status to be rejected")
	}
}
