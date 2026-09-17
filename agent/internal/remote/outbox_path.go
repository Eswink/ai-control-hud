package remote

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const outboxEnvironment = "AI_CONTROL_HUB_OUTBOX"

func ResolveOutboxPath() (string, error) {
	if configured := strings.TrimSpace(os.Getenv(outboxEnvironment)); configured != "" {
		absolute, err := filepath.Abs(configured)
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", outboxEnvironment, err)
		}
		return absolute, nil
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory for event outbox: %w", err)
	}
	return filepath.Join(configDir, "AIControlHUD", "events.sqlite3"), nil
}
