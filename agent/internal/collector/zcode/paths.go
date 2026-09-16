package zcode

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolvedPaths returns the ZCode database paths for the current interactive
// user, honoring the same environment overrides as the collector. Service
// installation snapshots these paths into machine config so LocalSystem does
// not accidentally resolve its own profile after reboot.
func ResolvedPaths() (runtimeDB, taskIndexDB string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	runtimeDB = envPath("HUD_ZCODE_RUNTIME_DB", filepath.Join(home, ".zcode", "cli", "db", "db.sqlite"))
	taskIndexDB = envPath("HUD_ZCODE_DB", filepath.Join(home, ".zcode", "v2", "tasks-index.sqlite"))
	return absolutePath(runtimeDB), absolutePath(taskIndexDB), nil
}

func envPath(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func absolutePath(path string) string {
	absolute, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(absolute)
}
