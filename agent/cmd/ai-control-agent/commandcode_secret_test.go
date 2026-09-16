package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Eswink/ai-control-hud/agent/internal/machineconfig"
	"github.com/Eswink/ai-control-hud/agent/internal/platform/secretstore"
)

func TestCommandCodeConfigureImportsOperatorKey(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "agent.json")
	keyPath := filepath.Join(root, "commandcode.key")
	const apiKey = "cc-test-operator-supplied-key"
	if err := os.WriteFile(keyPath, []byte(apiKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := runCommandCodeConfigure([]string{"--config", configPath, "--api-key-file", keyPath}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	secretPath := machineconfig.DefaultSecretPath(configPath)
	record, err := secretstore.Read(secretPath)
	if err != nil {
		t.Fatalf("read SecretStore: %v", err)
	}
	if record.ProviderID != manualCommandCodeProviderID {
		t.Fatalf("provider id = %q", record.ProviderID)
	}
	if record.BaseURL != manualCommandCodeProviderURL {
		t.Fatalf("base url = %q", record.BaseURL)
	}
	if record.APIKey != apiKey {
		t.Fatal("imported API key does not match operator input")
	}

	if err := runCommandCodeRemove([]string{"--config", configPath}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(secretPath); !os.IsNotExist(err) {
		t.Fatalf("secret still exists after remove: %v", err)
	}
}

func TestReadCommandCodeAPIKeyFileRejectsUnsafeInput(t *testing.T) {
	root := t.TempDir()
	cases := map[string]string{
		"empty":     "   \n",
		"multiline": "first\nsecond\n",
		"nul":       "abc\x00def\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(root, name+".key")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readCommandCodeAPIKeyFile(path); err == nil {
				t.Fatal("unsafe key file was accepted")
			}
		})
	}
}

func TestResolveCommandCodeSecretPathUsesMachineConfig(t *testing.T) {
	root := t.TempDir()
	runtimeDB := filepath.Join(root, "runtime.sqlite")
	secretPath := filepath.Join(root, "custom-commandcode.dpapi")
	if err := os.WriteFile(runtimeDB, []byte("placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "agent.json")
	config := machineconfig.New("127.0.0.1:8787", runtimeDB, "", secretPath)
	if err := machineconfig.Save(configPath, config); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "service access") {
			t.Skipf("platform source-access setup unavailable in test environment: %v", err)
		}
		t.Fatal(err)
	}
	resolved, err := resolveCommandCodeSecretPath(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != secretPath {
		t.Fatalf("resolved secret path = %q, want %q", resolved, secretPath)
	}
}
