package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	apihttp "github.com/Eswink/ai-control-hud/agent/internal/api"
	"github.com/Eswink/ai-control-hud/agent/internal/domain"
	"github.com/Eswink/ai-control-hud/agent/internal/mock"
	"github.com/Eswink/ai-control-hud/agent/internal/store"
)

var version = "0.2.0-go-dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ai-control-agent:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "version" {
		fmt.Println(version)
		return nil
	}
	if len(args) > 0 && args[0] == "run" {
		args = args[1:]
	}
	return runForeground(args)
}

func runForeground(args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	listen := flags.String("listen", envOr("AI_CONTROL_LISTEN", "127.0.0.1:8788"), "HTTP listen address")
	fixture := flags.String("fixture", os.Getenv("AI_CONTROL_FIXTURE"), "schema-v1 fixture used during migration")
	if err := flags.Parse(args); err != nil {
		return err
	}

	started := time.Now().UTC()
	state := disabledState(started)
	if *fixture != "" {
		loaded, err := mock.LoadFixture(*fixture)
		if err != nil {
			return err
		}
		state = loaded
	}

	snapshotStore, err := store.New(state)
	if err != nil {
		return fmt.Errorf("initialize snapshot store: %w", err)
	}

	apiServer := apihttp.New(snapshotStore, version, started)
	httpServer := &http.Server{
		Addr:              *listen,
		Handler:           apiServer.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		fmt.Printf("[agent] listen=http://%s fixture=%t schema=%d\n", *listen, *fixture != "", domain.SchemaVersion)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
		err := <-errCh
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func disabledState(now time.Time) domain.HudState {
	zMessage := "Go ZCode collector is not configured yet"
	ccMessage := "Go CommandCode collector is not configured yet"
	return domain.HudState{
		SchemaVersion: domain.SchemaVersion,
		Server: domain.ServerInfo{
			Version:       version,
			Time:          now,
			UptimeSeconds: 0,
		},
		Overall: domain.OverallStatus{Status: domain.OverallDegraded},
		ZCode: domain.ZCodeState{
			Health: domain.SourceHealth{
				Status:     domain.SourceDisabled,
				ObservedAt: now,
				Message:    &zMessage,
			},
		},
		CommandCode: domain.CommandCodeState{
			Health: domain.SourceHealth{
				Status:     domain.SourceDisabled,
				ObservedAt: now,
				Message:    &ccMessage,
			},
		},
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
