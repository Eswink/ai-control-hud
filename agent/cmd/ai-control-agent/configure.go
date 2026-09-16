package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Eswink/ai-control-hud/agent/internal/collector/commandcode"
	"github.com/Eswink/ai-control-hud/agent/internal/collector/zcode"
	"github.com/Eswink/ai-control-hud/agent/internal/machineconfig"
	"github.com/Eswink/ai-control-hud/agent/internal/platform/secretstore"
)

func runConfigure(args []string) error {
	if !secretstore.Supported() {
		return secretstore.ErrUnsupported
	}
	flags := flag.NewFlagSet("configure", flag.ContinueOnError)
	defaultConfig, err := machineconfig.DefaultPath()
	if err != nil {
		return err
	}
	configPath := flags.String("config", defaultConfig, "machine configuration path")
	providerConfig := flags.String("provider-config", defaultProviderConfigPath(), "CommandCode provider import file")
	listen := flags.String("listen", "127.0.0.1:8787", "HTTP listen address")
	runtimeDB := flags.String("runtime-db", "", "ZCode runtime Goal database")
	taskIndexDB := flags.String("task-index-db", "", "ZCode task index database")
	hubFlags := addHubSecretFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}

	resolvedConfig := absolute(*configPath)
	existing, existingErr := machineconfig.Load(resolvedConfig)
	hasExisting := existingErr == nil
	if existingErr != nil && !errors.Is(existingErr, os.ErrNotExist) {
		return fmt.Errorf("load existing machine config: %w", existingErr)
	}

	resolvedRuntime, resolvedTaskIndex, err := zcode.ResolvedPaths()
	if err != nil {
		return fmt.Errorf("resolve ZCode paths: %w", err)
	}
	listenValue := strings.TrimSpace(*listen)
	if hasExisting {
		if !flagProvided(flags, "listen") {
			listenValue = existing.Listen
		}
		if !flagProvided(flags, "runtime-db") && existing.ZCodeRuntimeDB != "" {
			resolvedRuntime = existing.ZCodeRuntimeDB
		}
		if !flagProvided(flags, "task-index-db") && existing.ZCodeTaskIndexDB != "" {
			resolvedTaskIndex = existing.ZCodeTaskIndexDB
		}
	}
	if flagProvided(flags, "runtime-db") {
		resolvedRuntime = absolute(*runtimeDB)
	}
	if flagProvided(flags, "task-index-db") {
		resolvedTaskIndex = absolute(*taskIndexDB)
	}
	if !fileExists(resolvedRuntime) && !fileExists(resolvedTaskIndex) {
		return errors.New("no readable ZCode database found for configuration")
	}

	secretPath := machineconfig.DefaultSecretPath(resolvedConfig)
	if hasExisting && existing.CommandCodeSecret != "" {
		secretPath = existing.CommandCodeSecret
	}
	providerPath := absolute(*providerConfig)
	imported := false
	if fileExists(providerPath) {
		provider, loadErr := commandcode.LoadProvider(providerPath, "")
		if loadErr != nil {
			return loadErr
		}
		if err := commandcode.ValidateProvider(provider, false); err != nil {
			return err
		}
		record := secretstore.Record{ProviderID: provider.ID, BaseURL: provider.BaseURL, APIKey: provider.APIKey}
		if err := secretstore.Write(secretPath, record); err != nil {
			return fmt.Errorf("write platform SecretStore: %w", err)
		}
		imported = true
	} else {
		record, readErr := secretstore.Read(secretPath)
		if readErr != nil {
			return fmt.Errorf("provider import file is unavailable and existing SecretStore cannot be read: %w", readErr)
		}
		if err := commandcode.ValidateProvider(providerFromSecret(record), false); err != nil {
			return err
		}
	}

	hubSecretPath, hubConfigured, hubTokenImported, err := configureProtectedHubSecret(flags, resolvedConfig, hubFlags)
	if err != nil {
		return err
	}

	config := machineconfig.New(listenValue, resolvedRuntime, resolvedTaskIndex, secretPath)
	if err := machineconfig.Save(resolvedConfig, config); err != nil {
		return err
	}
	fmt.Printf("[configure] config=%s listen=%s\n", resolvedConfig, config.Listen)
	fmt.Printf("[configure] zcode-runtime=%s\n", config.ZCodeRuntimeDB)
	fmt.Printf("[configure] zcode-task-index=%s\n", config.ZCodeTaskIndexDB)
	if imported {
		fmt.Println("[configure] CommandCode credential imported to platform SecretStore")
	} else {
		fmt.Println("[configure] existing platform SecretStore reused")
	}
	if hubConfigured {
		fmt.Printf("[configure] hub-credential=%s\n", hubSecretPath)
		if hubTokenImported {
			fmt.Println("[configure] hub token imported to platform SecretStore; delete the plaintext token file after validation")
		} else {
			fmt.Println("[configure] existing protected hub credential reused or updated")
		}
	} else {
		fmt.Println("[configure] protected hub credential=not configured; environment-based hub configuration remains available")
	}
	return nil
}
