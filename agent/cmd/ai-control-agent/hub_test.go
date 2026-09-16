package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Eswink/ai-control-hud/agent/internal/machineconfig"
	"github.com/Eswink/ai-control-hud/agent/internal/platform/secretstore"
)

func TestResolveRemoteConfigPrefersProtectedSecret(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "agent.json")
	t.Setenv("AI_CONTROL_MACHINE_CONFIG", configPath)
	t.Setenv("AI_CONTROL_HUB_URL", "https://environment.example.test")
	t.Setenv("AI_CONTROL_HUB_TOKEN", "environment-token")
	t.Setenv("AI_CONTROL_HUB_AGENT_ID", "environment-agent")

	want := secretstore.HubRecord{
		AgentID: "desktop-main",
		BaseURL: "http://100.64.0.10:8787",
		Token:   "protected-token",
	}
	if err := secretstore.WriteHub(machineconfig.DefaultHubSecretPath(configPath), want); err != nil {
		t.Fatal(err)
	}

	config, enabled, source, err := resolveRemoteConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !enabled || source != "secretstore" {
		t.Fatalf("enabled=%t source=%q", enabled, source)
	}
	if config.BaseURL != want.BaseURL || config.AgentID != want.AgentID || config.Token != want.Token {
		t.Fatalf("unexpected remote config %#v", config)
	}
}

func TestResolveRemoteConfigFallsBackToEnvironment(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "agent.json")
	t.Setenv("AI_CONTROL_MACHINE_CONFIG", configPath)
	t.Setenv("AI_CONTROL_HUB_URL", "https://environment.example.test")
	t.Setenv("AI_CONTROL_HUB_TOKEN", "environment-token")
	t.Setenv("AI_CONTROL_HUB_AGENT_ID", "environment-agent")

	config, enabled, source, err := resolveRemoteConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !enabled || source != "environment" {
		t.Fatalf("enabled=%t source=%q", enabled, source)
	}
	if config.AgentID != "environment-agent" || config.Token != "environment-token" {
		t.Fatalf("unexpected environment config %#v", config)
	}
}

func TestHubConfigureAndTokenRotationPreserveEndpoint(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "agent.json")
	firstToken := filepath.Join(t.TempDir(), "token-one.txt")
	secondToken := filepath.Join(t.TempDir(), "token-two.txt")
	if err := os.WriteFile(firstToken, []byte("token-one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondToken, []byte("token-two\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := runHubCommand([]string{
		"configure",
		"--config", configPath,
		"--hub-url", "http://100.64.0.10:8787",
		"--hub-agent-id", "desktop-main",
		"--hub-token-file", firstToken,
	}); err != nil {
		t.Fatal(err)
	}
	path := machineconfig.DefaultHubSecretPath(configPath)
	first, err := secretstore.ReadHub(path)
	if err != nil {
		t.Fatal(err)
	}
	if first.Token != "token-one" {
		t.Fatalf("first token=%q", first.Token)
	}

	if err := runHubCommand([]string{
		"configure",
		"--config", configPath,
		"--hub-token-file", secondToken,
	}); err != nil {
		t.Fatal(err)
	}
	rotated, err := secretstore.ReadHub(path)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Token != "token-two" || rotated.BaseURL != first.BaseURL || rotated.AgentID != first.AgentID {
		t.Fatalf("rotated record=%#v first=%#v", rotated, first)
	}

	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("hub-only configure unexpectedly created machine config: %v", err)
	}
}
