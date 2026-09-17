package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Eswink/ai-control-hud/agent/internal/platform/secretstore"
)

func TestPrepareCommandCodeSecretIgnoresUnspecifiedProviderFile(t *testing.T) {
	root := t.TempDir()
	providerPath := filepath.Join(root, ".local", "commandcode-provider.json")
	secretPath := filepath.Join(root, "state", "commandcode.dpapi")
	if err := os.MkdirAll(filepath.Dir(providerPath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeProviderFixture(t, providerPath, "must-not-be-imported")

	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	provider := flags.String("provider-config", providerPath, "provider")
	if err := flags.Parse(nil); err != nil {
		t.Fatal(err)
	}

	imported, err := prepareCommandCodeSecret(flags, *provider, secretPath)
	if err == nil {
		t.Fatal("implicit provider path was accepted without an existing SecretStore")
	}
	if imported {
		t.Fatal("implicit provider path was reported as imported")
	}
	if !strings.Contains(err.Error(), "commandcode configure --api-key-file") {
		t.Fatalf("error does not direct operator to explicit key import: %v", err)
	}
	if _, statErr := os.Stat(secretPath); !os.IsNotExist(statErr) {
		t.Fatalf("SecretStore was unexpectedly created: %v", statErr)
	}
}

func TestPrepareCommandCodeSecretImportsOnlyExplicitProviderFile(t *testing.T) {
	root := t.TempDir()
	providerPath := filepath.Join(root, "provider.json")
	secretPath := filepath.Join(root, "state", "commandcode.dpapi")
	const apiKey = "explicit-test-key"
	writeProviderFixture(t, providerPath, apiKey)

	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	provider := flags.String("provider-config", "", "provider")
	if err := flags.Parse([]string{"--provider-config", providerPath}); err != nil {
		t.Fatal(err)
	}

	imported, err := prepareCommandCodeSecret(flags, *provider, secretPath)
	if err != nil {
		t.Fatal(err)
	}
	if !imported {
		t.Fatal("explicit provider path was not reported as imported")
	}
	record, err := secretstore.Read(secretPath)
	if err != nil {
		t.Fatal(err)
	}
	if record.APIKey != apiKey || record.ProviderID != "command" {
		t.Fatal("explicit provider record was not preserved")
	}
}

func TestPrepareCommandCodeSecretReusesExistingStoreWithoutProviderFlag(t *testing.T) {
	root := t.TempDir()
	secretPath := filepath.Join(root, "state", "commandcode.dpapi")
	record := secretstore.Record{
		ProviderID: "command-code",
		BaseURL:    "https://api.commandcode.ai/provider/v1",
		APIKey:     "existing-protected-key",
	}
	if err := secretstore.Write(secretPath, record); err != nil {
		t.Fatal(err)
	}

	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	provider := flags.String("provider-config", "", "provider")
	if err := flags.Parse(nil); err != nil {
		t.Fatal(err)
	}
	imported, err := prepareCommandCodeSecret(flags, *provider, secretPath)
	if err != nil {
		t.Fatal(err)
	}
	if imported {
		t.Fatal("existing SecretStore reuse was reported as provider import")
	}
}

func writeProviderFixture(t *testing.T, path, apiKey string) {
	t.Helper()
	body := `{
  "provider": {
    "command": {
      "name": "command",
      "kind": "openai",
      "enabled": true,
      "options": {
        "baseURL": "https://api.commandcode.ai/provider/v1",
        "apiKey": "` + apiKey + `"
      },
      "models": {}
    }
  }
}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
