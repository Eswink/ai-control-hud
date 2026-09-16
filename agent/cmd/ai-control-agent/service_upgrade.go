package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Eswink/ai-control-hud/agent/internal/platform/winservice"
)

func serviceUpgrade(args []string) error {
	flags := flag.NewFlagSet("service upgrade", flag.ContinueOnError)
	sourceFlag := flags.String("source", "", "replacement Agent executable; defaults to the current CLI executable")
	if err := flags.Parse(args); err != nil {
		return err
	}

	info, err := winservice.Status(windowsServiceName)
	if err != nil {
		return err
	}
	if !info.Installed {
		return errors.New("AI Control Agent service is not installed")
	}
	if info.State != "running" && info.State != "stopped" {
		return fmt.Errorf("service upgrade requires running or stopped state; current state=%s", info.State)
	}

	source := strings.TrimSpace(*sourceFlag)
	if source == "" {
		source, err = os.Executable()
		if err != nil {
			return fmt.Errorf("resolve upgrade source executable: %w", err)
		}
	}
	source = absolute(source)
	target, err := defaultServiceExecutablePath()
	if err != nil {
		return err
	}
	target = absolute(target)

	staged, err := stageUpgradeExecutable(source, target)
	if err != nil {
		return err
	}
	stagedConsumed := false
	defer func() {
		if !stagedConsumed {
			_ = os.Remove(staged)
		}
	}()

	wasRunning := info.State == "running"
	if wasRunning {
		if err := winservice.Stop(windowsServiceName); err != nil {
			return fmt.Errorf("stop service before upgrade: %w", err)
		}
	}

	backup, err := swapUpgradeExecutable(target, staged)
	if err != nil {
		if wasRunning {
			if restartErr := winservice.Start(windowsServiceName); restartErr != nil {
				return fmt.Errorf("commit service upgrade: %w; old service restart also failed: %v", err, restartErr)
			}
		}
		return err
	}
	stagedConsumed = true

	if wasRunning {
		if startErr := winservice.Start(windowsServiceName); startErr != nil {
			stopErr := winservice.Stop(windowsServiceName)
			rollbackErr := rollbackUpgradeExecutable(target, backup)
			restartErr := winservice.Start(windowsServiceName)
			if rollbackErr != nil {
				return fmt.Errorf(
					"new service failed to start: %w; stop-before-rollback: %v; binary rollback failed: %v; old service restart: %v",
					startErr, stopErr, rollbackErr, restartErr,
				)
			}
			if restartErr != nil {
				return fmt.Errorf(
					"new service failed to start: %w; stop-before-rollback: %v; old binary restored but old service restart failed: %v",
					startErr, stopErr, restartErr,
				)
			}
			return fmt.Errorf("new service failed to start and was rolled back successfully: %w", startErr)
		}
	}

	if err := os.Remove(backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove successful-upgrade backup: %w", err)
	}
	state := "stopped"
	if wasRunning {
		state = "running"
	}
	fmt.Printf("[service] upgraded=true executable=%s state=%s\n", target, state)
	fmt.Println("[service] machine config, CommandCode SecretStore, Hub SecretStore, firewall rule, and SCM registration were preserved")
	return nil
}

func stageUpgradeExecutable(source, target string) (string, error) {
	source = absolute(source)
	target = absolute(target)
	if source == "" || target == "" {
		return "", errors.New("upgrade source and installed target are required")
	}
	if strings.EqualFold(source, target) {
		return "", errors.New("upgrade source is the installed service executable; run the command from the new binary or pass --source")
	}
	if info, err := os.Stat(source); err != nil {
		return "", fmt.Errorf("inspect upgrade source executable: %w", err)
	} else if !info.Mode().IsRegular() {
		return "", errors.New("upgrade source executable is not a regular file")
	}
	if info, err := os.Stat(target); err != nil {
		return "", fmt.Errorf("inspect installed service executable: %w", err)
	} else if !info.Mode().IsRegular() {
		return "", errors.New("installed service executable is not a regular file")
	}

	backup := target + ".upgrade.bak"
	if _, err := os.Stat(backup); err == nil {
		return "", fmt.Errorf("stale upgrade backup exists at %s; refusing to overwrite it", backup)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect upgrade backup: %w", err)
	}

	staged := target + ".upgrade.new"
	if err := os.Remove(staged); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("remove stale staged upgrade: %w", err)
	}
	if err := copyUpgradeFile(source, staged); err != nil {
		_ = os.Remove(staged)
		return "", err
	}
	return staged, nil
}

func copyUpgradeFile(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open upgrade source executable: %w", err)
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create upgrade staging directory: %w", err)
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("create staged upgrade executable: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = output.Close()
		}
	}()
	if _, err := io.Copy(output, input); err != nil {
		return fmt.Errorf("copy upgrade executable: %w", err)
	}
	if err := output.Sync(); err != nil {
		return fmt.Errorf("flush staged upgrade executable: %w", err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("close staged upgrade executable: %w", err)
	}
	closed = true
	return nil
}

func swapUpgradeExecutable(target, staged string) (string, error) {
	target = absolute(target)
	staged = absolute(staged)
	backup := target + ".upgrade.bak"
	if _, err := os.Stat(backup); err == nil {
		return "", fmt.Errorf("stale upgrade backup exists at %s", backup)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect upgrade backup: %w", err)
	}
	if err := os.Rename(target, backup); err != nil {
		return "", fmt.Errorf("stage installed executable for rollback: %w", err)
	}
	if err := os.Rename(staged, target); err != nil {
		restoreErr := os.Rename(backup, target)
		if restoreErr != nil {
			return "", fmt.Errorf("commit staged upgrade: %w; restore old executable failed: %v", err, restoreErr)
		}
		return "", fmt.Errorf("commit staged upgrade: %w", err)
	}
	return backup, nil
}

func rollbackUpgradeExecutable(target, backup string) error {
	target = absolute(target)
	backup = absolute(backup)
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove failed upgraded executable: %w", err)
	}
	if err := os.Rename(backup, target); err != nil {
		return fmt.Errorf("restore previous service executable: %w", err)
	}
	return nil
}
