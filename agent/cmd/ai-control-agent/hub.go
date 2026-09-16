package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/Eswink/ai-control-hud/agent/internal/machineconfig"
	"github.com/Eswink/ai-control-hud/agent/internal/platform/secretstore"
)

func runHubCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("hub command required: configure, status, remove")
	}
	switch args[0] {
	case "configure":
		return runHubConfigure(args[1:])
	case "status":
		return runHubStatus(args[1:])
	case "remove":
		return runHubRemove(args[1:])
	default:
		return fmt.Errorf("unknown hub command %q", args[0])
	}
}

func runHubConfigure(args []string) error {
	if !secretstore.Supported() {
		return secretstore.ErrUnsupported
	}
	flags := flag.NewFlagSet("hub configure", flag.ContinueOnError)
	defaultConfig, err := machineconfig.DefaultPath()
	if err != nil {
		return err
	}
	configPath := flags.String("config", defaultConfig, "machine configuration path used to derive the protected hub credential path")
	hubFlags := addHubSecretFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedConfig := absolute(*configPath)
	path, configured, imported, err := configureProtectedHubSecret(flags, resolvedConfig, hubFlags)
	if err != nil {
		return err
	}
	if !configured {
		return errors.New("hub configure requires --hub-url/--hub-agent-id/--hub-token-file or an existing protected hub credential")
	}
	record, err := secretstore.ReadHub(path)
	if err != nil {
		return err
	}
	fmt.Printf("[hub] configured=true agent=%s url=%s secret=%s\n", record.AgentID, record.BaseURL, path)
	if imported {
		fmt.Println("[hub] token imported to platform SecretStore; delete the plaintext token file after validation")
	}
	return nil
}

func runHubStatus(args []string) error {
	flags := flag.NewFlagSet("hub status", flag.ContinueOnError)
	defaultConfig, err := machineconfig.DefaultPath()
	if err != nil {
		return err
	}
	configPath := flags.String("config", defaultConfig, "machine configuration path used to derive the protected hub credential path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	path := machineconfig.DefaultHubSecretPath(absolute(*configPath))
	_, exists, err := readExistingHubSecret(path)
	if err != nil {
		return err
	}
	if !exists {
		fmt.Printf("[hub] configured=false secret=%s\n", path)
		return nil
	}
	record, err := secretstore.ReadHub(path)
	if err != nil {
		return err
	}
	fmt.Printf("[hub] configured=true agent=%s url=%s secret=%s\n", record.AgentID, record.BaseURL, path)
	return nil
}

func runHubRemove(args []string) error {
	flags := flag.NewFlagSet("hub remove", flag.ContinueOnError)
	defaultConfig, err := machineconfig.DefaultPath()
	if err != nil {
		return err
	}
	configPath := flags.String("config", defaultConfig, "machine configuration path used to derive the protected hub credential path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	path := machineconfig.DefaultHubSecretPath(absolute(*configPath))
	if err := secretstore.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	fmt.Printf("[hub] configured=false removed=%s\n", path)
	return nil
}
