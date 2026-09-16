package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	apihttp "github.com/Eswink/ai-control-hud/agent/internal/api"
	"github.com/Eswink/ai-control-hud/agent/internal/collector/commandcode"
	"github.com/Eswink/ai-control-hud/agent/internal/collector/zcode"
	"github.com/Eswink/ai-control-hud/agent/internal/domain"
	"github.com/Eswink/ai-control-hud/agent/internal/mock"
	agentruntime "github.com/Eswink/ai-control-hud/agent/internal/runtime"
	"github.com/Eswink/ai-control-hud/agent/internal/store"
)

var version = "0.3.0-go-dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ai-control-agent:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "version":
			fmt.Println(version)
			return nil
		case "service":
			return runServiceCommand(args[1:])
		case "doctor":
			return runDoctor(args[1:])
		case "run":
			args = args[1:]
		}
	}
	return runForeground(args)
}

func runForeground(args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	listen := flags.String("listen", envOr("AI_CONTROL_LISTEN", "127.0.0.1:8788"), "HTTP listen address")
	fixture := flags.String("fixture", os.Getenv("AI_CONTROL_FIXTURE"), "schema-v1 fixture used during migration")
	config := flags.String("config", os.Getenv("AI_CONTROL_MACHINE_CONFIG"), "machine service configuration for foreground troubleshooting")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *config != "" && *fixture != "" {
		return errors.New("run --config and --fixture cannot be used together")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if *config != "" {
		return runConfigured(ctx, absolute(*config))
	}

	started := time.Now().UTC()
	var (
		initial       domain.HudState
		collectorLoop *agentruntime.Runtime
		zEnabled      bool
		ccEnabled     bool
	)

	if *fixture != "" {
		loaded, err := mock.LoadFixture(*fixture)
		if err != nil {
			return err
		}
		initial = loaded
	} else {
		zCollector, foundZCode := zcode.NewFromEnvironment()
		ccCollector, foundCommandCode := commandcode.NewFromEnvironment()
		zEnabled, ccEnabled = foundZCode, foundCommandCode
		initial = agentruntime.InitialState(started, version, zEnabled, ccEnabled)

		var zCollect agentruntime.ZCodeCollectFunc
		if zCollector != nil {
			zCollect = zCollector.Collect
		}
		var ccCollect agentruntime.CommandCodeCollectFunc
		if ccCollector != nil {
			ccCollect = ccCollector.Collect
		}

		snapshotStore, err := store.New(initial)
		if err != nil {
			return fmt.Errorf("initialize snapshot store: %w", err)
		}
		collectorLoop = agentruntime.New(snapshotStore, zCollect, ccCollect, agentruntime.DefaultConfig())
		return serve(ctx, *listen, false, zEnabled, ccEnabled, started, snapshotStore, collectorLoop)
	}

	snapshotStore, err := store.New(initial)
	if err != nil {
		return fmt.Errorf("initialize snapshot store: %w", err)
	}
	return serve(ctx, *listen, true, false, false, started, snapshotStore, nil)
}

func serve(
	ctx context.Context,
	listen string,
	fixture bool,
	zEnabled bool,
	ccEnabled bool,
	started time.Time,
	snapshotStore *store.SnapshotStore,
	collectorLoop *agentruntime.Runtime,
) error {
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("listen HTTP server on %s: %w", listen, err)
	}
	defer listener.Close()

	apiServer := apihttp.New(snapshotStore, version, started)
	httpServer := &http.Server{
		Handler:           apiServer.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if collectorLoop != nil {
		collectorLoop.Start(runCtx)
	}

	errCh := make(chan error, 1)
	go func() {
		fmt.Printf(
			"[agent] listen=http://%s schema=%d fixture=%t zcode=%s commandCode=%s\n",
			listen,
			domain.SchemaVersion,
			fixture,
			enabledLabel(zEnabled),
			enabledLabel(ccEnabled),
		)
		errCh <- httpServer.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
		if collectorLoop != nil {
			collectorLoop.Wait()
		}
		err := <-errCh
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case err := <-errCh:
		cancel()
		if collectorLoop != nil {
			collectorLoop.Wait()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func enabledLabel(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
