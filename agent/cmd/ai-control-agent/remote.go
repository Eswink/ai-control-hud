package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/Eswink/ai-control-hud/agent/internal/events"
	"github.com/Eswink/ai-control-hud/agent/internal/machineconfig"
	"github.com/Eswink/ai-control-hud/agent/internal/platform/secretstore"
	"github.com/Eswink/ai-control-hud/agent/internal/remote"
	"github.com/Eswink/ai-control-hud/agent/internal/store"
)

func startRemoteUploader(ctx context.Context, snapshotStore *store.SnapshotStore) (*remote.Runtime, error) {
	config, enabled, source, err := resolveRemoteConfig()
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, nil
	}
	client, err := remote.NewClient(config)
	if err != nil {
		return nil, err
	}
	outboxPath, err := remote.ResolveOutboxPath()
	if err != nil {
		return nil, err
	}
	outbox, err := events.Open(outboxPath)
	if err != nil {
		return nil, err
	}
	loop := remote.NewRuntime(
		snapshotStore,
		client,
		outbox,
		version,
		config,
		func(uploadErr error) {
			fmt.Printf("[agent] hub-upload-error=%v\n", uploadErr)
		},
	)
	loop.Start(ctx)
	fmt.Printf(
		"[agent] hub=%s agent=%s credential=%s snapshot=%s heartbeat=%s events=%s/%s\n",
		config.BaseURL,
		config.AgentID,
		source,
		config.SnapshotInterval,
		config.HeartbeatInterval,
		config.EventScanInterval,
		config.EventSendInterval,
	)
	return loop, nil
}

func resolveRemoteConfig() (remote.Config, bool, string, error) {
	configPath, pathErr := machineconfig.DefaultPath()
	if pathErr == nil {
		secretPath := machineconfig.DefaultHubSecretPath(configPath)
		info, err := os.Stat(secretPath)
		switch {
		case err == nil:
			if info.IsDir() {
				return remote.Config{}, false, "", errors.New("protected hub credential path is a directory")
			}
			record, err := secretstore.ReadHub(secretPath)
			if err != nil {
				return remote.Config{}, false, "", fmt.Errorf("load protected hub credential: %w", err)
			}
			config, err := remote.NewConfig(record.BaseURL, record.AgentID, record.Token)
			if err != nil {
				return remote.Config{}, false, "", err
			}
			return config, true, "secretstore", nil
		case !errors.Is(err, os.ErrNotExist):
			return remote.Config{}, false, "", fmt.Errorf("inspect protected hub credential: %w", err)
		}
	}

	config, enabled, err := remote.FromEnvironment()
	if err != nil {
		return remote.Config{}, false, "", err
	}
	if !enabled {
		return remote.Config{}, false, "", nil
	}
	return config, true, "environment", nil
}
