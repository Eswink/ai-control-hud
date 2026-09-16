package remote

import (
	"path/filepath"
	"testing"
)

func TestResolveOutboxPathHonorsExplicitConfiguration(t *testing.T) {
	configured := filepath.Join(t.TempDir(), "nested", "events.sqlite3")
	t.Setenv(outboxEnvironment, configured)

	got, err := ResolveOutboxPath()
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(configured)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("outbox path=%q want %q", got, want)
	}
}
