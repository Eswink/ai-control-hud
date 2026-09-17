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
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/hub"
)

var version = "0.4.0-hub-go-dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ai-control-hub:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "version":
			fmt.Println(version)
			return nil
		case "backup":
			return runBackup(args[1:])
		case "serve":
			args = args[1:]
		}
	}
	return runServe(args)
}

func runServe(args []string) error {
	config, err := hub.FromEnvironment()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	host := flags.String("host", "127.0.0.1", "HTTP listen host")
	port := flags.Int("port", config.HTTPPort, "HTTP listen port")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *port < 1 || *port > 65535 {
		return errors.New("HTTP port must be within 1..65535")
	}
	config.HTTPPort = *port

	store, err := hub.OpenStore(config.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()
	api, err := hub.NewServer(config, store, version, nil)
	if err != nil {
		return err
	}

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(signalCtx)
	defer cancel()

	discovery, err := hub.StartDiscovery(ctx, config, version)
	if err != nil {
		return err
	}
	if discovery != nil {
		defer discovery.Close()
	}

	var maintenanceWG sync.WaitGroup
	maintenanceWG.Add(1)
	go func() {
		defer maintenanceWG.Done()
		hub.RunMaintenance(ctx, store, config, func(format string, args ...any) {
			fmt.Printf(format+"\n", args...)
		})
	}()
	defer func() {
		cancel()
		maintenanceWG.Wait()
	}()

	address := net.JoinHostPort(*host, strconv.Itoa(*port))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", address, err)
	}
	defer listener.Close()

	httpServer := &http.Server{
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	fmt.Printf("[hub] version=%s listen=%s database=%s agent=%s discovery=%t udp=%d hub=%s retention=%dd/%d..%d maintenance=%s\n",
		version,
		address,
		config.DatabasePath,
		config.PrimaryAgentID,
		config.DiscoveryEnabled,
		config.DiscoveryPort,
		config.HubID,
		config.EventRetentionDays,
		config.EventRetentionMin,
		config.EventRetentionMax,
		config.MaintenanceInterval,
	)

	httpErr := make(chan error, 1)
	go func() {
		err := httpServer.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		httpErr <- err
	}()
	var discoveryErr <-chan error
	if discovery != nil {
		ch := make(chan error, 1)
		discoveryErr = ch
		go func() { ch <- discovery.Wait() }()
	}

	select {
	case <-ctx.Done():
	case err := <-httpErr:
		if err != nil {
			return err
		}
	case err := <-discoveryErr:
		if err != nil {
			return err
		}
	}
	cancel()
	if discovery != nil {
		_ = discovery.Close()
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown Hub HTTP server: %w", err)
	}
	return nil
}

func runBackup(args []string) error {
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	database := flags.String("database", "", "source Hub SQLite database")
	output := flags.String("output", "", "new backup SQLite file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *database == "" || *output == "" {
		return errors.New("backup requires --database and --output")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := hub.BackupDatabase(ctx, *database, *output); err != nil {
		return err
	}
	fmt.Printf("[hub] backup=%s source=%s integrity=ok\n", *output, *database)
	return nil
}
