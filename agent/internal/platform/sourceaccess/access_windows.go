//go:build windows

package sourceaccess

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const localSystemSID = "*S-1-5-18"

func Supported() bool { return true }

// EnsureServiceReadable adds the minimum LocalSystem read/traverse access needed
// for the Windows service to consume user-owned ZCode SQLite databases. Existing
// ACL entries are preserved. Directory inheritance covers future SQLite -wal/-shm
// files without granting the service write access.
func EnsureServiceReadable(paths ...string) error {
	seenDirectories := map[string]struct{}{}
	seenFiles := map[string]struct{}{}
	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("resolve ZCode source path: %w", err)
		}
		absolute = filepath.Clean(absolute)
		info, err := os.Stat(absolute)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("inspect ZCode source path: %w", err)
		}
		if info.IsDir() {
			if err := grantDirectory(absolute, seenDirectories); err != nil {
				return err
			}
			continue
		}
		if err := grantDirectory(filepath.Dir(absolute), seenDirectories); err != nil {
			return err
		}
		if err := grantFile(absolute, seenFiles); err != nil {
			return err
		}
		for _, suffix := range []string{"-wal", "-shm"} {
			sidecar := absolute + suffix
			if sidecarInfo, statErr := os.Stat(sidecar); statErr == nil && !sidecarInfo.IsDir() {
				if err := grantFile(sidecar, seenFiles); err != nil {
					return err
				}
			} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
				return fmt.Errorf("inspect ZCode SQLite sidecar: %w", statErr)
			}
		}
	}
	return nil
}

func grantDirectory(path string, seen map[string]struct{}) error {
	key := strings.ToLower(filepath.Clean(path))
	if _, exists := seen[key]; exists {
		return nil
	}
	seen[key] = struct{}{}
	// (OI)(CI)(RX): current directory plus inheritable read/execute for future
	// child files/directories. /grant is additive and preserves existing ACLs.
	if err := runICACLS(path, "/grant", localSystemSID+":(OI)(CI)(RX)"); err != nil {
		return fmt.Errorf("grant LocalSystem read access to ZCode directory %s: %w", path, err)
	}
	return nil
}

func grantFile(path string, seen map[string]struct{}) error {
	key := strings.ToLower(filepath.Clean(path))
	if _, exists := seen[key]; exists {
		return nil
	}
	seen[key] = struct{}{}
	if err := runICACLS(path, "/grant", localSystemSID+":(R)"); err != nil {
		return fmt.Errorf("grant LocalSystem read access to ZCode file %s: %w", path, err)
	}
	return nil
}

func runICACLS(path string, args ...string) error {
	commandArgs := append([]string{path}, args...)
	command := exec.Command("icacls.exe", commandArgs...)
	output, err := command.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if text == "" {
			return err
		}
		return fmt.Errorf("%s: %w", text, err)
	}
	return nil
}
