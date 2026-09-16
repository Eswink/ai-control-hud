package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Eswink/ai-control-hud/agent/internal/collector/commandcode"
	"github.com/Eswink/ai-control-hud/agent/internal/machineconfig"
	"github.com/Eswink/ai-control-hud/agent/internal/platform/secretstore"
)

const (
	manualCommandCodeProviderID  = "command-code"
	manualCommandCodeProviderURL = "https://api.commandcode.ai/provider/v1"
)

func runCommandCodeCommand(args []string) error {
	if !secretstore.Supported() {
		return secretstore.ErrUnsupported
	}
	if len(args) == 0 {
		return errors.New("commandcode command required: configure, status, remove")
	}
	switch args[0] {
	case "configure":
		return runCommandCodeConfigure(args[1:])
	case "status":
		return runCommandCodeStatus(args[1:])
	case "remove":
		return runCommandCodeRemove(args[1:])
	default:
		return fmt.Errorf("unknown commandcode command %q", args[0])
	}
}

func runCommandCodeConfigure(args []string) error {
	flags := flag.NewFlagSet("commandcode configure", flag.ContinueOnError)
	defaultConfig, err := machineconfig.DefaultPath()
	if err != nil {
		return err
	}
	configPath := flags.String("config", defaultConfig, "machine configuration path used to locate the protected CommandCode SecretStore")
	apiKeyFile := flags.String("api-key-file", "", "one-time plaintext file containing the operator-supplied CommandCode API key")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*apiKeyFile) == "" {
		return errors.New("commandcode configure --api-key-file is required")
	}

	secretPath, err := resolveCommandCodeSecretPath(absolute(*configPath))
	if err != nil {
		return err
	}
	apiKey, err := readCommandCodeAPIKeyFile(*apiKeyFile)
	if err != nil {
		return err
	}
	provider := &commandcode.Provider{
		ID:      manualCommandCodeProviderID,
		Name:    manualCommandCodeProviderID,
		Kind:    "openai",
		Enabled: true,
		BaseURL: manualCommandCodeProviderURL,
		APIKey:  apiKey,
	}
	if err := commandcode.ValidateProvider(provider, false); err != nil {
		return err
	}
	record := secretstore.Record{
		ProviderID: provider.ID,
		BaseURL:    provider.BaseURL,
		APIKey:     provider.APIKey,
	}
	if err := secretstore.Write(secretPath, record); err != nil {
		return fmt.Errorf("write CommandCode platform SecretStore: %w", err)
	}

	fmt.Printf("[commandcode] configured=true provider=%s host=%s secret=%s\n", provider.ID, provider.Host(), secretPath)
	fmt.Println("[commandcode] API key imported from the operator-supplied file; delete the plaintext key file after validation")
	return nil
}

func runCommandCodeStatus(args []string) error {
	flags := flag.NewFlagSet("commandcode status", flag.ContinueOnError)
	defaultConfig, err := machineconfig.DefaultPath()
	if err != nil {
		return err
	}
	configPath := flags.String("config", defaultConfig, "machine configuration path used to locate the protected CommandCode SecretStore")
	if err := flags.Parse(args); err != nil {
		return err
	}
	secretPath, err := resolveCommandCodeSecretPath(absolute(*configPath))
	if err != nil {
		return err
	}
	record, err := secretstore.Read(secretPath)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Printf("[commandcode] configured=false secret=%s\n", secretPath)
		return nil
	}
	if err != nil {
		return fmt.Errorf("read CommandCode platform SecretStore: %w", err)
	}
	provider := providerFromSecret(record)
	if err := commandcode.ValidateProvider(provider, false); err != nil {
		return err
	}
	fmt.Printf("[commandcode] configured=true provider=%s host=%s secret=%s\n", provider.ID, provider.Host(), secretPath)
	return nil
}

func runCommandCodeRemove(args []string) error {
	flags := flag.NewFlagSet("commandcode remove", flag.ContinueOnError)
	defaultConfig, err := machineconfig.DefaultPath()
	if err != nil {
		return err
	}
	configPath := flags.String("config", defaultConfig, "machine configuration path used to locate the protected CommandCode SecretStore")
	if err := flags.Parse(args); err != nil {
		return err
	}
	secretPath, err := resolveCommandCodeSecretPath(absolute(*configPath))
	if err != nil {
		return err
	}
	if err := secretstore.Remove(secretPath); err != nil {
		return fmt.Errorf("remove CommandCode platform SecretStore: %w", err)
	}
	fmt.Printf("[commandcode] configured=false removed=%s\n", secretPath)
	return nil
}

func resolveCommandCodeSecretPath(configPath string) (string, error) {
	if configPath == "" {
		return "", errors.New("machine configuration path is required")
	}
	config, err := machineconfig.Load(configPath)
	if err == nil {
		if strings.TrimSpace(config.CommandCodeSecret) == "" {
			return "", errors.New("machine config CommandCode secret path is missing")
		}
		return config.CommandCodeSecret, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("load machine config while locating CommandCode SecretStore: %w", err)
	}
	return machineconfig.DefaultSecretPath(configPath), nil
}

func readCommandCodeAPIKeyFile(path string) (string, error) {
	resolved := absolute(path)
	if resolved == "" {
		return "", errors.New("CommandCode API key file path is required")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect CommandCode API key file: %w", err)
	}
	if info.IsDir() {
		return "", errors.New("CommandCode API key file path is a directory")
	}
	if info.Size() > 16*1024 {
		return "", errors.New("CommandCode API key file is unexpectedly large")
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("read CommandCode API key file: %w", err)
	}
	defer zeroBytes(data)
	apiKey := strings.TrimSpace(string(data))
	if apiKey == "" {
		return "", errors.New("CommandCode API key file is empty")
	}
	if len(apiKey) > 4096 {
		return "", errors.New("CommandCode API key is unexpectedly large")
	}
	if strings.ContainsAny(apiKey, "\r\n\x00") {
		return "", errors.New("CommandCode API key must be a single non-NUL line")
	}
	return apiKey, nil
}
