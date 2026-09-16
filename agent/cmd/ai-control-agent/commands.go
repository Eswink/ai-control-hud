package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/collector/commandcode"
	"github.com/Eswink/ai-control-hud/agent/internal/collector/zcode"
	"github.com/Eswink/ai-control-hud/agent/internal/domain"
	"github.com/Eswink/ai-control-hud/agent/internal/machineconfig"
	"github.com/Eswink/ai-control-hud/agent/internal/platform/secretstore"
	"github.com/Eswink/ai-control-hud/agent/internal/platform/winservice"
	agentruntime "github.com/Eswink/ai-control-hud/agent/internal/runtime"
	"github.com/Eswink/ai-control-hud/agent/internal/store"
)

const (
	windowsServiceName        = "AIControlHUD"
	windowsServiceDisplayName = "AI Control HUD Agent"
	windowsServiceDescription = "AI Control HUD local telemetry agent"
)

func runServiceCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("service command required: install, start, stop, restart, status, remove, run")
	}
	switch args[0] {
	case "install":
		return serviceInstall(args[1:])
	case "start":
		if err := winservice.Start(windowsServiceName); err != nil {
			return err
		}
		fmt.Println("[service] state=running")
		return nil
	case "stop":
		if err := winservice.Stop(windowsServiceName); err != nil {
			return err
		}
		fmt.Println("[service] state=stopped")
		return nil
	case "restart":
		if err := winservice.Restart(windowsServiceName); err != nil {
			return err
		}
		fmt.Println("[service] state=running")
		return nil
	case "status":
		info, err := winservice.Status(windowsServiceName)
		if err != nil {
			return err
		}
		fmt.Printf("[service] installed=%t state=%s pid=%d\n", info.Installed, info.State, info.ProcessID)
		return nil
	case "remove":
		return serviceRemove(args[1:])
	case "run":
		return serviceRun(args[1:])
	default:
		return fmt.Errorf("unknown service command %q", args[0])
	}
}

func serviceInstall(args []string) error {
	flags := flag.NewFlagSet("service install", flag.ContinueOnError)
	defaultConfig, err := machineconfig.DefaultPath()
	if err != nil {
		return err
	}
	configPath := flags.String("config", defaultConfig, "machine configuration path")
	providerConfig := flags.String("provider-config", defaultProviderConfigPath(), "one-time CommandCode provider import file")
	listen := flags.String("listen", "0.0.0.0:8787", "service HTTP listen address")
	runtimeDB := flags.String("runtime-db", "", "ZCode runtime Goal database")
	taskIndexDB := flags.String("task-index-db", "", "ZCode task index database")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if info, statusErr := winservice.Status(windowsServiceName); statusErr == nil && info.Installed {
		return errors.New("AI Control Agent service is already installed; remove it before reinstalling")
	} else if statusErr != nil && !errors.Is(statusErr, winservice.ErrUnsupported) {
		return statusErr
	}

	resolvedConfig := absolute(*configPath)
	existingConfig, existingErr := machineconfig.Load(resolvedConfig)
	hasExistingConfig := existingErr == nil
	if existingErr != nil && !errors.Is(existingErr, os.ErrNotExist) {
		return fmt.Errorf("load existing machine config: %w", existingErr)
	}

	resolvedRuntime, resolvedTaskIndex, err := zcode.ResolvedPaths()
	if err != nil {
		return fmt.Errorf("resolve ZCode paths: %w", err)
	}
	listenValue := *listen
	if hasExistingConfig {
		if !flagProvided(flags, "listen") {
			listenValue = existingConfig.Listen
		}
		if !flagProvided(flags, "runtime-db") && existingConfig.ZCodeRuntimeDB != "" {
			resolvedRuntime = existingConfig.ZCodeRuntimeDB
		}
		if !flagProvided(flags, "task-index-db") && existingConfig.ZCodeTaskIndexDB != "" {
			resolvedTaskIndex = existingConfig.ZCodeTaskIndexDB
		}
	}
	if flagProvided(flags, "runtime-db") {
		resolvedRuntime = absolute(*runtimeDB)
	}
	if flagProvided(flags, "task-index-db") {
		resolvedTaskIndex = absolute(*taskIndexDB)
	}
	if !fileExists(resolvedRuntime) && !fileExists(resolvedTaskIndex) {
		return errors.New("no readable ZCode database found for service installation")
	}

	secretPath := machineconfig.DefaultSecretPath(resolvedConfig)
	if hasExistingConfig && existingConfig.CommandCodeSecret != "" {
		secretPath = existingConfig.CommandCodeSecret
	}
	providerPath := absolute(*providerConfig)
	importedCredential := false
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
		importedCredential = true
	} else {
		record, readErr := secretstore.Read(secretPath)
		if readErr != nil {
			return fmt.Errorf("provider import file is unavailable and existing SecretStore cannot be read: %w", readErr)
		}
		if err := commandcode.ValidateProvider(providerFromSecret(record), false); err != nil {
			return err
		}
	}

	config := machineconfig.New(listenValue, resolvedRuntime, resolvedTaskIndex, secretPath)
	if err := machineconfig.Save(resolvedConfig, config); err != nil {
		return err
	}

	sourceExecutable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve agent executable: %w", err)
	}
	targetExecutable, err := defaultServiceExecutablePath()
	if err != nil {
		return err
	}
	if err := copyExecutable(absolute(sourceExecutable), targetExecutable); err != nil {
		return err
	}
	if err := winservice.Install(windowsServiceName, windowsServiceDisplayName, windowsServiceDescription, targetExecutable, resolvedConfig); err != nil {
		_ = os.Remove(targetExecutable)
		return err
	}

	fmt.Printf("[service] installed name=%s start=automatic\n", windowsServiceName)
	fmt.Printf("[service] executable=%s\n", targetExecutable)
	fmt.Printf("[service] config=%s\n", resolvedConfig)
	fmt.Printf("[service] zcode-runtime=%s\n", resolvedRuntime)
	fmt.Printf("[service] zcode-task-index=%s\n", resolvedTaskIndex)
	if importedCredential {
		fmt.Println("[service] CommandCode credential imported to Windows DPAPI SecretStore")
		fmt.Println("[service] plaintext provider import file was not modified; remove it only after service validation")
	} else {
		fmt.Println("[service] existing Windows DPAPI SecretStore reused; plaintext provider import is no longer required")
	}
	return nil
}

func serviceRemove(args []string) error {
	flags := flag.NewFlagSet("service remove", flag.ContinueOnError)
	purge := flags.Bool("purge", false, "also delete installed binary, machine config, and protected secret")
	defaultConfig, err := machineconfig.DefaultPath()
	if err != nil {
		return err
	}
	configPath := flags.String("config", defaultConfig, "machine configuration path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedConfig := absolute(*configPath)
	var secretPath string
	if config, loadErr := machineconfig.Load(resolvedConfig); loadErr == nil {
		secretPath = config.CommandCodeSecret
	}
	if err := winservice.Remove(windowsServiceName); err != nil {
		return err
	}
	fmt.Println("[service] removed")
	if *purge {
		if secretPath != "" {
			_ = secretstore.Remove(secretPath)
		}
		_ = os.Remove(resolvedConfig)
		if executable, executableErr := defaultServiceExecutablePath(); executableErr == nil {
			_ = os.Remove(executable)
		}
		fmt.Println("[service] installed binary, machine config, and protected secret purged")
	}
	return nil
}

func serviceRun(args []string) error {
	flags := flag.NewFlagSet("service run", flag.ContinueOnError)
	defaultConfig, err := machineconfig.DefaultPath()
	if err != nil {
		return err
	}
	configPath := flags.String("config", defaultConfig, "machine configuration path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolved := absolute(*configPath)
	return winservice.Run(windowsServiceName, func(ctx context.Context) error {
		return runConfigured(ctx, resolved)
	})
}

func runDoctor(args []string) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	defaultConfig, err := machineconfig.DefaultPath()
	if err != nil {
		return err
	}
	configPath := flags.String("config", defaultConfig, "machine configuration path")
	live := flags.Bool("live", false, "perform one live CommandCode billing request")
	if err := flags.Parse(args); err != nil {
		return err
	}

	resolved := absolute(*configPath)
	config, err := machineconfig.Load(resolved)
	if err != nil {
		return doctorFailure("config", err)
	}
	fmt.Printf("[doctor] config=ok path=%s listen=%s\n", resolved, config.Listen)

	zcodeReadable := false
	if config.ZCodeRuntimeDB != "" && fileExists(config.ZCodeRuntimeDB) {
		zcodeReadable = true
		fmt.Printf("[doctor] zcode-runtime=ok path=%s\n", config.ZCodeRuntimeDB)
	} else {
		fmt.Println("[doctor] zcode-runtime=missing")
	}
	if config.ZCodeTaskIndexDB != "" && fileExists(config.ZCodeTaskIndexDB) {
		zcodeReadable = true
		fmt.Printf("[doctor] zcode-task-index=ok path=%s\n", config.ZCodeTaskIndexDB)
	} else {
		fmt.Println("[doctor] zcode-task-index=missing")
	}
	if !zcodeReadable {
		return errors.New("doctor: no configured ZCode database is readable")
	}

	record, err := secretstore.Read(config.CommandCodeSecret)
	if err != nil {
		return doctorFailure("secret-store", err)
	}
	provider := providerFromSecret(record)
	if err := commandcode.ValidateProvider(provider, false); err != nil {
		return doctorFailure("commandcode-provider", err)
	}
	fmt.Printf("[doctor] secret-store=ok path=%s\n", config.CommandCodeSecret)
	fmt.Printf("[doctor] commandcode-provider=ok id=%s host=%s\n", provider.ID, provider.Host())

	if *live {
		collector := commandcode.New(nil)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		usage, err := collector.CollectProvider(ctx, provider)
		if err != nil {
			return doctorFailure("commandcode-live", err)
		}
		plan := "unknown"
		if usage != nil && usage.Plan != nil {
			plan = *usage.Plan
		}
		fmt.Printf("[doctor] commandcode-live=ok plan=%s\n", plan)
	}

	if secretstore.Supported() {
		if info, statusErr := winservice.Status(windowsServiceName); statusErr == nil {
			fmt.Printf("[doctor] service installed=%t state=%s pid=%d\n", info.Installed, info.State, info.ProcessID)
		}
	}
	fmt.Println("[doctor] result=ok")
	return nil
}

func runConfigured(ctx context.Context, configPath string) error {
	config, err := machineconfig.Load(configPath)
	if err != nil {
		return err
	}
	zCollector := zcode.New(config.ZCodeRuntimeDB, config.ZCodeTaskIndexDB)
	record, err := secretstore.Read(config.CommandCodeSecret)
	if err != nil {
		return fmt.Errorf("load CommandCode SecretStore: %w", err)
	}
	provider := providerFromSecret(record)
	if err := commandcode.ValidateProvider(provider, false); err != nil {
		return err
	}
	ccCollector := commandcode.New(nil)

	started := time.Now().UTC()
	initial := agentruntime.InitialState(started, version, true, true)
	snapshotStore, err := store.New(initial)
	if err != nil {
		return fmt.Errorf("initialize snapshot store: %w", err)
	}
	collectorLoop := agentruntime.New(
		snapshotStore,
		zCollector.Collect,
		func(collectCtx context.Context) (*domain.UsageSummary, error) {
			return ccCollector.CollectProvider(collectCtx, provider)
		},
		agentruntime.DefaultConfig(),
	)
	return serve(ctx, config.Listen, false, true, true, started, snapshotStore, collectorLoop)
}

func providerFromSecret(record secretstore.Record) *commandcode.Provider {
	return &commandcode.Provider{
		ID:      record.ProviderID,
		Name:    record.ProviderID,
		Kind:    "openai",
		Enabled: true,
		BaseURL: record.BaseURL,
		APIKey:  record.APIKey,
	}
}

func defaultProviderConfigPath() string {
	if configured := strings.TrimSpace(os.Getenv("HUD_ZCODE_CONFIG")); configured != "" {
		return configured
	}
	return filepath.Join(".local", "commandcode-provider.json")
}

func defaultServiceExecutablePath() (string, error) {
	programFiles := strings.TrimSpace(os.Getenv("ProgramFiles"))
	if programFiles == "" {
		return "", errors.New("ProgramFiles is unavailable")
	}
	return filepath.Join(programFiles, "AI Control HUD", "ai-control-agent.exe"), nil
}

func copyExecutable(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create service binary directory: %w", err)
	}
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open service source executable: %w", err)
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("create installed service executable: %w", err)
	}
	committed := false
	defer func() {
		_ = output.Close()
		if !committed {
			_ = os.Remove(target)
		}
	}()
	if _, err := io.Copy(output, input); err != nil {
		return fmt.Errorf("copy service executable: %w", err)
	}
	if err := output.Sync(); err != nil {
		return fmt.Errorf("flush service executable: %w", err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("close service executable: %w", err)
	}
	committed = true
	return nil
}

func flagProvided(flags *flag.FlagSet, name string) bool {
	provided := false
	flags.Visit(func(current *flag.Flag) {
		if current.Name == name {
			provided = true
		}
	})
	return provided
}

func doctorFailure(check string, err error) error {
	fmt.Printf("[doctor] %s=error\n", check)
	return fmt.Errorf("doctor: %s failed: %w", check, err)
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func absolute(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	result, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(result)
}
