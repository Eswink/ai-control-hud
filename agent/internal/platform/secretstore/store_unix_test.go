//go:build linux || darwin

package secretstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnixSecretStoreRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "commandcode.json")
	want := Record{ProviderID: "command", BaseURL: "https://api.commandcode.ai/provider/v1", APIKey: "test-value"}
	if err := Write(path, want); err != nil {
		t.Fatal(err)
	}
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
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestUnixSecretStoreRejectsBroadPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "commandcode.json")
	if err := os.WriteFile(path, []byte(`{"providerId":"command","baseUrl":"https://api.commandcode.ai/provider/v1","apiKey":"test-value"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err == nil {
		t.Fatal("expected broad permission rejection")
	}
}
