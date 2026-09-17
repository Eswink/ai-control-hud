//go:build linux || darwin

package secretstore

import (
	"os"
	"path/filepath"
	"testing"
)

const testRecordJSON = `{"providerId":"command","baseUrl":"https://api.commandcode.ai/provider/v1","apiKey":"test-value"}`

func TestUnixSecretStoreRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "commandcode.json")
	want := Record{ProviderID: "command", BaseURL: "https://api.commandcode.ai/provider/v1", APIKey: "test-value"}
	if err := Write(path, want); err != nil {
		t.Fatal(err)
	}
	assertSecretPermissions(t, path)
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestUnixHubSecretRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "hub.json")
	want := HubRecord{AgentID: "desktop-main", BaseURL: "http://100.64.0.10:8787", Token: "hub-test-value"}
	if err := WriteHub(path, want); err != nil {
		t.Fatal(err)
	}
	assertSecretPermissions(t, path)
	got, err := ReadHub(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestUnixSecretStoreRejectsBroadFilePermissions(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "commandcode.json")
	if err := os.WriteFile(path, []byte(testRecordJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err == nil {
		t.Fatal("expected broad file permission rejection")
	}
}

func TestUnixSecretStoreRejectsBroadDirectoryPermissions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "commandcode.json")
	if err := os.WriteFile(path, []byte(testRecordJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err == nil {
		t.Fatal("expected broad directory permission rejection")
	}
}

func assertSecretPermissions(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("secret mode = %o, want 600", got)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("secret directory mode = %o, want 700", got)
	}
}
