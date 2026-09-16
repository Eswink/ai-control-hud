package main

import (
	"context"
	"fmt"

	"github.com/Eswink/ai-control-hud/agent/internal/events"
	"github.com/Eswink/ai-control-hud/agent/internal/remote"
	"github.com/Eswink/ai-control-hud/agent/internal/store"
)

func startRemoteUploader(ctx context.Context, snapshotStore *store.SnapshotStore) (*remote.Runtime, error) {
	config, enabled, err := remote.FromEnvironment()
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
		"[agent] hub=%s agent=%s snapshot=%s heartbeat=%s events=%s/%s\n",
		config.BaseURL,
		config.AgentID,
		config.SnapshotInterval,
		config.HeartbeatInterval,
		config.EventScanInterval,
		config.EventSendInterval,
	)
	return loop, nil
}
