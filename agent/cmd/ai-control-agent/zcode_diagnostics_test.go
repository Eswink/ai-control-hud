package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestZCodeStorageDiagnosticsArePathFreeAndActionable(t *testing.T) {
	root := t.TempDir()
	runtimeDB := filepath.Join(root, ".zcode", "cli", "db", "db.sqlite")
	taskDB := filepath.Join(root, ".zcode", "v2", "tasks-index.sqlite")
	logDir := filepath.Join(root, ".zcode", "cli", "log")
	for _, path := range []string{filepath.Dir(runtimeDB), filepath.Dir(taskDB), logDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(runtimeDB, []byte("db"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(taskDB, []byte("db"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := zcodeStorageDiagnosticsProvider(
		"machine-config",
		"machine-config",
		runtimeDB,
		taskDB,
		logDir,
	)()
	if got.BindingMode != "machine-config" || got.LayoutSource != "machine-config" {
		t.Fatalf("unexpected binding labels: %#v", got)
	}
	if !got.RuntimeDatabaseReadable || !got.TaskIndexReadable || !got.TurnLogReadable {
		t.Fatalf("expected readable sources: %#v", got)
	}
	if got.RefreshRecommended {
		t.Fatalf("unexpected refresh recommendation: %#v", got)
	}

	if err := os.Remove(runtimeDB); err != nil {
		t.Fatal(err)
	}
	got = zcodeStorageDiagnosticsProvider(
		"machine-config",
		"machine-config",
		runtimeDB,
		taskDB,
		logDir,
	)()
	if got.RuntimeDatabaseReadable || !got.RefreshRecommended {
		t.Fatalf("missing configured runtime must recommend refresh: %#v", got)
	}
}

func TestSanitizeBindingLabelRejectsPrivateOrUnexpectedText(t *testing.T) {
	if got := sanitizeBindingLabel("data_base_setting", "unknown"); got != "data_base_setting" {
		t.Fatalf("valid label changed to %q", got)
	}
	for _, value := range []string{
		"C:\\Users\\private\\.zcode",
		"data base setting",
		"../../private",
	} {
		if got := sanitizeBindingLabel(value, "unknown"); got != "unknown" {
			t.Fatalf("unsafe label %q became %q", value, got)
		}
	}
}
