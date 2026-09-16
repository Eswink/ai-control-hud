package machineconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "agent.json")
	config := New(
		"0.0.0.0:8787",
		filepath.Join(root, "db.sqlite"),
		filepath.Join(root, "tasks.sqlite"),
		filepath.Join(root, "secret.dpapi"),
	)
	if err := Save(path, config); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != config {
		t.Fatalf("loaded = %#v, want %#v", loaded, config)
	}
}

func TestRejectsRelativeCollectorPaths(t *testing.T) {
	config := Config{
		SchemaVersion:     schemaVersion,
		Listen:            "0.0.0.0:8787",
		ZCodeRuntimeDB:    "relative.db",
		CommandCodeSecret: filepath.Join(t.TempDir(), "secret.dpapi"),
	}
	if err := config.Validate(); err == nil {
		t.Fatal("expected relative ZCode path rejection")
	}
}

func TestDefaultPathCanBeOverridden(t *testing.T) {
	path := filepath.Join(t.TempDir(), "machine.json")
	t.Setenv("AI_CONTROL_MACHINE_CONFIG", path)
	got, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.json")
	if err := os.WriteFile(path, []byte(`{"schemaVersion":1,"listen":"0.0.0.0:8787","zcodeRuntimeDb":"C:/x","unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown-field rejection")
	}
}
