package hub

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultHTTPPort      = 8787
	DefaultDiscoveryPort = 8788
	DefaultAgentID       = "desktop-main"
	DefaultHubID         = "central-hub"
	DefaultStaleAfter    = 45 * time.Second
)

type Config struct {
	DatabasePath     string
	PrimaryAgentID   string
	AgentToken       string
	StaleAfter       time.Duration
	HubID            string
	DiscoveryEnabled bool
	DiscoveryPort    int
	HTTPScheme       string
	HTTPPort         int
}

func FromEnvironment() (Config, error) {
	config := Config{
		DatabasePath:     strings.TrimSpace(os.Getenv("HUD_HUB_DB")),
		PrimaryAgentID:   strings.TrimSpace(os.Getenv("HUD_HUB_AGENT_ID")),
		AgentToken:       strings.TrimSpace(os.Getenv("HUD_HUB_AGENT_TOKEN")),
		HubID:            strings.TrimSpace(os.Getenv("HUD_HUB_ID")),
		HTTPScheme:       strings.ToLower(strings.TrimSpace(os.Getenv("HUD_HUB_HTTP_SCHEME"))),
		StaleAfter:       DefaultStaleAfter,
		DiscoveryPort:    DefaultDiscoveryPort,
		HTTPPort:         DefaultHTTPPort,
		DiscoveryEnabled: false,
	}
	if config.DatabasePath == "" {
		config.DatabasePath = ".local/hub.sqlite3"
	}
	if config.PrimaryAgentID == "" {
		config.PrimaryAgentID = DefaultAgentID
	}
	if config.HubID == "" {
		config.HubID = DefaultHubID
	}
	if config.HTTPScheme == "" {
		config.HTTPScheme = "http"
	}

	if raw := strings.TrimSpace(os.Getenv("HUD_HUB_STALE_AFTER_SECONDS")); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("HUD_HUB_STALE_AFTER_SECONDS must be an integer: %w", err)
		}
		config.StaleAfter = time.Duration(seconds) * time.Second
	}
	if raw := strings.TrimSpace(os.Getenv("HUD_HUB_DISCOVERY_ENABLED")); raw != "" {
		value, err := parseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("HUD_HUB_DISCOVERY_ENABLED: %w", err)
		}
		config.DiscoveryEnabled = value
	}
	if raw := strings.TrimSpace(os.Getenv("HUD_HUB_DISCOVERY_PORT")); raw != "" {
		port, err := parsePort(raw)
		if err != nil {
			return Config{}, fmt.Errorf("HUD_HUB_DISCOVERY_PORT: %w", err)
		}
		config.DiscoveryPort = port
	}
	if raw := strings.TrimSpace(os.Getenv("HUD_HUB_HTTP_PORT")); raw != "" {
		port, err := parsePort(raw)
		if err != nil {
			return Config{}, fmt.Errorf("HUD_HUB_HTTP_PORT: %w", err)
		}
		config.HTTPPort = port
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.DatabasePath) == "" {
		return errors.New("HUD_HUB_DB must not be empty")
	}
	if strings.TrimSpace(c.PrimaryAgentID) == "" {
		return errors.New("HUD_HUB_AGENT_ID must not be empty")
	}
	if strings.TrimSpace(c.AgentToken) == "" {
		return errors.New("HUD_HUB_AGENT_TOKEN is required")
	}
	if c.StaleAfter < 5*time.Second {
		return errors.New("HUD_HUB_STALE_AFTER_SECONDS must be at least 5")
	}
	if strings.TrimSpace(c.HubID) == "" {
		return errors.New("HUD_HUB_ID must not be empty")
	}
	if c.HTTPScheme != "http" && c.HTTPScheme != "https" {
		return errors.New("HUD_HUB_HTTP_SCHEME must be http or https")
	}
	if c.DiscoveryPort < 1 || c.DiscoveryPort > 65535 {
		return errors.New("HUD_HUB_DISCOVERY_PORT must be within 1..65535")
	}
	if c.HTTPPort < 1 || c.HTTPPort > 65535 {
		return errors.New("HUD_HUB_HTTP_PORT must be within 1..65535")
	}
	return nil
}

func parseBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, errors.New("must be a boolean")
	}
}

func parsePort(raw string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, errors.New("must be an integer")
	}
	if port < 1 || port > 65535 {
		return 0, errors.New("must be within 1..65535")
	}
	return port, nil
}
