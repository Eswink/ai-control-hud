package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
)

func zcodeStorageDiagnosticsProvider(
	bindingMode string,
	layoutSource string,
	runtimeDB string,
	taskIndexDB string,
	logDir string,
) func() domain.ZCodeStorageDiagnostics {
	return func() domain.ZCodeStorageDiagnostics {
		runtimeExpected := strings.TrimSpace(runtimeDB) != ""
		taskExpected := strings.TrimSpace(taskIndexDB) != ""
		runtimeReadable := readableRegularFile(runtimeDB)
		taskReadable := readableRegularFile(taskIndexDB)
		logReadable := readableDirectory(logDir)

		return domain.ZCodeStorageDiagnostics{
			BindingMode:             sanitizeBindingLabel(bindingMode, "unknown"),
			LayoutSource:            sanitizeBindingLabel(layoutSource, "unknown"),
			RuntimeDatabaseReadable: runtimeReadable,
			TaskIndexReadable:       taskReadable,
			TurnLogReadable:         logReadable,
			RefreshRecommended:      (runtimeExpected && !runtimeReadable) || (taskExpected && !taskReadable),
		}
	}
}

func readableRegularFile(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	return err == nil && info.Mode().IsRegular()
}

func readableDirectory(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	directory, err := os.Open(path)
	if err != nil {
		return false
	}
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil || !info.IsDir() {
		return false
	}
	_, err = directory.Readdirnames(1)
	return err == nil || err == io.EOF
}

func logDirForRuntimeDB(runtimeDB string) string {
	runtimeDB = strings.TrimSpace(runtimeDB)
	if runtimeDB == "" {
		return ""
	}
	parent := filepath.Dir(filepath.Clean(runtimeDB))
	if !strings.EqualFold(filepath.Base(parent), "db") {
		return ""
	}
	return filepath.Join(filepath.Dir(parent), "log")
}

func sanitizeBindingLabel(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 {
		return fallback
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return fallback
	}
	return value
}
